package resources

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/x-qdo/ecsmate/internal/config"
)

func testIngressRule(priority int, host string) config.IngressRule {
	return config.IngressRule{
		Priority: priority,
		Host:     host,
		Service: &config.IngressServiceBackend{
			Name:          "web",
			ContainerName: "nginx",
			ContainerPort: 80,
		},
	}
}

func testForwardRule(arn string, priority int, host string, targetGroupArn string) types.Rule {
	return types.Rule{
		RuleArn:  aws.String(arn),
		Priority: aws.String(fmt.Sprintf("%d", priority)),
		Conditions: []types.RuleCondition{
			{
				Field: aws.String("host-header"),
				HostHeaderConfig: &types.HostHeaderConditionConfig{
					Values: []string{host},
				},
			},
		},
		Actions: []types.Action{
			{
				Type:           types.ActionTypeEnumForward,
				TargetGroupArn: aws.String(targetGroupArn),
			},
		},
	}
}

func TestListenerRuleResource_DetermineAction_NoChange(t *testing.T) {
	resource := &ListenerRuleResource{
		Priority: 100,
		Desired: &config.IngressRule{
			Priority: 100,
			Host:     "example.com",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "nginx",
				ContainerPort: 80,
			},
		},
		TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789:targetgroup/app/abc",
		Current: &types.Rule{
			Priority: aws.String("100"),
			Conditions: []types.RuleCondition{
				{
					Field: aws.String("host-header"),
					HostHeaderConfig: &types.HostHeaderConditionConfig{
						Values: []string{"example.com"},
					},
				},
			},
			Actions: []types.Action{
				{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789:targetgroup/app/abc"),
				},
			},
		},
	}

	resource.determineAction()

	if resource.Action != ListenerRuleActionNoop {
		t.Fatalf("expected action NOOP, got %s", resource.Action)
	}
}

func TestListenerRuleResource_DetermineAction_NoChangeWhenARNUnresolved(t *testing.T) {
	// When TargetGroupArn is empty (not resolved from ingress) but current rule has ARN,
	// should detect as NOOP since no actual change is needed
	resource := &ListenerRuleResource{
		Priority: 100,
		Desired: &config.IngressRule{
			Priority: 100,
			Host:     "example.com",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "nginx",
				ContainerPort: 80,
			},
		},
		TargetGroupArn: "", // Not resolved yet
		Current: &types.Rule{
			Priority: aws.String("100"),
			Conditions: []types.RuleCondition{
				{
					Field: aws.String("host-header"),
					HostHeaderConfig: &types.HostHeaderConditionConfig{
						Values: []string{"example.com"},
					},
				},
			},
			Actions: []types.Action{
				{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789:targetgroup/app/abc"),
				},
			},
		},
	}

	resource.determineAction()

	if resource.Action != ListenerRuleActionNoop {
		t.Fatalf("expected action NOOP when ARN unresolved but current has ARN, got %s", resource.Action)
	}
}

func TestActionForwardMatches_EmptyDesiredWithExistingCurrent(t *testing.T) {
	// When desired ARN is empty but current rule has ARN, should return true (matches)
	rule := &types.Rule{
		Actions: []types.Action{
			{
				Type:           types.ActionTypeEnumForward,
				TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789:targetgroup/app/abc"),
			},
		},
	}

	if !actionForwardMatches(rule, "") {
		t.Fatal("expected actionForwardMatches to return true when desired is empty but current has ARN")
	}
}

func TestActionForwardMatches_BothEmpty(t *testing.T) {
	// When both desired and current ARN are empty, should return false
	rule := &types.Rule{
		Actions: []types.Action{
			{
				Type: types.ActionTypeEnumForward,
				// No TargetGroupArn set
			},
		},
	}

	if actionForwardMatches(rule, "") {
		t.Fatal("expected actionForwardMatches to return false when both are empty")
	}
}

func TestListenerRuleResource_DetermineAction_DeleteWhenRuleRemovedFromManifest(t *testing.T) {
	// When an ingress rule is removed from manifest (Desired=nil) but exists in AWS (Current!=nil),
	// it should be marked for DELETE
	resource := &ListenerRuleResource{
		Priority: 100,
		Desired:  nil, // Rule removed from manifest
		Current: &types.Rule{
			Priority: aws.String("100"),
			Conditions: []types.RuleCondition{
				{
					Field: aws.String("host-header"),
					HostHeaderConfig: &types.HostHeaderConditionConfig{
						Values: []string{"example.com"},
					},
				},
			},
			Actions: []types.Action{
				{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789:targetgroup/app/abc"),
				},
			},
		},
	}

	resource.determineAction()

	if resource.Action != ListenerRuleActionDelete {
		t.Fatalf("expected action DELETE when rule removed from manifest, got %s", resource.Action)
	}
}

func TestListenerRuleResource_DetermineAction_NoopWhenBothNil(t *testing.T) {
	// When both Desired and Current are nil, should be NOOP (nothing to do)
	resource := &ListenerRuleResource{
		Priority: 100,
		Desired:  nil,
		Current:  nil,
	}

	resource.determineAction()

	if resource.Action != ListenerRuleActionNoop {
		t.Fatalf("expected action NOOP when both nil, got %s", resource.Action)
	}
}

func TestListenerRuleResource_DetermineAction_UpdateOnRedirectChange(t *testing.T) {
	resource := &ListenerRuleResource{
		Priority: 100,
		Desired: &config.IngressRule{
			Priority: 100,
			Host:     "example.com",
			Redirect: &config.IngressRedirect{
				StatusCode: "HTTP_301",
				Protocol:   "HTTPS",
				Host:       "example.com",
				Path:       "/new",
			},
		},
		Current: &types.Rule{
			Priority: aws.String("100"),
			Conditions: []types.RuleCondition{
				{
					Field: aws.String("host-header"),
					HostHeaderConfig: &types.HostHeaderConditionConfig{
						Values: []string{"example.com"},
					},
				},
			},
			Actions: []types.Action{
				{
					Type: types.ActionTypeEnumRedirect,
					RedirectConfig: &types.RedirectActionConfig{
						StatusCode: types.RedirectActionStatusCodeEnumHttp301,
						Protocol:   aws.String("HTTPS"),
						Host:       aws.String("example.com"),
						Path:       aws.String("/old"),
					},
				},
			},
		},
	}

	resource.determineAction()

	if resource.Action != ListenerRuleActionUpdate {
		t.Fatalf("expected action UPDATE, got %s", resource.Action)
	}
}

func TestBuildResourcesWithExisting_DetectsOrphanedRules(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:us-east-1:123:listener/app/my-alb/abc"
	manifestName := "myapp"

	// Manifest has one rule at priority 100
	manifestRules := []config.IngressRule{
		{
			Priority: 100,
			Host:     "example.com",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "app",
				ContainerPort: 80,
			},
		},
	}

	// AWS has two rules: priority 100 (in manifest) and priority 200 (orphaned but owned by same manifest)
	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123:rule/abc/100"),
			Priority: aws.String("100"),
			Conditions: []types.RuleCondition{
				{
					Field: aws.String("host-header"),
					HostHeaderConfig: &types.HostHeaderConditionConfig{
						Values: []string{"example.com"},
					},
				},
			},
			Actions: []types.Action{
				{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/myapp-r100/abc"),
				},
			},
		},
		{
			RuleArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123:rule/abc/200"),
			Priority: aws.String("200"),
			Conditions: []types.RuleCondition{
				{
					Field: aws.String("host-header"),
					HostHeaderConfig: &types.HostHeaderConditionConfig{
						Values: []string{"orphaned.com"},
					},
				},
			},
			Actions: []types.Action{
				{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/myapp-r200/xyz"),
				},
			},
		},
	}

	targetGroupArns := map[int]string{
		0: "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/myapp-r100/abc",
	}

	tgTags := map[string]map[string]string{
		"arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/myapp-r200/xyz": {
			TagKeyManagedBy: TagValueEcsmate,
		},
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, manifestName, nil, tgTags)

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources (1 manifest + 1 orphaned), got %d", len(resources))
	}

	var manifestResource, orphanedResource *ListenerRuleResource
	for _, r := range resources {
		if r.Priority == 100 {
			manifestResource = r
		} else if r.Priority == 200 {
			orphanedResource = r
		}
	}

	if manifestResource == nil {
		t.Fatal("manifest rule resource not found")
	}
	if manifestResource.Action != ListenerRuleActionNoop {
		t.Errorf("expected manifest rule action NOOP, got %s", manifestResource.Action)
	}
	if manifestResource.Desired == nil {
		t.Error("manifest rule should have Desired set")
	}

	if orphanedResource == nil {
		t.Fatal("orphaned rule resource not found")
	}
	if orphanedResource.Action != ListenerRuleActionDelete {
		t.Errorf("expected orphaned rule action DELETE, got %s", orphanedResource.Action)
	}
	if orphanedResource.Desired != nil {
		t.Error("orphaned rule should NOT have Desired set")
	}
	if orphanedResource.Current == nil {
		t.Error("orphaned rule should have Current set")
	}
	if orphanedResource.Arn != "arn:aws:elasticloadbalancing:us-east-1:123:rule/abc/200" {
		t.Errorf("orphaned rule ARN wrong, got %s", orphanedResource.Arn)
	}
}

func TestBuildResourcesWithExisting_SkipsDefaultRule(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:us-east-1:123:listener/app/my-alb/abc"

	manifestRules := []config.IngressRule{}

	// AWS has only the default rule
	existingRules := []types.Rule{
		{
			RuleArn:   aws.String("arn:aws:elasticloadbalancing:us-east-1:123:rule/abc/default"),
			Priority:  aws.String("default"),
			IsDefault: aws.Bool(true),
		},
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, nil, existingRules, "myapp", nil, nil)

	if len(resources) != 0 {
		t.Errorf("expected 0 resources (default rule should be skipped), got %d", len(resources))
	}
}

func TestMatchExistingListenerRulesWithUsed_TracksUsedArns(t *testing.T) {
	desiredRules := []config.IngressRule{
		{Priority: 100, Host: "a.com"},
		{Priority: 200, Host: "b.com"},
	}

	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:rule-100"),
			Priority: aws.String("100"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"a.com"}}},
			},
		},
		{
			RuleArn:  aws.String("arn:rule-200"),
			Priority: aws.String("200"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"b.com"}}},
			},
		},
		{
			RuleArn:  aws.String("arn:rule-300"),
			Priority: aws.String("300"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"c.com"}}},
			},
		},
	}

	matches, usedArns := matchExistingListenerRulesWithUsed(desiredRules, existingRules, "")

	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(matches))
	}

	if !usedArns["arn:rule-100"] {
		t.Error("expected arn:rule-100 to be marked as used")
	}
	if !usedArns["arn:rule-200"] {
		t.Error("expected arn:rule-200 to be marked as used")
	}
	if usedArns["arn:rule-300"] {
		t.Error("expected arn:rule-300 to NOT be marked as used (orphaned)")
	}
}

func TestMatchExistingListenerRulesWithUsed_DoesNotMatchDifferentManifestSamePriority(t *testing.T) {
	desiredRules := []config.IngressRule{
		{Priority: 200, Host: "calingo.sandbox.eu.cloudinsurance.app"},
	}

	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:rule-mdn"),
			Priority: aws.String("200"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"maiden.sandbox.eu.cloudinsurance.app"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/mdn-r200/abc")},
			},
		},
	}

	matches, usedArns := matchExistingListenerRulesWithUsed(desiredRules, existingRules, "cal")

	if len(matches) != 0 {
		t.Fatalf("expected no match for different manifest rule at same priority, got %d", len(matches))
	}
	if usedArns["arn:rule-mdn"] {
		t.Fatal("different manifest rule must not be marked used")
	}
}

func TestBuildResourcesWithExisting_AssignsNextPriorityWhenRequestedPriorityIsOccupied(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:eu-west-1:123:listener/app/alb/abc"
	manifestRules := []config.IngressRule{
		{
			Priority: 200,
			Host:     "calingo.sandbox.eu.cloudinsurance.app",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "nginx",
				ContainerPort: 80,
			},
		},
	}

	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:rule-mdn"),
			Priority: aws.String("200"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"maiden.sandbox.eu.cloudinsurance.app"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/mdn-r200/abc")},
			},
		},
		{
			RuleArn:  aws.String("arn:rule-thf"),
			Priority: aws.String("201"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"thf.sandbox.eu.cloudinsurance.app"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/thf-r201/abc")},
			},
		},
	}

	targetGroupArns := map[int]string{
		0: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r200/abc",
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, "cal", nil, nil)

	if len(resources) != 1 {
		t.Fatalf("expected one desired listener rule resource, got %d", len(resources))
	}
	resource := resources[0]
	if resource.Action != ListenerRuleActionCreate {
		t.Fatalf("expected CREATE, got %s", resource.Action)
	}
	if resource.Priority != 202 {
		t.Fatalf("expected next free priority 202, got %d", resource.Priority)
	}
	if resource.TargetGroupArn != targetGroupArns[0] {
		t.Fatalf("expected target group %q, got %q", targetGroupArns[0], resource.TargetGroupArn)
	}
	if resource.Current != nil {
		t.Fatal("different manifest rule must not be treated as current")
	}
}

func TestBuildResourcesWithExisting_CascadesShiftInDesiredPriorityOrder(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:eu-west-1:123:listener/app/alb/abc"
	manifestRules := []config.IngressRule{
		{
			Priority: 201,
			Host:     "calingo-admin.sandbox.eu.cloudinsurance.app",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "nginx",
				ContainerPort: 80,
			},
		},
		{
			Priority: 200,
			Host:     "calingo.sandbox.eu.cloudinsurance.app",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "nginx",
				ContainerPort: 80,
			},
		},
	}

	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:rule-mdn"),
			Priority: aws.String("200"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"maiden.sandbox.eu.cloudinsurance.app"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/mdn-r200/abc")},
			},
		},
	}

	targetGroupArns := map[int]string{
		0: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r201/abc",
		1: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r200/abc",
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, "cal", nil, nil)

	if len(resources) != 2 {
		t.Fatalf("expected two desired listener rule resources, got %d", len(resources))
	}

	prioritiesByHost := map[string]int{}
	for _, resource := range resources {
		prioritiesByHost[resource.Desired.Host] = resource.Priority
	}

	if prioritiesByHost["calingo.sandbox.eu.cloudinsurance.app"] != 201 {
		t.Fatalf("expected occupied priority 200 to move to 201, got %d", prioritiesByHost["calingo.sandbox.eu.cloudinsurance.app"])
	}
	if prioritiesByHost["calingo-admin.sandbox.eu.cloudinsurance.app"] != 202 {
		t.Fatalf("expected requested priority 201 to shift with the tenant block to 202, got %d", prioritiesByHost["calingo-admin.sandbox.eu.cloudinsurance.app"])
	}
}

func TestBuildResourcesWithExisting_ShiftsThirdOverlappingTenantBlock(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:eu-west-1:123:listener/app/alb/abc"
	manifestRules := []config.IngressRule{
		testIngressRule(200, "calingo.sandbox.eu.cloudinsurance.app"),
		testIngressRule(201, "calingo-admin.sandbox.eu.cloudinsurance.app"),
		testIngressRule(202, "calingo-api.sandbox.eu.cloudinsurance.app"),
	}

	existingRules := []types.Rule{
		testForwardRule("arn:rule-mdn-main", 200, "maiden.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/mdn-r200/abc"),
		testForwardRule("arn:rule-mdn-admin", 201, "maiden-admin.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/mdn-r201/abc"),
		testForwardRule("arn:rule-bal-main", 202, "balder.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/bal-r200/abc"),
		testForwardRule("arn:rule-bal-admin", 203, "balder-admin.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/bal-r201/abc"),
	}

	targetGroupArns := map[int]string{
		0: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r200/abc",
		1: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r201/abc",
		2: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r202/abc",
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, "cal", nil, nil)

	if len(resources) != 3 {
		t.Fatalf("expected three desired listener rule resources, got %d", len(resources))
	}

	prioritiesByHost := map[string]int{}
	actionsByHost := map[string]ListenerRuleAction{}
	for _, resource := range resources {
		prioritiesByHost[resource.Desired.Host] = resource.Priority
		actionsByHost[resource.Desired.Host] = resource.Action
		if resource.Current != nil {
			t.Fatalf("expected %s to be created, got matched current rule %s", resource.Desired.Host, resource.Arn)
		}
	}

	expectedPriorities := map[string]int{
		"calingo.sandbox.eu.cloudinsurance.app":       204,
		"calingo-admin.sandbox.eu.cloudinsurance.app": 205,
		"calingo-api.sandbox.eu.cloudinsurance.app":   206,
	}
	for host, expected := range expectedPriorities {
		if prioritiesByHost[host] != expected {
			t.Fatalf("expected %s to shift to priority %d, got %d", host, expected, prioritiesByHost[host])
		}
		if actionsByHost[host] != ListenerRuleActionCreate {
			t.Fatalf("expected %s to be CREATE, got %s", host, actionsByHost[host])
		}
	}
}

func TestBuildResourcesWithExisting_KeepsShiftedPrioritiesOnRerun(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:eu-west-1:123:listener/app/alb/abc"
	manifestRules := []config.IngressRule{
		testIngressRule(200, "calingo.sandbox.eu.cloudinsurance.app"),
		testIngressRule(201, "calingo-admin.sandbox.eu.cloudinsurance.app"),
	}

	existingRules := []types.Rule{
		testForwardRule("arn:rule-mdn-main", 200, "maiden.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/mdn-r200/abc"),
		testForwardRule("arn:rule-mdn-admin", 201, "maiden-admin.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/mdn-r201/abc"),
		testForwardRule("arn:rule-bal-main", 202, "balder.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/bal-r200/abc"),
		testForwardRule("arn:rule-bal-admin", 203, "balder-admin.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/bal-r201/abc"),
		testForwardRule("arn:rule-cal-main", 204, "calingo.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r200/abc"),
		testForwardRule("arn:rule-cal-admin", 205, "calingo-admin.sandbox.eu.cloudinsurance.app", "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r201/abc"),
	}

	targetGroupArns := map[int]string{
		0: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r200/abc",
		1: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r201/abc",
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, "cal", nil, nil)

	if len(resources) != 2 {
		t.Fatalf("expected two desired listener rule resources, got %d", len(resources))
	}

	prioritiesByHost := map[string]int{}
	actionsByHost := map[string]ListenerRuleAction{}
	for _, resource := range resources {
		prioritiesByHost[resource.Desired.Host] = resource.Priority
		actionsByHost[resource.Desired.Host] = resource.Action
		if resource.Current == nil {
			t.Fatalf("expected %s to match the existing shifted rule", resource.Desired.Host)
		}
	}

	expectedPriorities := map[string]int{
		"calingo.sandbox.eu.cloudinsurance.app":       204,
		"calingo-admin.sandbox.eu.cloudinsurance.app": 205,
	}
	for host, expected := range expectedPriorities {
		if prioritiesByHost[host] != expected {
			t.Fatalf("expected %s to keep priority %d, got %d", host, expected, prioritiesByHost[host])
		}
		if actionsByHost[host] != ListenerRuleActionNoop {
			t.Fatalf("expected %s to be NOOP on rerun, got %s", host, actionsByHost[host])
		}
	}
}

func TestBuildResourcesWithExisting_MatchesOwnPriorityRuleForHostUpdate(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:eu-west-1:123:listener/app/alb/abc"
	manifestRules := []config.IngressRule{
		{
			Priority: 200,
			Host:     "new.calingo.example.com",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "nginx",
				ContainerPort: 80,
			},
		},
	}

	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:rule-cal"),
			Priority: aws.String("200"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"old.calingo.example.com"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r200/abc")},
			},
		},
	}

	targetGroupArns := map[int]string{
		0: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r200/abc",
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, "cal", nil, nil)

	if len(resources) != 1 {
		t.Fatalf("expected one desired listener rule resource, got %d", len(resources))
	}
	resource := resources[0]
	if resource.Current == nil {
		t.Fatal("expected existing cal rule to be matched by priority")
	}
	if resource.Action != ListenerRuleActionUpdate {
		t.Fatalf("expected UPDATE for host change on own rule, got %s", resource.Action)
	}
	if resource.Arn != "arn:rule-cal" {
		t.Fatalf("expected arn:rule-cal, got %s", resource.Arn)
	}
}

func TestBuildResourcesWithExisting_ReusesExistingRulePriorityWhenMatchedByConditions(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:eu-west-1:123:listener/app/alb/abc"
	manifestRules := []config.IngressRule{
		{
			Priority: 200,
			Host:     "calingo.sandbox.eu.cloudinsurance.app",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "nginx",
				ContainerPort: 80,
			},
		},
	}

	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:rule-mdn"),
			Priority: aws.String("200"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"maiden.sandbox.eu.cloudinsurance.app"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/mdn-r200/abc")},
			},
		},
		{
			RuleArn:  aws.String("arn:rule-cal"),
			Priority: aws.String("201"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"calingo.sandbox.eu.cloudinsurance.app"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r200/abc")},
			},
		},
	}

	targetGroupArns := map[int]string{
		0: "arn:aws:elasticloadbalancing:eu-west-1:123:targetgroup/cal-r200/abc",
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, "cal", nil, nil)

	if len(resources) != 1 {
		t.Fatalf("expected one desired listener rule resource, got %d", len(resources))
	}
	resource := resources[0]
	if resource.Action != ListenerRuleActionNoop {
		t.Fatalf("expected NOOP for matched existing rule, got %s", resource.Action)
	}
	if resource.Priority != 201 {
		t.Fatalf("expected existing priority 201 to be retained, got %d", resource.Priority)
	}
	if resource.Arn != "arn:rule-cal" {
		t.Fatalf("expected arn:rule-cal, got %s", resource.Arn)
	}
}

func TestExtractTargetGroupName(t *testing.T) {
	tests := []struct {
		arn      string
		expected string
	}{
		{"arn:aws:elasticloadbalancing:us-east-1:123456789:targetgroup/my-tg/abc123", "my-tg"},
		{"arn:aws:elasticloadbalancing:eu-west-1:999:targetgroup/app-r100/xyz", "app-r100"},
		{"invalid-arn", "invalid-arn"},
	}

	for _, tt := range tests {
		result := extractTargetGroupName(tt.arn)
		if result != tt.expected {
			t.Errorf("extractTargetGroupName(%q) = %q, want %q", tt.arn, result, tt.expected)
		}
	}
}

func TestIsListenerRuleOwnedByManifest(t *testing.T) {
	tests := []struct {
		name         string
		tgName       string
		manifestName string
		expected     bool
	}{
		{"owned by manifest", "myapp-r100", "myapp", true},
		{"owned by manifest with dashes", "my-app-r200", "my-app", true},
		{"not owned - different prefix", "otherapp-r100", "myapp", false},
		{"not owned - no -r suffix", "myapp-100", "myapp", false},
		{"not owned - r without number", "myapp-rabc", "myapp", false},
		{"empty tg name", "", "myapp", false},
		{"empty manifest name allows all", "myapp-r100", "", true},
		{"partial match should fail", "myapp-r100-extra", "myapp", false},
		{"manifest name is prefix of tg", "myappservice-r100", "myapp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isListenerRuleOwnedByManifest(tt.tgName, tt.manifestName)
			if result != tt.expected {
				t.Errorf("isListenerRuleOwnedByManifest(%q, %q) = %v, want %v",
					tt.tgName, tt.manifestName, result, tt.expected)
			}
		})
	}
}

func TestBuildResourcesWithExisting_FiltersOrphansByOwnership(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:us-east-1:123:listener/app/my-alb/abc"
	manifestName := "myapp"

	// Manifest has one rule at priority 100
	manifestRules := []config.IngressRule{
		{
			Priority: 100,
			Host:     "example.com",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "app",
				ContainerPort: 80,
			},
		},
	}

	// AWS has three rules:
	// - Priority 100: in manifest (myapp-r100)
	// - Priority 200: orphaned, owned by this manifest (myapp-r200) - should be deleted
	// - Priority 300: orphaned, owned by different manifest (otherapp-r300) - should NOT be deleted
	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123:rule/abc/100"),
			Priority: aws.String("100"),
			Conditions: []types.RuleCondition{
				{
					Field: aws.String("host-header"),
					HostHeaderConfig: &types.HostHeaderConditionConfig{
						Values: []string{"example.com"},
					},
				},
			},
			Actions: []types.Action{
				{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/myapp-r100/abc"),
				},
			},
		},
		{
			RuleArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123:rule/abc/200"),
			Priority: aws.String("200"),
			Conditions: []types.RuleCondition{
				{
					Field: aws.String("host-header"),
					HostHeaderConfig: &types.HostHeaderConditionConfig{
						Values: []string{"orphaned.myapp.com"},
					},
				},
			},
			Actions: []types.Action{
				{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/myapp-r200/xyz"),
				},
			},
		},
		{
			RuleArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123:rule/abc/300"),
			Priority: aws.String("300"),
			Conditions: []types.RuleCondition{
				{
					Field: aws.String("host-header"),
					HostHeaderConfig: &types.HostHeaderConditionConfig{
						Values: []string{"other.example.com"},
					},
				},
			},
			Actions: []types.Action{
				{
					Type:           types.ActionTypeEnumForward,
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/otherapp-r300/def"),
				},
			},
		},
	}

	targetGroupArns := map[int]string{
		0: "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/myapp-r100/abc",
	}

	tgTags := map[string]map[string]string{
		"arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/myapp-r200/xyz": {
			TagKeyManagedBy: TagValueEcsmate,
		},
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, manifestName, nil, tgTags)

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources (1 manifest + 1 owned orphan), got %d", len(resources))
	}

	var manifestResource, orphanedResource *ListenerRuleResource
	for _, r := range resources {
		switch r.Priority {
		case 100:
			manifestResource = r
		case 200:
			orphanedResource = r
		case 300:
			t.Error("rule at priority 300 should not be included - it belongs to otherapp")
		}
	}

	if manifestResource == nil {
		t.Fatal("manifest rule resource not found")
	}
	if manifestResource.Action != ListenerRuleActionNoop {
		t.Errorf("expected manifest rule action NOOP, got %s", manifestResource.Action)
	}

	if orphanedResource == nil {
		t.Fatal("owned orphaned rule resource not found")
	}
	if orphanedResource.Action != ListenerRuleActionDelete {
		t.Errorf("expected owned orphaned rule action DELETE, got %s", orphanedResource.Action)
	}
}

func TestBuildResourcesWithExisting_FiltersOrphansByManifestTags(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:us-east-1:123:listener/app/my-alb/abc"
	manifestName := "cal"
	manifestTags := map[string]string{
		"Environment": "stage",
		"Tenant":      "cal",
	}

	manifestRules := []config.IngressRule{
		{
			Priority: 100,
			Host:     "cal.stage.example.com",
			Service: &config.IngressServiceBackend{
				Name:          "web",
				ContainerName: "app",
				ContainerPort: 80,
			},
		},
	}

	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:rule/100"),
			Priority: aws.String("100"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"cal.stage.example.com"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:tg/cal-r100/a")},
			},
		},
		{
			RuleArn:  aws.String("arn:rule/101"),
			Priority: aws.String("101"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"cal.stage.old.com"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:tg/cal-r101/b")},
			},
		},
		{
			RuleArn:  aws.String("arn:rule/102"),
			Priority: aws.String("102"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"cal.prod.example.com"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:tg/cal-r102/c")},
			},
		},
	}

	targetGroupArns := map[int]string{
		0: "arn:tg/cal-r100/a",
	}

	tgTags := map[string]map[string]string{
		"arn:tg/cal-r101/b": {
			TagKeyManagedBy: TagValueEcsmate,
			"Environment":   "stage",
			"Tenant":        "cal",
		},
		"arn:tg/cal-r102/c": {
			TagKeyManagedBy: TagValueEcsmate,
			"Environment":   "prod",
			"Tenant":        "cal",
		},
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, manifestName, manifestTags, tgTags)

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources (1 manifest + 1 matching orphan), got %d", len(resources))
	}

	for _, r := range resources {
		switch r.Priority {
		case 100:
			if r.Action != ListenerRuleActionNoop {
				t.Errorf("priority 100: expected NOOP, got %s", r.Action)
			}
		case 101:
			if r.Action != ListenerRuleActionDelete {
				t.Errorf("priority 101: expected DELETE (same env/tenant), got %s", r.Action)
			}
		case 102:
			t.Error("priority 102 should NOT be included - different Environment (prod vs stage)")
		}
	}
}

func TestBuildResourcesWithExisting_NoManifestNameDeletesAll(t *testing.T) {
	listenerArn := "arn:aws:elasticloadbalancing:us-east-1:123:listener/app/my-alb/abc"

	manifestRules := []config.IngressRule{}

	// Two orphaned rules from different "manifests"
	existingRules := []types.Rule{
		{
			RuleArn:  aws.String("arn:rule-100"),
			Priority: aws.String("100"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"a.com"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:tg/myapp-r100/x")},
			},
		},
		{
			RuleArn:  aws.String("arn:rule-200"),
			Priority: aws.String("200"),
			Conditions: []types.RuleCondition{
				{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"b.com"}}},
			},
			Actions: []types.Action{
				{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String("arn:tg/otherapp-r200/y")},
			},
		},
	}

	tgTags := map[string]map[string]string{
		"arn:tg/myapp-r100/x": {
			TagKeyManagedBy: TagValueEcsmate,
		},
		"arn:tg/otherapp-r200/y": {
			TagKeyManagedBy: TagValueEcsmate,
		},
	}

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, nil, existingRules, "", nil, tgTags)

	if len(resources) != 2 {
		t.Fatalf("expected 2 orphaned resources when no manifest name, got %d", len(resources))
	}

	for _, r := range resources {
		if r.Action != ListenerRuleActionDelete {
			t.Errorf("expected DELETE action for priority %d, got %s", r.Priority, r.Action)
		}
	}
}
