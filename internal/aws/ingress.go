package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// IngressAPI is the load balancer surface used by ingress discovery and execution.
type IngressAPI interface {
	DescribeListenerRules(context.Context, string) ([]types.Rule, error)
	DescribeTargetGroup(context.Context, string) (*types.TargetGroup, error)
	DescribeTargetGroupByArn(context.Context, string) (*types.TargetGroup, error)
	DescribeTags(context.Context, []string) (map[string]map[string]string, error)
	CreateTargetGroup(context.Context, *CreateTargetGroupInput) (*types.TargetGroup, error)
	ModifyTargetGroup(context.Context, string, *ModifyTargetGroupInput) error
	DeleteTargetGroup(context.Context, string) error
	CreateListenerRule(context.Context, *CreateListenerRuleInput) (*types.Rule, error)
	ModifyListenerRule(context.Context, string, []types.RuleCondition, []types.Action) error
	DeleteListenerRule(context.Context, string) error
}

var _ IngressAPI = (*ELBV2Client)(nil)
