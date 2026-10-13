package resources

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	awsclient "github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/log"
)

// IngressError is a preflight failure, not a failed service rollout.
type IngressError struct{ Err error }

func (e *IngressError) Error() string { return "ingress guard: " + e.Err.Error() }
func (e *IngressError) Unwrap() error { return e.Err }

type IngressPlan struct {
	TargetGroups  map[string]*TargetGroupResource
	ListenerRules []*ListenerRuleResource
}

var targetGroupNamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

// ValidateIngressManifest is offline and shared by validate, diff and apply.
func ValidateIngressManifest(manifest *config.Manifest) error {
	if manifest.Ingress == nil {
		return nil
	}
	identifyingTags := 0
	for key, value := range manifest.Tags {
		if key != TagKeyManagedBy && strings.TrimSpace(value) != "" {
			identifyingTags++
		}
	}
	if identifyingTags == 0 {
		return fmt.Errorf("manifest %q with ingress requires instance-identifying tags beyond ManagedBy", manifest.Name)
	}
	if manager, ok := manifest.Tags[TagKeyManagedBy]; ok && manager != TagValueEcsmate {
		return fmt.Errorf("manifest %q must use ManagedBy=ecsmate", manifest.Name)
	}
	priorities := make(map[int]int)
	for i, rule := range manifest.Ingress.Rules {
		if rule.Priority < 1 || rule.Priority > 50000 {
			return fmt.Errorf("rule %d has invalid priority %d", i, rule.Priority)
		}
		if other, ok := priorities[rule.Priority]; ok {
			return fmt.Errorf("rules %d and %d request the same priority %d", other, i, rule.Priority)
		}
		priorities[rule.Priority] = i
		for key, value := range rule.Tags {
			if expected, ok := manifest.Tags[key]; ok && value != expected {
				return fmt.Errorf("rule %d tag %q contradicts manifest ownership", i, key)
			}
			if key == TagKeyManagedBy && value != TagValueEcsmate {
				return fmt.Errorf("rule %d must use ManagedBy=ecsmate", i)
			}
		}
		if rule.Service != nil {
			name := targetGroupName(manifest.Name, rule.Priority)
			if len(name) > 32 || !targetGroupNamePattern.MatchString(name) {
				return fmt.Errorf("rule %d generates invalid target-group name %q; use at most 32 alphanumeric/hyphen characters without leading or trailing hyphens", i, name)
			}
		}
	}
	return nil
}

func targetGroupName(manifest string, priority int) string {
	return fmt.Sprintf("%s-r%d", manifest, priority)
}

func ingressTags(manifestTags, ruleTags map[string]string) map[string]string {
	tags := make(map[string]string, len(manifestTags)+len(ruleTags)+1)
	for key, value := range manifestTags {
		tags[key] = value
	}
	for key, value := range ruleTags {
		tags[key] = value
	}
	tags[TagKeyManagedBy] = TagValueEcsmate
	return tags
}

// DiscoverIngress never writes. The resulting identities are also used by the executor.
func DiscoverIngress(ctx context.Context, client awsclient.IngressAPI, manifest *config.Manifest) (plan *IngressPlan, err error) {
	if manifest == nil || manifest.Ingress == nil {
		return nil, nil
	}
	defer func() {
		if err != nil {
			err = &IngressError{fmt.Errorf("manifest %q %v: %w", manifest.Name, manifest.Tags, err)}
		}
	}()
	if err := ValidateIngressManifest(manifest); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("load balancer client unavailable")
	}
	listener := manifest.Ingress.ListenerArn
	rules, err := client.DescribeListenerRules(ctx, listener)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect listener %s: %w", listener, err)
	}
	sort.Slice(rules, func(i, j int) bool { return parseRulePriority(&rules[i]) < parseRulePriority(&rules[j]) })
	specs := ExtractTargetGroups(manifest, manifest.Name)
	arns := make(map[string]bool)
	namedGroups := make(map[string]*types.TargetGroup)
	for _, rule := range rules {
		if arn := aws.ToString(rule.RuleArn); arn != "" && !aws.ToBool(rule.IsDefault) && parseRulePriority(&rule) > 0 {
			arns[arn] = true
		}
		for _, arn := range ruleTargetGroups(&rule) {
			arns[arn] = true
		}
	}
	for key, spec := range specs {
		tg, err := client.DescribeTargetGroup(ctx, spec.Name)
		if err != nil {
			return nil, fmt.Errorf("cannot inspect target group %s: %w", spec.Name, err)
		}
		namedGroups[key] = tg
		if tg != nil {
			arns[aws.ToString(tg.TargetGroupArn)] = true
		}
	}
	resourceARNs := make([]string, 0, len(arns))
	for arn := range arns {
		resourceARNs = append(resourceARNs, arn)
	}
	sort.Strings(resourceARNs)
	tags := make(map[string]map[string]string)
	if len(resourceARNs) > 0 {
		tags, err = client.DescribeTags(ctx, resourceARNs)
		if err != nil {
			return nil, fmt.Errorf("cannot inspect ingress ownership on %s: %w", listener, err)
		}
	}
	for key, tg := range namedGroups {
		if tg != nil {
			arn := aws.ToString(tg.TargetGroupArn)
			if !ownsIngressTargetGroup(manifest, arn, tags[arn]) {
				rule := manifest.Ingress.Rules[specs[key].RuleIndex]
				hosts, _ := normalizeIngressConditions(&rule)
				return nil, fmt.Errorf("listener %s requested priority %d (hosts %v), target group %s: %w", listener, rule.Priority, hosts, aws.ToString(tg.TargetGroupName), ownershipConflict(manifest, arn, tags[arn]))
			}
		}
	}

	plan = &IngressPlan{TargetGroups: make(map[string]*TargetGroupResource)}
	usedRules, usedGroups := make(map[string]bool), make(map[string]bool)
	for i, desired := range manifest.Ingress.Rules {
		var matched *types.Rule
		hosts, paths := normalizeIngressConditions(&desired)
		for j := range rules {
			existing := &rules[j]
			if parseRulePriority(existing) == 0 || aws.ToBool(existing.IsDefault) {
				continue
			}
			owned := ownsIngressRule(manifest, existing, tags)
			manageable := manageableIngressRule(existing)
			if parseRulePriority(existing) == desired.Priority && (!owned || !manageable) {
				return nil, fmt.Errorf("listener %s priority %d is occupied by protected rule %s (hosts %v): %w", listener, desired.Priority, aws.ToString(existing.RuleArn), ruleHosts(existing), ruleOwnershipConflict(manifest, existing, tags))
			}
			if hostsOverlap(hosts, ruleHosts(existing)) && hasForwardAction(existing) && (!owned || !manageable) {
				return nil, fmt.Errorf("listener %s hosts %v overlap forwarding rule %s at priority %d (hosts %v): %w", listener, hosts, aws.ToString(existing.RuleArn), parseRulePriority(existing), ruleHosts(existing), ruleOwnershipConflict(manifest, existing, tags))
			}
			if !manageable || !owned {
				continue
			}
			existingHosts, existingPaths := extractIngressConditions(existing)
			if conditionsMatch(hosts, paths, existingHosts, existingPaths) {
				if matched != nil {
					return nil, fmt.Errorf("rule %d ambiguously matches both %s and %s", i, aws.ToString(matched.RuleArn), aws.ToString(existing.RuleArn))
				}
				matched = existing
			}
		}
		// An owned, simple rule at the same priority permits an intentional host change.
		if matched == nil {
			for j := range rules {
				if parseRulePriority(&rules[j]) == desired.Priority {
					matched = &rules[j] // ownership and shape were checked above
					break
				}
			}
		}
		priority := desired.Priority
		if matched != nil {
			arn := aws.ToString(matched.RuleArn)
			if usedRules[arn] {
				return nil, fmt.Errorf("multiple manifest rules would manage listener rule %s", arn)
			}
			usedRules[arn] = true
			priority = parseRulePriority(matched)
			if priority != desired.Priority {
				log.Warn("ingress priority drift; preserving owned rule and target group", "rule", arn, "requested", desired.Priority, "actual", priority)
			}
		}
		desired.Tags = ingressTags(manifest.Tags, desired.Tags)
		rule := &ListenerRuleResource{RuleIndex: i, ListenerArn: listener, Priority: priority, Desired: &desired, Current: matched}
		if matched != nil {
			rule.Arn = aws.ToString(matched.RuleArn)
		}
		key := fmt.Sprintf("rule-%d", i)
		if spec := specs[key]; spec != nil {
			tg := namedGroups[key]
			if arn := extractTargetGroupArn(matched); arn != "" && (tg == nil || aws.ToString(tg.TargetGroupArn) != arn) {
				tg, err = client.DescribeTargetGroupByArn(ctx, arn)
				if err != nil {
					return nil, fmt.Errorf("cannot inspect target group %s: %w", arn, err)
				}
				if tg == nil {
					return nil, fmt.Errorf("referenced target group %s no longer exists", arn)
				}
			}
			resource := &TargetGroupResource{Name: spec.Name, ManifestName: key, Desired: spec, Current: tg, VpcID: manifest.Ingress.VpcID}
			if resource.Current != nil {
				resource.Arn = aws.ToString(tg.TargetGroupArn)
				if !ownsIngressTargetGroup(manifest, resource.Arn, tags[resource.Arn]) {
					return nil, ownershipConflict(manifest, resource.Arn, tags[resource.Arn])
				}
				// Priority drift must not rename/recreate the adopted group during apply.
				resource.Name = aws.ToString(resource.Current.TargetGroupName)
				resource.Desired.Name = resource.Name
			}
			resource.determineAction()
			if resource.Arn != "" && usedGroups[resource.Arn] {
				return nil, fmt.Errorf("multiple ingress backends would share target group %s", resource.Arn)
			}
			usedGroups[resource.Arn] = resource.Arn != ""
			rule.TargetGroupArn = resource.Arn
			plan.TargetGroups[key] = resource
		}
		rule.determineAction()
		plan.ListenerRules = append(plan.ListenerRules, rule)
	}

	// Only simple, owned unused rules are eligible for cleanup. A group referenced
	// by any protected rule must remain, even if its tags look like ours.
	protectedGroups := make(map[string]bool)
	for i := range rules {
		if !manageableIngressRule(&rules[i]) || !ownsIngressRule(manifest, &rules[i], tags) {
			for _, arn := range ruleTargetGroups(&rules[i]) {
				protectedGroups[arn] = true
			}
		}
	}
	for i := range rules {
		rule := &rules[i]
		arn := aws.ToString(rule.RuleArn)
		if usedRules[arn] || !manageableIngressRule(rule) || !ownsIngressRule(manifest, rule, tags) {
			continue
		}
		plan.ListenerRules = append(plan.ListenerRules, &ListenerRuleResource{ListenerArn: listener, Priority: parseRulePriority(rule), Arn: arn, Current: rule, Action: ListenerRuleActionDelete})
		tgARN := extractTargetGroupArn(rule)
		if tgARN == "" || usedGroups[tgARN] || protectedGroups[tgARN] {
			continue
		}
		tg, err := client.DescribeTargetGroupByArn(ctx, tgARN)
		if err != nil {
			return nil, fmt.Errorf("cannot inspect orphan target group %s: %w", tgARN, err)
		}
		if tg == nil {
			return nil, fmt.Errorf("orphan target group %s no longer exists; run diff/apply again", tgARN)
		}
		plan.TargetGroups["orphan-"+extractTargetGroupName(tgARN)] = &TargetGroupResource{Name: aws.ToString(tg.TargetGroupName), Arn: tgARN, Current: tg, Action: TargetGroupActionDelete}
	}
	for _, tg := range plan.TargetGroups {
		if protectedGroups[tg.Arn] && tg.Action != TargetGroupActionNoop {
			return nil, fmt.Errorf("target group %s is referenced by a protected rule; refusing to change it", tg.Arn)
		}
	}
	return plan, nil
}

func ownsIngressTargetGroup(manifest *config.Manifest, arn string, tags map[string]string) bool {
	return isTargetGroupOwnedByManifest(extractTargetGroupName(arn), manifest.Name) && matchesManifestTags(tags, manifest.Tags)
}

func ownsIngressRule(manifest *config.Manifest, rule *types.Rule, tags map[string]map[string]string) bool {
	if parseRulePriority(rule) == 0 || aws.ToBool(rule.IsDefault) {
		return false
	}
	ruleTags := tags[aws.ToString(rule.RuleArn)]
	if len(ruleTags) > 0 && !matchesManifestTags(ruleTags, manifest.Tags) {
		return false
	}
	groups := ruleTargetGroups(rule)
	if len(groups) == 0 {
		return matchesManifestTags(ruleTags, manifest.Tags)
	}
	for _, arn := range groups {
		if !ownsIngressTargetGroup(manifest, arn, tags[arn]) {
			return false
		}
	}
	return true
}

func ownershipConflict(manifest *config.Manifest, arn string, tags map[string]string) error {
	var differences []string
	for key, expected := range ingressTags(manifest.Tags, nil) {
		if actual, exists := tags[key]; !exists {
			differences = append(differences, fmt.Sprintf("missing tag %q (expected %q)", key, expected))
		} else if actual != expected {
			differences = append(differences, fmt.Sprintf("tag %q is %q (expected %q)", key, actual, expected))
		}
	}
	if len(differences) > 0 {
		sort.Strings(differences)
		return fmt.Errorf("ownership unverified for %s: %s", arn, strings.Join(differences, "; "))
	}
	return fmt.Errorf("target group %s does not match naming pattern %q", arn, manifest.Name+"-r<priority>")
}

func ruleOwnershipConflict(manifest *config.Manifest, rule *types.Rule, tags map[string]map[string]string) error {
	for _, arn := range ruleTargetGroups(rule) {
		if !ownsIngressTargetGroup(manifest, arn, tags[arn]) {
			return ownershipConflict(manifest, arn, tags[arn])
		}
	}
	if !manageableIngressRule(rule) {
		return fmt.Errorf("conditions or actions are not managed by ECSMate")
	}
	arn := aws.ToString(rule.RuleArn)
	return ownershipConflict(manifest, arn, tags[arn])
}

func ruleTargetGroups(rule *types.Rule) []string {
	if rule == nil {
		return nil
	}
	groups := make(map[string]bool)
	for _, action := range rule.Actions {
		if action.Type != types.ActionTypeEnumForward {
			continue
		}
		if arn := aws.ToString(action.TargetGroupArn); arn != "" {
			groups[arn] = true
		}
		if action.ForwardConfig != nil {
			for _, group := range action.ForwardConfig.TargetGroups {
				groups[aws.ToString(group.TargetGroupArn)] = true
			}
		}
	}
	result := make([]string, 0, len(groups))
	for arn := range groups {
		if arn != "" {
			result = append(result, arn)
		}
	}
	sort.Strings(result)
	return result
}

func hasForwardAction(rule *types.Rule) bool {
	for _, action := range rule.Actions {
		if action.Type == types.ActionTypeEnumForward {
			return true
		}
	}
	return false
}

func manageableIngressRule(rule *types.Rule) bool {
	if parseRulePriority(rule) == 0 || len(rule.Actions) != 1 || len(rule.Transforms) != 0 {
		return false
	}
	for _, condition := range rule.Conditions {
		switch aws.ToString(condition.Field) {
		case "host-header":
			if condition.HostHeaderConfig == nil || len(condition.HostHeaderConfig.RegexValues) > 0 {
				return false
			}
		case "path-pattern":
			if condition.PathPatternConfig == nil || len(condition.PathPatternConfig.RegexValues) > 0 {
				return false
			}
		default:
			return false
		}
	}
	action := rule.Actions[0]
	switch action.Type {
	case types.ActionTypeEnumForward:
		if len(ruleTargetGroups(rule)) != 1 {
			return false
		}
		if forward := action.ForwardConfig; forward != nil {
			if len(forward.TargetGroups) != 1 || (forward.TargetGroupStickinessConfig != nil && aws.ToBool(forward.TargetGroupStickinessConfig.Enabled)) {
				return false
			}
			if weight := forward.TargetGroups[0].Weight; weight != nil && *weight == 0 {
				return false
			}
		}
		return true
	case types.ActionTypeEnumRedirect, types.ActionTypeEnumFixedResponse:
		return true
	}
	return false
}

func ruleHosts(rule *types.Rule) []string {
	hosts, _ := extractIngressConditions(rule)
	// No supported host condition is a catch-all or an unclassified regex: do
	// not let it silently establish a different instance's hostname ownership.
	return hosts
}

func hostsOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, left := range a {
		for _, right := range b {
			if strings.ContainsAny(left, "*?") && strings.ContainsAny(right, "*?") {
				return true // conservatively reject ambiguous wildcard-to-wildcard ownership
			}
			if match, _ := path.Match(left, right); match {
				return true
			}
			if match, _ := path.Match(right, left); match {
				return true
			}
		}
	}
	return false
}

// RevalidateIngress refuses a stale plan; it never silently replans writes.
func RevalidateIngress(ctx context.Context, client awsclient.IngressAPI, manifest *config.Manifest, groups map[string]*TargetGroupResource, rules []*ListenerRuleResource) error {
	fresh, err := DiscoverIngress(ctx, client, manifest)
	if err != nil || fresh == nil {
		return err
	}
	stale := func() error {
		return &IngressError{fmt.Errorf("routing changed since planning for %q; run diff/apply again", manifest.Name)}
	}
	if len(groups) != len(fresh.TargetGroups) || len(rules) != len(fresh.ListenerRules) {
		return stale()
	}
	for key, previous := range groups {
		current := fresh.TargetGroups[key]
		if current == nil || previous.Arn != current.Arn || !reflect.DeepEqual(previous.Current, current.Current) {
			return stale()
		}
	}
	for i, previous := range rules {
		current := fresh.ListenerRules[i]
		if previous.Arn != current.Arn || previous.Priority != current.Priority || previous.TargetGroupArn != current.TargetGroupArn || !reflect.DeepEqual(previous.Current, current.Current) {
			return stale()
		}
	}
	return nil
}

// Check only services managed by manifest ingress, not unrelated explicit LB bindings.
func ValidateIngressServices(ctx context.Context, client awsclient.IngressAPI, manifest *config.Manifest, services map[string]*ServiceResource) error {
	if manifest.Ingress == nil {
		return nil
	}
	for _, rule := range manifest.Ingress.Rules {
		if rule.Service == nil {
			continue
		}
		service := services[rule.Service.Name]
		if service == nil || service.Current == nil {
			continue
		}
		for _, binding := range service.Current.LoadBalancers {
			if aws.ToString(binding.ContainerName) != rule.Service.ContainerName || int(aws.ToInt32(binding.ContainerPort)) != rule.Service.ContainerPort {
				continue
			}
			arn := aws.ToString(binding.TargetGroupArn)
			tags, err := client.DescribeTags(ctx, []string{arn})
			if err != nil {
				return &IngressError{fmt.Errorf("cannot inspect service %s target group %s: %w", service.Name, arn, err)}
			}
			if !ownsIngressTargetGroup(manifest, arn, tags[arn]) {
				return &IngressError{fmt.Errorf("manifest %q %v, service %s has an unverified target-group binding: %w", manifest.Name, manifest.Tags, service.Name, ownershipConflict(manifest, arn, tags[arn]))}
			}
		}
	}
	return nil
}
