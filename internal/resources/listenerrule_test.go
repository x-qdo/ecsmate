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
