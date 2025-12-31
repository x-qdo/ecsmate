package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/qdo/ecsmate/internal/log"
)

type ELBV2Client struct {
	client *elasticloadbalancingv2.Client
}

func NewELBV2Client(ctx context.Context, region string) (*ELBV2Client, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &ELBV2Client{
		client: elasticloadbalancingv2.NewFromConfig(cfg),
	}, nil
}

type CreateTargetGroupInput struct {
	Name                string
	Protocol            string
	Port                int
	VpcID               string
	TargetType          string
	HealthCheckPath     string
	HealthCheckProtocol string
	HealthCheckPort     string
	HealthyThreshold    int
	UnhealthyThreshold  int
	Timeout             int
	Interval            int
	Matcher             string
	DeregistrationDelay int
	Tags                map[string]string
}

func (c *ELBV2Client) CreateTargetGroup(ctx context.Context, input *CreateTargetGroupInput) (*types.TargetGroup, error) {
	log.Debug("creating target group", "name", input.Name)

	createInput := &elasticloadbalancingv2.CreateTargetGroupInput{
		Name:       aws.String(input.Name),
		Protocol:   types.ProtocolEnum(input.Protocol),
		Port:       aws.Int32(int32(input.Port)),
		VpcId:      aws.String(input.VpcID),
		TargetType: types.TargetTypeEnum(input.TargetType),
	}

	if input.HealthCheckPath != "" {
		createInput.HealthCheckPath = aws.String(input.HealthCheckPath)
	}
	if input.HealthCheckProtocol != "" {
		createInput.HealthCheckProtocol = types.ProtocolEnum(input.HealthCheckProtocol)
	}
	if input.HealthCheckPort != "" {
		createInput.HealthCheckPort = aws.String(input.HealthCheckPort)
	}
	if input.HealthyThreshold > 0 {
		createInput.HealthyThresholdCount = aws.Int32(int32(input.HealthyThreshold))
	}
	if input.UnhealthyThreshold > 0 {
		createInput.UnhealthyThresholdCount = aws.Int32(int32(input.UnhealthyThreshold))
	}
	if input.Timeout > 0 {
		createInput.HealthCheckTimeoutSeconds = aws.Int32(int32(input.Timeout))
	}
	if input.Interval > 0 {
		createInput.HealthCheckIntervalSeconds = aws.Int32(int32(input.Interval))
	}
	if input.Matcher != "" {
		createInput.Matcher = &types.Matcher{
			HttpCode: aws.String(input.Matcher),
		}
	}

	out, err := c.client.CreateTargetGroup(ctx, createInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create target group %s: %w", input.Name, err)
	}

	if len(out.TargetGroups) == 0 {
		return nil, fmt.Errorf("no target group returned after creation")
	}

	tg := &out.TargetGroups[0]

	// Set deregistration delay if specified
	if input.DeregistrationDelay > 0 {
		_, err := c.client.ModifyTargetGroupAttributes(ctx, &elasticloadbalancingv2.ModifyTargetGroupAttributesInput{
			TargetGroupArn: tg.TargetGroupArn,
			Attributes: []types.TargetGroupAttribute{
				{
					Key:   aws.String("deregistration_delay.timeout_seconds"),
					Value: aws.String(fmt.Sprintf("%d", input.DeregistrationDelay)),
				},
			},
		})
		if err != nil {
			log.Warn("failed to set deregistration delay", "error", err)
		}
	}

	// Add tags
	if len(input.Tags) > 0 {
		tags := make([]types.Tag, 0, len(input.Tags))
		for k, v := range input.Tags {
			tags = append(tags, types.Tag{
				Key:   aws.String(k),
				Value: aws.String(v),
			})
		}
		_, err := c.client.AddTags(ctx, &elasticloadbalancingv2.AddTagsInput{
			ResourceArns: []string{aws.ToString(tg.TargetGroupArn)},
			Tags:         tags,
		})
		if err != nil {
			log.Warn("failed to add tags to target group", "error", err)
		}
	}

	log.Info("created target group", "name", input.Name, "arn", aws.ToString(tg.TargetGroupArn))
	return tg, nil
}

func (c *ELBV2Client) DescribeTargetGroup(ctx context.Context, name string) (*types.TargetGroup, error) {
	log.Debug("describing target group", "name", name)

	out, err := c.client.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{
		Names: []string{name},
	})
	if err != nil {
		var notFound *types.TargetGroupNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to describe target group %s: %w", name, err)
	}

	if len(out.TargetGroups) == 0 {
		return nil, nil
	}

	return &out.TargetGroups[0], nil
}

func (c *ELBV2Client) DescribeTargetGroupByArn(ctx context.Context, arn string) (*types.TargetGroup, error) {
	log.Debug("describing target group by ARN", "arn", arn)

	out, err := c.client.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{
		TargetGroupArns: []string{arn},
	})
	if err != nil {
		var notFound *types.TargetGroupNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to describe target group %s: %w", arn, err)
	}

	if len(out.TargetGroups) == 0 {
		return nil, nil
	}

	return &out.TargetGroups[0], nil
}

func (c *ELBV2Client) DeleteTargetGroup(ctx context.Context, arn string) error {
	log.Debug("deleting target group", "arn", arn)

	_, err := c.client.DeleteTargetGroup(ctx, &elasticloadbalancingv2.DeleteTargetGroupInput{
		TargetGroupArn: aws.String(arn),
	})
	if err != nil {
		var notFound *types.TargetGroupNotFoundException
		if errors.As(err, &notFound) {
			log.Debug("target group not found", "arn", arn)
			return nil
		}
		return fmt.Errorf("failed to delete target group %s: %w", arn, err)
	}

	log.Info("deleted target group", "arn", arn)
	return nil
}

type CreateListenerRuleInput struct {
	ListenerArn string
	Priority    int
	Conditions  []RuleConditionInput
	Actions     []RuleActionInput
}

type RuleConditionInput struct {
	Type          string
	Values        []string
	Name          string // for http-header
	KeyValuePairs []QueryStringKV
}

type QueryStringKV struct {
	Key   string
	Value string
}

type RuleActionInput struct {
	Type           string
	TargetGroupArn string

	// For redirect
	StatusCode string
	Protocol   string
	Host       string
	Port       string
	Path       string
	Query      string

	// For fixed-response
	ContentType string
	MessageBody string
}

func (c *ELBV2Client) CreateListenerRule(ctx context.Context, input *CreateListenerRuleInput) (*types.Rule, error) {
	log.Debug("creating listener rule", "priority", input.Priority)

	conditions := make([]types.RuleCondition, 0, len(input.Conditions))
	for _, cond := range input.Conditions {
		rc := types.RuleCondition{}
		switch cond.Type {
		case "host-header":
			rc.Field = aws.String("host-header")
			rc.HostHeaderConfig = &types.HostHeaderConditionConfig{
				Values: cond.Values,
			}
		case "path-pattern":
			rc.Field = aws.String("path-pattern")
			rc.PathPatternConfig = &types.PathPatternConditionConfig{
				Values: cond.Values,
			}
		case "http-header":
			rc.Field = aws.String("http-header")
			rc.HttpHeaderConfig = &types.HttpHeaderConditionConfig{
				HttpHeaderName: aws.String(cond.Name),
				Values:         cond.Values,
			}
		case "query-string":
			rc.Field = aws.String("query-string")
			kvConfigs := make([]types.QueryStringKeyValuePair, 0, len(cond.KeyValuePairs))
			for _, kv := range cond.KeyValuePairs {
				kvConfigs = append(kvConfigs, types.QueryStringKeyValuePair{
					Key:   aws.String(kv.Key),
					Value: aws.String(kv.Value),
				})
			}
			rc.QueryStringConfig = &types.QueryStringConditionConfig{
				Values: kvConfigs,
			}
		}
		conditions = append(conditions, rc)
	}

	actions := make([]types.Action, 0, len(input.Actions))
	for i, act := range input.Actions {
		ra := types.Action{
			Order: aws.Int32(int32(i + 1)),
		}
		switch act.Type {
		case "forward":
			ra.Type = types.ActionTypeEnumForward
			ra.TargetGroupArn = aws.String(act.TargetGroupArn)
		case "redirect":
			ra.Type = types.ActionTypeEnumRedirect
			ra.RedirectConfig = &types.RedirectActionConfig{
				StatusCode: types.RedirectActionStatusCodeEnum(act.StatusCode),
			}
			if act.Protocol != "" {
				ra.RedirectConfig.Protocol = aws.String(act.Protocol)
			}
			if act.Host != "" {
				ra.RedirectConfig.Host = aws.String(act.Host)
			}
			if act.Port != "" {
				ra.RedirectConfig.Port = aws.String(act.Port)
			}
			if act.Path != "" {
				ra.RedirectConfig.Path = aws.String(act.Path)
			}
			if act.Query != "" {
				ra.RedirectConfig.Query = aws.String(act.Query)
			}
		case "fixed-response":
			ra.Type = types.ActionTypeEnumFixedResponse
			ra.FixedResponseConfig = &types.FixedResponseActionConfig{
				StatusCode: aws.String(act.StatusCode),
			}
			if act.ContentType != "" {
				ra.FixedResponseConfig.ContentType = aws.String(act.ContentType)
			}
			if act.MessageBody != "" {
				ra.FixedResponseConfig.MessageBody = aws.String(act.MessageBody)
			}
		}
		actions = append(actions, ra)
	}

	out, err := c.client.CreateRule(ctx, &elasticloadbalancingv2.CreateRuleInput{
		ListenerArn: aws.String(input.ListenerArn),
		Priority:    aws.Int32(int32(input.Priority)),
		Conditions:  conditions,
		Actions:     actions,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create listener rule: %w", err)
	}

	if len(out.Rules) == 0 {
		return nil, fmt.Errorf("no rule returned after creation")
	}

	log.Info("created listener rule", "priority", input.Priority, "arn", aws.ToString(out.Rules[0].RuleArn))
	return &out.Rules[0], nil
}

func (c *ELBV2Client) DescribeListenerRules(ctx context.Context, listenerArn string) ([]types.Rule, error) {
	log.Debug("describing listener rules", "listener", listenerArn)

	var rules []types.Rule
	paginator := elasticloadbalancingv2.NewDescribeRulesPaginator(c.client, &elasticloadbalancingv2.DescribeRulesInput{
		ListenerArn: aws.String(listenerArn),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe listener rules: %w", err)
		}
		rules = append(rules, page.Rules...)
	}

	return rules, nil
}

func (c *ELBV2Client) DeleteListenerRule(ctx context.Context, ruleArn string) error {
	log.Debug("deleting listener rule", "arn", ruleArn)

	_, err := c.client.DeleteRule(ctx, &elasticloadbalancingv2.DeleteRuleInput{
		RuleArn: aws.String(ruleArn),
	})
	if err != nil {
		var notFound *types.RuleNotFoundException
		if errors.As(err, &notFound) {
			log.Debug("listener rule not found", "arn", ruleArn)
			return nil
		}
		return fmt.Errorf("failed to delete listener rule %s: %w", ruleArn, err)
	}

	log.Info("deleted listener rule", "arn", ruleArn)
	return nil
}

func (c *ELBV2Client) ModifyListenerRule(ctx context.Context, ruleArn string, conditions []types.RuleCondition, actions []types.Action) error {
	log.Debug("modifying listener rule", "arn", ruleArn)

	_, err := c.client.ModifyRule(ctx, &elasticloadbalancingv2.ModifyRuleInput{
		RuleArn:    aws.String(ruleArn),
		Conditions: conditions,
		Actions:    actions,
	})
	if err != nil {
		return fmt.Errorf("failed to modify listener rule %s: %w", ruleArn, err)
	}

	log.Info("modified listener rule", "arn", ruleArn)
	return nil
}
