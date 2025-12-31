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
