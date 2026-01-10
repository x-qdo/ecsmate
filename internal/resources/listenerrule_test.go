package resources

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/qdo/ecsmate/internal/config"
)

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

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, manifestName)

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
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, nil, existingRules, "myapp")

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

	matches, usedArns := matchExistingListenerRulesWithUsed(desiredRules, existingRules)

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

	mgr := &ListenerRuleManager{}
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, targetGroupArns, existingRules, manifestName)

	// Should have 2 resources: manifest rule (NOOP) + owned orphan (DELETE)
	// The otherapp-r300 rule should NOT be included
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

	mgr := &ListenerRuleManager{}

	// Without manifest name, all orphans should be deleted (backwards compatible)
	resources := mgr.BuildResourcesWithExisting(listenerArn, manifestRules, nil, existingRules, "")

	if len(resources) != 2 {
		t.Fatalf("expected 2 orphaned resources when no manifest name, got %d", len(resources))
	}

	for _, r := range resources {
		if r.Action != ListenerRuleActionDelete {
			t.Errorf("expected DELETE action for priority %d, got %s", r.Priority, r.Action)
		}
	}
}
