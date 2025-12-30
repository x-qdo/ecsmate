package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"

	"github.com/qdo/ecsmate/internal/log"
)

type AutoScalingClient struct {
	client *applicationautoscaling.Client
}

func NewAutoScalingClient(ctx context.Context, region string) (*AutoScalingClient, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &AutoScalingClient{
		client: applicationautoscaling.NewFromConfig(cfg),
	}, nil
}

// ServiceResourceID generates the resource ID for ECS service scaling
func ServiceResourceID(cluster, service string) string {
	return fmt.Sprintf("service/%s/%s", cluster, service)
}

// RegisterScalableTarget registers an ECS service as a scalable target
func (c *AutoScalingClient) RegisterScalableTarget(ctx context.Context, input *RegisterScalableTargetInput) error {
	resourceID := ServiceResourceID(input.Cluster, input.Service)
	log.Debug("registering scalable target", "resourceID", resourceID, "min", input.MinCapacity, "max", input.MaxCapacity)

	_, err := c.client.RegisterScalableTarget(ctx, &applicationautoscaling.RegisterScalableTargetInput{
		ServiceNamespace:  types.ServiceNamespaceEcs,
		ResourceId:        aws.String(resourceID),
		ScalableDimension: types.ScalableDimensionECSServiceDesiredCount,
		MinCapacity:       aws.Int32(int32(input.MinCapacity)),
		MaxCapacity:       aws.Int32(int32(input.MaxCapacity)),
		RoleARN:           input.RoleARN,
	})
	if err != nil {
		return fmt.Errorf("failed to register scalable target %s: %w", resourceID, err)
	}

	log.Info("registered scalable target", "resourceID", resourceID)
	return nil
}

// DeregisterScalableTarget removes a scalable target
func (c *AutoScalingClient) DeregisterScalableTarget(ctx context.Context, cluster, service string) error {
	resourceID := ServiceResourceID(cluster, service)
	log.Debug("deregistering scalable target", "resourceID", resourceID)

	_, err := c.client.DeregisterScalableTarget(ctx, &applicationautoscaling.DeregisterScalableTargetInput{
		ServiceNamespace:  types.ServiceNamespaceEcs,
		ResourceId:        aws.String(resourceID),
		ScalableDimension: types.ScalableDimensionECSServiceDesiredCount,
	})
	if err != nil {
		return fmt.Errorf("failed to deregister scalable target %s: %w", resourceID, err)
	}

	log.Info("deregistered scalable target", "resourceID", resourceID)
	return nil
}

// DescribeScalableTarget returns the current scalable target configuration
func (c *AutoScalingClient) DescribeScalableTarget(ctx context.Context, cluster, service string) (*ScalableTarget, error) {
	resourceID := ServiceResourceID(cluster, service)
	log.Debug("describing scalable target", "resourceID", resourceID)

	out, err := c.client.DescribeScalableTargets(ctx, &applicationautoscaling.DescribeScalableTargetsInput{
		ServiceNamespace:  types.ServiceNamespaceEcs,
		ResourceIds:       []string{resourceID},
		ScalableDimension: types.ScalableDimensionECSServiceDesiredCount,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe scalable target %s: %w", resourceID, err)
	}

	if len(out.ScalableTargets) == 0 {
		return nil, nil
	}

	target := out.ScalableTargets[0]
	return &ScalableTarget{
		ResourceID:    aws.ToString(target.ResourceId),
		MinCapacity:   int(aws.ToInt32(target.MinCapacity)),
		MaxCapacity:   int(aws.ToInt32(target.MaxCapacity)),
		RoleARN:       aws.ToString(target.RoleARN),
		CreationTime:  aws.ToTime(target.CreationTime),
		SuspendedFrom: extractSuspendedActions(target.SuspendedState),
	}, nil
}

// PutTargetTrackingScalingPolicy creates or updates a target tracking scaling policy
func (c *AutoScalingClient) PutTargetTrackingScalingPolicy(ctx context.Context, input *TargetTrackingPolicyInput) error {
	resourceID := ServiceResourceID(input.Cluster, input.Service)
	policyName := fmt.Sprintf("%s-%s", input.Service, input.PolicyName)
	log.Debug("putting target tracking policy", "resourceID", resourceID, "policyName", policyName, "targetValue", input.TargetValue)

	policyInput := &applicationautoscaling.PutScalingPolicyInput{
		PolicyName:        aws.String(policyName),
		ServiceNamespace:  types.ServiceNamespaceEcs,
		ResourceId:        aws.String(resourceID),
		ScalableDimension: types.ScalableDimensionECSServiceDesiredCount,
		PolicyType:        types.PolicyTypeTargetTrackingScaling,
		TargetTrackingScalingPolicyConfiguration: &types.TargetTrackingScalingPolicyConfiguration{
			TargetValue: aws.Float64(input.TargetValue),
		},
	}

	ttConfig := policyInput.TargetTrackingScalingPolicyConfiguration

	if input.ScaleInCooldown > 0 {
		ttConfig.ScaleInCooldown = aws.Int32(int32(input.ScaleInCooldown))
	}
	if input.ScaleOutCooldown > 0 {
		ttConfig.ScaleOutCooldown = aws.Int32(int32(input.ScaleOutCooldown))
	}

	if input.DisableScaleIn {
		ttConfig.DisableScaleIn = aws.Bool(true)
	}

	if input.PredefinedMetric != "" {
		ttConfig.PredefinedMetricSpecification = &types.PredefinedMetricSpecification{
			PredefinedMetricType: types.MetricType(input.PredefinedMetric),
		}
		if input.PredefinedMetricResourceLabel != "" {
			ttConfig.PredefinedMetricSpecification.ResourceLabel = aws.String(input.PredefinedMetricResourceLabel)
		}
	} else if input.CustomMetric != nil {
		ttConfig.CustomizedMetricSpecification = &types.CustomizedMetricSpecification{
			MetricName: aws.String(input.CustomMetric.MetricName),
			Namespace:  aws.String(input.CustomMetric.Namespace),
			Statistic:  types.MetricStatistic(input.CustomMetric.Statistic),
		}
		for _, dim := range input.CustomMetric.Dimensions {
			ttConfig.CustomizedMetricSpecification.Dimensions = append(
				ttConfig.CustomizedMetricSpecification.Dimensions,
				types.MetricDimension{
					Name:  aws.String(dim.Name),
					Value: aws.String(dim.Value),
				},
			)
		}
	}

	_, err := c.client.PutScalingPolicy(ctx, policyInput)
	if err != nil {
		return fmt.Errorf("failed to put target tracking policy %s: %w", policyName, err)
	}

	log.Info("created/updated target tracking policy", "policyName", policyName, "targetValue", input.TargetValue)
	return nil
}

// PutStepScalingPolicy creates or updates a step scaling policy
func (c *AutoScalingClient) PutStepScalingPolicy(ctx context.Context, input *StepScalingPolicyInput) error {
	resourceID := ServiceResourceID(input.Cluster, input.Service)
	policyName := fmt.Sprintf("%s-%s", input.Service, input.PolicyName)
	log.Debug("putting step scaling policy", "resourceID", resourceID, "policyName", policyName)

	stepAdjustments := make([]types.StepAdjustment, len(input.StepAdjustments))
	for i, step := range input.StepAdjustments {
		adj := types.StepAdjustment{
			ScalingAdjustment: aws.Int32(int32(step.ScalingAdjustment)),
		}
		if step.MetricIntervalLowerBound != nil {
			adj.MetricIntervalLowerBound = step.MetricIntervalLowerBound
		}
		if step.MetricIntervalUpperBound != nil {
			adj.MetricIntervalUpperBound = step.MetricIntervalUpperBound
		}
		stepAdjustments[i] = adj
	}

	policyInput := &applicationautoscaling.PutScalingPolicyInput{
		PolicyName:        aws.String(policyName),
		ServiceNamespace:  types.ServiceNamespaceEcs,
		ResourceId:        aws.String(resourceID),
		ScalableDimension: types.ScalableDimensionECSServiceDesiredCount,
		PolicyType:        types.PolicyTypeStepScaling,
		StepScalingPolicyConfiguration: &types.StepScalingPolicyConfiguration{
			AdjustmentType:         types.AdjustmentType(input.AdjustmentType),
			StepAdjustments:        stepAdjustments,
			Cooldown:               aws.Int32(int32(input.Cooldown)),
			MetricAggregationType:  types.MetricAggregationType(input.MetricAggregationType),
			MinAdjustmentMagnitude: aws.Int32(int32(input.MinAdjustmentMagnitude)),
		},
	}

	result, err := c.client.PutScalingPolicy(ctx, policyInput)
	if err != nil {
		return fmt.Errorf("failed to put step scaling policy %s: %w", policyName, err)
	}

	log.Info("created/updated step scaling policy", "policyName", policyName, "policyARN", aws.ToString(result.PolicyARN))
	return nil
}

// DeleteScalingPolicy removes a scaling policy
func (c *AutoScalingClient) DeleteScalingPolicy(ctx context.Context, cluster, service, policyName string) error {
	resourceID := ServiceResourceID(cluster, service)
	fullPolicyName := fmt.Sprintf("%s-%s", service, policyName)
	log.Debug("deleting scaling policy", "resourceID", resourceID, "policyName", fullPolicyName)

	_, err := c.client.DeleteScalingPolicy(ctx, &applicationautoscaling.DeleteScalingPolicyInput{
		PolicyName:        aws.String(fullPolicyName),
		ServiceNamespace:  types.ServiceNamespaceEcs,
		ResourceId:        aws.String(resourceID),
		ScalableDimension: types.ScalableDimensionECSServiceDesiredCount,
	})
	if err != nil {
		return fmt.Errorf("failed to delete scaling policy %s: %w", fullPolicyName, err)
	}

	log.Info("deleted scaling policy", "policyName", fullPolicyName)
	return nil
}

// DescribeScalingPolicies returns all scaling policies for a service
func (c *AutoScalingClient) DescribeScalingPolicies(ctx context.Context, cluster, service string) ([]ScalingPolicyInfo, error) {
	resourceID := ServiceResourceID(cluster, service)
	log.Debug("describing scaling policies", "resourceID", resourceID)

	var policies []ScalingPolicyInfo
	paginator := applicationautoscaling.NewDescribeScalingPoliciesPaginator(c.client, &applicationautoscaling.DescribeScalingPoliciesInput{
		ServiceNamespace:  types.ServiceNamespaceEcs,
		ResourceId:        aws.String(resourceID),
		ScalableDimension: types.ScalableDimensionECSServiceDesiredCount,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe scaling policies: %w", err)
		}

		for _, policy := range page.ScalingPolicies {
			info := ScalingPolicyInfo{
				PolicyName: aws.ToString(policy.PolicyName),
				PolicyARN:  aws.ToString(policy.PolicyARN),
				PolicyType: string(policy.PolicyType),
				ResourceID: aws.ToString(policy.ResourceId),
			}

			if policy.TargetTrackingScalingPolicyConfiguration != nil {
				info.TargetValue = aws.ToFloat64(policy.TargetTrackingScalingPolicyConfiguration.TargetValue)
			}

			policies = append(policies, info)
		}
	}

	return policies, nil
}

// PutScheduledAction creates or updates a scheduled scaling action
func (c *AutoScalingClient) PutScheduledAction(ctx context.Context, input *ScheduledActionInput) error {
	resourceID := ServiceResourceID(input.Cluster, input.Service)
	log.Debug("putting scheduled action", "resourceID", resourceID, "actionName", input.ActionName)

	actionInput := &applicationautoscaling.PutScheduledActionInput{
		ScheduledActionName: aws.String(input.ActionName),
		ServiceNamespace:    types.ServiceNamespaceEcs,
		ResourceId:          aws.String(resourceID),
		ScalableDimension:   types.ScalableDimensionECSServiceDesiredCount,
		Schedule:            aws.String(input.Schedule),
		ScalableTargetAction: &types.ScalableTargetAction{
			MinCapacity: aws.Int32(int32(input.MinCapacity)),
			MaxCapacity: aws.Int32(int32(input.MaxCapacity)),
		},
	}

	if input.Timezone != "" {
		actionInput.Timezone = aws.String(input.Timezone)
	}
	if !input.StartTime.IsZero() {
		actionInput.StartTime = aws.Time(input.StartTime)
	}
	if !input.EndTime.IsZero() {
		actionInput.EndTime = aws.Time(input.EndTime)
	}

	_, err := c.client.PutScheduledAction(ctx, actionInput)
	if err != nil {
		return fmt.Errorf("failed to put scheduled action %s: %w", input.ActionName, err)
	}

	log.Info("created/updated scheduled action", "actionName", input.ActionName, "schedule", input.Schedule)
	return nil
}

// DeleteScheduledAction removes a scheduled scaling action
func (c *AutoScalingClient) DeleteScheduledAction(ctx context.Context, cluster, service, actionName string) error {
	resourceID := ServiceResourceID(cluster, service)
	log.Debug("deleting scheduled action", "resourceID", resourceID, "actionName", actionName)

	_, err := c.client.DeleteScheduledAction(ctx, &applicationautoscaling.DeleteScheduledActionInput{
		ScheduledActionName: aws.String(actionName),
		ServiceNamespace:    types.ServiceNamespaceEcs,
		ResourceId:          aws.String(resourceID),
		ScalableDimension:   types.ScalableDimensionECSServiceDesiredCount,
	})
	if err != nil {
		return fmt.Errorf("failed to delete scheduled action %s: %w", actionName, err)
	}

	log.Info("deleted scheduled action", "actionName", actionName)
	return nil
}

// DescribeScheduledActions returns all scheduled actions for a service
func (c *AutoScalingClient) DescribeScheduledActions(ctx context.Context, cluster, service string) ([]ScheduledActionInfo, error) {
	resourceID := ServiceResourceID(cluster, service)
	log.Debug("describing scheduled actions", "resourceID", resourceID)

	var actions []ScheduledActionInfo
	paginator := applicationautoscaling.NewDescribeScheduledActionsPaginator(c.client, &applicationautoscaling.DescribeScheduledActionsInput{
		ServiceNamespace:  types.ServiceNamespaceEcs,
		ResourceId:        aws.String(resourceID),
		ScalableDimension: types.ScalableDimensionECSServiceDesiredCount,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe scheduled actions: %w", err)
		}

		for _, action := range page.ScheduledActions {
			info := ScheduledActionInfo{
				ActionName:  aws.ToString(action.ScheduledActionName),
				ActionARN:   aws.ToString(action.ScheduledActionARN),
				Schedule:    aws.ToString(action.Schedule),
				ResourceID:  aws.ToString(action.ResourceId),
				MinCapacity: int(aws.ToInt32(action.ScalableTargetAction.MinCapacity)),
				MaxCapacity: int(aws.ToInt32(action.ScalableTargetAction.MaxCapacity)),
			}
			if action.StartTime != nil {
				info.StartTime = aws.ToTime(action.StartTime)
			}
			if action.EndTime != nil {
				info.EndTime = aws.ToTime(action.EndTime)
			}
			actions = append(actions, info)
		}
	}

	return actions, nil
}

func extractSuspendedActions(state *types.SuspendedState) []string {
	if state == nil {
		return nil
	}
	var suspended []string
	if state.DynamicScalingInSuspended != nil && *state.DynamicScalingInSuspended {
		suspended = append(suspended, "DynamicScalingIn")
	}
	if state.DynamicScalingOutSuspended != nil && *state.DynamicScalingOutSuspended {
		suspended = append(suspended, "DynamicScalingOut")
	}
	if state.ScheduledScalingSuspended != nil && *state.ScheduledScalingSuspended {
		suspended = append(suspended, "ScheduledScaling")
	}
	return suspended
}
