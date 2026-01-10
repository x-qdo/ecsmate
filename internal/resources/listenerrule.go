package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

type ListenerRuleAction string

const (
	ListenerRuleActionCreate ListenerRuleAction = "CREATE"
	ListenerRuleActionUpdate ListenerRuleAction = "UPDATE"
	ListenerRuleActionDelete ListenerRuleAction = "DELETE"
	ListenerRuleActionNoop   ListenerRuleAction = "NOOP"
)

type ListenerRuleResource struct {
	Priority          int
	Desired           *config.IngressRule
	Current           *types.Rule
	Action            ListenerRuleAction
	ListenerArn       string
	TargetGroupArn    string // Resolved ARN for service backends
	Arn               string
	PropagationReason string // Set when action was propagated from dependency
}

type ListenerRuleManager struct {
	client *awsclient.ELBV2Client
}

func NewListenerRuleManager(client *awsclient.ELBV2Client) *ListenerRuleManager {
	return &ListenerRuleManager{
		client: client,
	}
}

func (m *ListenerRuleManager) DescribeExistingRules(ctx context.Context, listenerArn string) ([]types.Rule, error) {
	return m.client.DescribeListenerRules(ctx, listenerArn)
}

func (m *ListenerRuleManager) BuildResources(ctx context.Context, listenerArn string, rules []config.IngressRule, targetGroupArns map[int]string, manifestName string) ([]*ListenerRuleResource, error) {
	existingRules, err := m.client.DescribeListenerRules(ctx, listenerArn)
	if err != nil {
		return nil, fmt.Errorf("failed to describe listener rules: %w", err)
	}

	return m.BuildResourcesWithExisting(listenerArn, rules, targetGroupArns, existingRules, manifestName), nil
}

// BuildResourcesWithExisting builds listener rule resources with manifest name for ownership filtering.
// When manifestName is provided, only rules whose target groups belong to this manifest are marked for deletion.
func (m *ListenerRuleManager) BuildResourcesWithExisting(listenerArn string, rules []config.IngressRule, targetGroupArns map[int]string, existingRules []types.Rule, manifestName string) []*ListenerRuleResource {
	matches, usedArns := matchExistingListenerRulesWithUsed(rules, existingRules)

	var resources []*ListenerRuleResource

	// Build resources for desired rules
	for i := range rules {
		rule := &rules[i]
		resource := &ListenerRuleResource{
			Priority:    rule.Priority,
			Desired:     rule,
			ListenerArn: listenerArn,
		}

		// Resolve target group ARN for service backends
		if rule.Service != nil {
			if arn, ok := targetGroupArns[i]; ok {
				resource.TargetGroupArn = arn
			}
		}

		if existing, ok := matches[i]; ok && existing != nil {
			resource.Current = existing
			resource.Arn = aws.ToString(existing.RuleArn)
		}

		resource.determineAction()
		resources = append(resources, resource)
	}

	// Build DELETE resources for orphaned rules (exist in AWS but not in manifest)
	// Only delete rules whose target groups belong to this manifest
	for i := range existingRules {
		existing := &existingRules[i]
		arn := aws.ToString(existing.RuleArn)
		if arn == "" || usedArns[arn] {
			continue // Skip matched rules
		}

		// Skip default rule
		priority := aws.ToString(existing.Priority)
		if priority == "default" {
			continue
		}

		tgArn := extractTargetGroupArn(existing)
		tgName := extractTargetGroupName(tgArn)

		// Skip rules whose target groups don't belong to this manifest
		if manifestName != "" && tgArn != "" && !isListenerRuleOwnedByManifest(tgName, manifestName) {
			continue
		}

		priorityInt := 0
		fmt.Sscanf(priority, "%d", &priorityInt)

		resource := &ListenerRuleResource{
			Priority:       priorityInt,
			Desired:        nil, // No desired state = delete
			Current:        existing,
			Arn:            arn,
			ListenerArn:    listenerArn,
			TargetGroupArn: tgArn,
		}
		resource.determineAction()
		resources = append(resources, resource)
	}

	return resources
}

// isListenerRuleOwnedByManifest checks if a listener rule's target group belongs to the manifest.
// TG naming convention: {manifestName}-r{priority}
func isListenerRuleOwnedByManifest(tgName, manifestName string) bool {
	if manifestName == "" {
		return true // No filtering if manifest name not provided
	}
	if tgName == "" {
		return false // Can't determine ownership without TG name
	}

	// Pattern: {manifestName}-r{number}
	prefix := manifestName + "-r"
	if !strings.HasPrefix(tgName, prefix) {
		return false
	}

	// Check that what follows is a number
	suffix := tgName[len(prefix):]
	if suffix == "" {
		return false
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (resource *ListenerRuleResource) determineAction() {
	if resource.Desired == nil {
		if resource.Current != nil {
			resource.Action = ListenerRuleActionDelete
		} else {
			resource.Action = ListenerRuleActionNoop
		}
		return
	}

	if resource.Current == nil {
		resource.Action = ListenerRuleActionCreate
		return
	}

	if resource.configChanged() {
		resource.Action = ListenerRuleActionUpdate
		return
	}

	resource.Action = ListenerRuleActionNoop
}

func (resource *ListenerRuleResource) configChanged() bool {
	if resource.Desired == nil || resource.Current == nil {
		return false
	}

	desiredHosts, desiredPaths := normalizeIngressConditions(resource.Desired)
	currentHosts, currentPaths := extractIngressConditions(resource.Current)
	if !conditionsMatch(desiredHosts, desiredPaths, currentHosts, currentPaths) {
		return true
	}

	desired := resource.Desired

	switch {
	case desired.Service != nil:
		return !actionForwardMatches(resource.Current, resource.TargetGroupArn)
	case desired.Redirect != nil:
		return !actionRedirectMatches(resource.Current, desired.Redirect)
	case desired.FixedResponse != nil:
		return !actionFixedResponseMatches(resource.Current, desired.FixedResponse)
	default:
		return false
	}
}

// HasConfigChanges returns true if the resource has actual configuration changes.
// This is used to verify propagated actions have real changes to apply.
func (resource *ListenerRuleResource) HasConfigChanges() bool {
	return resource.configChanged()
}

func actionForwardMatches(rule *types.Rule, targetGroupArn string) bool {
	if rule == nil {
		return false
	}

	currentArn := extractTargetGroupArn(rule)

	// If desired ARN is empty but current exists, no change needed
	// (ARN will be resolved during apply from the existing rule)
	if targetGroupArn == "" {
		return currentArn != ""
	}

	return currentArn == targetGroupArn
}

func actionRedirectMatches(rule *types.Rule, desired *config.IngressRedirect) bool {
	if rule == nil || desired == nil {
		return false
	}
	for _, action := range rule.Actions {
		if action.Type != types.ActionTypeEnumRedirect || action.RedirectConfig == nil {
			continue
		}
		rc := action.RedirectConfig
		if string(rc.StatusCode) != desired.StatusCode {
			return false
		}
		if desired.Protocol != "" && aws.ToString(rc.Protocol) != desired.Protocol {
			return false
		}
		if desired.Host != "" && aws.ToString(rc.Host) != desired.Host {
			return false
		}
		if desired.Port != "" && aws.ToString(rc.Port) != desired.Port {
			return false
		}
		if desired.Path != "" && aws.ToString(rc.Path) != desired.Path {
			return false
		}
		if desired.Query != "" && aws.ToString(rc.Query) != desired.Query {
			return false
		}
		return true
	}
	return false
}

func actionFixedResponseMatches(rule *types.Rule, desired *config.IngressFixedResponse) bool {
	if rule == nil || desired == nil {
		return false
	}
	for _, action := range rule.Actions {
		if action.Type != types.ActionTypeEnumFixedResponse || action.FixedResponseConfig == nil {
			continue
		}
		fc := action.FixedResponseConfig
		if aws.ToString(fc.StatusCode) != desired.StatusCode {
			return false
		}
		if desired.ContentType != "" && aws.ToString(fc.ContentType) != desired.ContentType {
			return false
		}
		if desired.MessageBody != "" && aws.ToString(fc.MessageBody) != desired.MessageBody {
			return false
		}
		return true
	}
	return false
}

func (m *ListenerRuleManager) Create(ctx context.Context, resource *ListenerRuleResource) error {
	log.Info("creating listener rule", "priority", resource.Priority)

	conditions := m.buildConditions(resource.Desired)
	actions := m.buildActions(resource.Desired, resource.TargetGroupArn)

	rule, err := m.client.CreateListenerRule(ctx, &awsclient.CreateListenerRuleInput{
		ListenerArn: resource.ListenerArn,
		Priority:    resource.Priority,
		Conditions:  conditions,
		Actions:     actions,
	})
	if err != nil {
		return err
	}

	resource.Arn = aws.ToString(rule.RuleArn)
	return nil
}

func (m *ListenerRuleManager) Update(ctx context.Context, resource *ListenerRuleResource) error {
	log.Info("updating listener rule", "priority", resource.Priority)

	conditions := m.buildRuleConditions(resource.Desired)
	actions := m.buildRuleActions(resource.Desired, resource.TargetGroupArn)

	return m.client.ModifyListenerRule(ctx, resource.Arn, conditions, actions)
}

func (m *ListenerRuleManager) Delete(ctx context.Context, resource *ListenerRuleResource) error {
	log.Info("deleting listener rule", "priority", resource.Priority)
	return m.client.DeleteListenerRule(ctx, resource.Arn)
}

func (m *ListenerRuleManager) Apply(ctx context.Context, resource *ListenerRuleResource) error {
	switch resource.Action {
	case ListenerRuleActionCreate:
		return m.Create(ctx, resource)
	case ListenerRuleActionUpdate:
		return m.Update(ctx, resource)
	case ListenerRuleActionDelete:
		return m.Delete(ctx, resource)
	case ListenerRuleActionNoop:
		log.Debug("no changes detected, skipping listener rule", "priority", resource.Priority)
		return nil
	default:
		return fmt.Errorf("unknown action: %s", resource.Action)
	}
}

func (m *ListenerRuleManager) buildConditions(rule *config.IngressRule) []awsclient.RuleConditionInput {
	var result []awsclient.RuleConditionInput

	// Host header conditions
	if rule.Host != "" {
		result = append(result, awsclient.RuleConditionInput{
			Type:   "host-header",
			Values: []string{rule.Host},
		})
	}
	if len(rule.Hosts) > 0 {
		result = append(result, awsclient.RuleConditionInput{
			Type:   "host-header",
			Values: rule.Hosts,
		})
	}

	// Path pattern conditions
	if len(rule.Paths) > 0 {
		result = append(result, awsclient.RuleConditionInput{
			Type:   "path-pattern",
			Values: rule.Paths,
		})
	}

	return result
}

func (m *ListenerRuleManager) buildActions(rule *config.IngressRule, targetGroupArn string) []awsclient.RuleActionInput {
	var result []awsclient.RuleActionInput

	if rule.Service != nil {
		result = append(result, awsclient.RuleActionInput{
			Type:           "forward",
			TargetGroupArn: targetGroupArn,
		})
	} else if rule.Redirect != nil {
		result = append(result, awsclient.RuleActionInput{
			Type:       "redirect",
			StatusCode: rule.Redirect.StatusCode,
			Protocol:   rule.Redirect.Protocol,
			Host:       rule.Redirect.Host,
			Port:       rule.Redirect.Port,
			Path:       rule.Redirect.Path,
			Query:      rule.Redirect.Query,
		})
	} else if rule.FixedResponse != nil {
		result = append(result, awsclient.RuleActionInput{
			Type:        "fixed-response",
			StatusCode:  rule.FixedResponse.StatusCode,
			ContentType: rule.FixedResponse.ContentType,
			MessageBody: rule.FixedResponse.MessageBody,
		})
	}

	return result
}

func (m *ListenerRuleManager) buildRuleConditions(rule *config.IngressRule) []types.RuleCondition {
	var result []types.RuleCondition

	// Host header conditions
	if rule.Host != "" {
		result = append(result, types.RuleCondition{
			Field: aws.String("host-header"),
			HostHeaderConfig: &types.HostHeaderConditionConfig{
				Values: []string{rule.Host},
			},
		})
	}
	if len(rule.Hosts) > 0 {
		result = append(result, types.RuleCondition{
			Field: aws.String("host-header"),
			HostHeaderConfig: &types.HostHeaderConditionConfig{
				Values: rule.Hosts,
			},
		})
	}

	// Path pattern conditions
	if len(rule.Paths) > 0 {
		result = append(result, types.RuleCondition{
			Field: aws.String("path-pattern"),
			PathPatternConfig: &types.PathPatternConditionConfig{
				Values: rule.Paths,
			},
		})
	}

	return result
}

func (m *ListenerRuleManager) buildRuleActions(rule *config.IngressRule, targetGroupArn string) []types.Action {
	var result []types.Action

	if rule.Service != nil {
		result = append(result, types.Action{
			Type:           types.ActionTypeEnumForward,
			Order:          aws.Int32(1),
			TargetGroupArn: aws.String(targetGroupArn),
		})
	} else if rule.Redirect != nil {
		ra := types.Action{
			Type:  types.ActionTypeEnumRedirect,
			Order: aws.Int32(1),
			RedirectConfig: &types.RedirectActionConfig{
				StatusCode: types.RedirectActionStatusCodeEnum(rule.Redirect.StatusCode),
			},
		}
		if rule.Redirect.Protocol != "" {
			ra.RedirectConfig.Protocol = aws.String(rule.Redirect.Protocol)
		}
		if rule.Redirect.Host != "" {
			ra.RedirectConfig.Host = aws.String(rule.Redirect.Host)
		}
		if rule.Redirect.Port != "" {
			ra.RedirectConfig.Port = aws.String(rule.Redirect.Port)
		}
		if rule.Redirect.Path != "" {
			ra.RedirectConfig.Path = aws.String(rule.Redirect.Path)
		}
		if rule.Redirect.Query != "" {
			ra.RedirectConfig.Query = aws.String(rule.Redirect.Query)
		}
		result = append(result, ra)
	} else if rule.FixedResponse != nil {
		ra := types.Action{
			Type:  types.ActionTypeEnumFixedResponse,
			Order: aws.Int32(1),
			FixedResponseConfig: &types.FixedResponseActionConfig{
				StatusCode: aws.String(rule.FixedResponse.StatusCode),
			},
		}
		if rule.FixedResponse.ContentType != "" {
			ra.FixedResponseConfig.ContentType = aws.String(rule.FixedResponse.ContentType)
		}
		if rule.FixedResponse.MessageBody != "" {
			ra.FixedResponseConfig.MessageBody = aws.String(rule.FixedResponse.MessageBody)
		}
		result = append(result, ra)
	}

	return result
}

func matchExistingListenerRulesWithUsed(desired []config.IngressRule, existing []types.Rule) (map[int]*types.Rule, map[string]bool) {
	matches := make(map[int]*types.Rule)
	used := make(map[string]bool)
	existingByPriority := make(map[int]*types.Rule)

	for i := range existing {
		rule := &existing[i]
		if rule.Priority != nil && aws.ToString(rule.Priority) != "default" {
			priority := 0
			fmt.Sscanf(aws.ToString(rule.Priority), "%d", &priority)
			if priority > 0 {
				existingByPriority[priority] = rule
			}
		}
	}

	for i := range desired {
		rule := &desired[i]
		if rule.Priority > 0 {
			if ex := existingByPriority[rule.Priority]; ex != nil {
				arn := aws.ToString(ex.RuleArn)
				if arn != "" {
					used[arn] = true
				}
				matches[i] = ex
				continue
			}
		}

		if ex := findRuleByConditions(rule, existing, used); ex != nil {
			arn := aws.ToString(ex.RuleArn)
			if arn != "" {
				used[arn] = true
			}
			matches[i] = ex
		}
	}

	return matches, used
}

func findRuleByConditions(desired *config.IngressRule, existing []types.Rule, used map[string]bool) *types.Rule {
	if desired == nil {
		return nil
	}

	desiredHosts, desiredPaths := normalizeIngressConditions(desired)

	for i := range existing {
		rule := &existing[i]
		if rule.Priority != nil && aws.ToString(rule.Priority) == "default" {
			continue
		}
		if arn := aws.ToString(rule.RuleArn); arn != "" && used[arn] {
			continue
		}

		hosts, paths := extractIngressConditions(rule)
		if conditionsMatch(desiredHosts, desiredPaths, hosts, paths) {
			return rule
		}
	}

	return nil
}

func normalizeIngressConditions(rule *config.IngressRule) ([]string, []string) {
	var hosts []string
	if rule.Host != "" {
		hosts = append(hosts, rule.Host)
	}
	hosts = append(hosts, rule.Hosts...)

	paths := append([]string{}, rule.Paths...)

	for i, host := range hosts {
		hosts[i] = strings.ToLower(strings.TrimSpace(host))
	}
	for i, path := range paths {
		paths[i] = strings.TrimSpace(path)
	}

	sort.Strings(hosts)
	sort.Strings(paths)

	return hosts, paths
}

func extractIngressConditions(rule *types.Rule) ([]string, []string) {
	var hosts []string
	var paths []string

	for _, cond := range rule.Conditions {
		if cond.Field == nil {
			continue
		}
		switch aws.ToString(cond.Field) {
		case "host-header":
			if cond.HostHeaderConfig != nil {
				for _, host := range cond.HostHeaderConfig.Values {
					hosts = append(hosts, strings.ToLower(strings.TrimSpace(host)))
				}
			}
		case "path-pattern":
			if cond.PathPatternConfig != nil {
				for _, path := range cond.PathPatternConfig.Values {
					paths = append(paths, strings.TrimSpace(path))
				}
			}
		}
	}

	sort.Strings(hosts)
	sort.Strings(paths)

	return hosts, paths
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func conditionsMatch(desiredHosts, desiredPaths, existingHosts, existingPaths []string) bool {
	if !stringSlicesEqual(desiredHosts, existingHosts) {
		return false
	}

	if len(desiredPaths) == 0 {
		if len(existingPaths) == 0 {
			return true
		}
		if len(existingPaths) == 1 && existingPaths[0] == "/*" {
			return true
		}
		return false
	}

	return stringSlicesEqual(desiredPaths, existingPaths)
}

func extractTargetGroupArn(rule *types.Rule) string {
	for _, action := range rule.Actions {
		if action.Type != types.ActionTypeEnumForward {
			continue
		}
		if action.TargetGroupArn != nil {
			return aws.ToString(action.TargetGroupArn)
		}
		if action.ForwardConfig != nil && len(action.ForwardConfig.TargetGroups) > 0 {
			tg := action.ForwardConfig.TargetGroups[0]
			if tg.TargetGroupArn != nil {
				return aws.ToString(tg.TargetGroupArn)
			}
		}
	}

	return ""
}

func extractRuleArn(rule *types.Rule) string {
	if rule == nil {
		return ""
	}
	return aws.ToString(rule.RuleArn)
}

func extractTargetGroupName(arn string) string {
	// ARN format: arn:aws:elasticloadbalancing:region:account:targetgroup/name/id
	parts := strings.Split(arn, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return arn
}
