package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"
	"github.com/aws/aws-sdk-go-v2/service/scheduler/types"

	awsclient "github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/log"
)

type ScheduledTaskAction string

const (
	ScheduledTaskActionCreate ScheduledTaskAction = "CREATE"
	ScheduledTaskActionUpdate ScheduledTaskAction = "UPDATE"
	ScheduledTaskActionDelete ScheduledTaskAction = "DELETE"
	ScheduledTaskActionNoop   ScheduledTaskAction = "NOOP"
)

type ScheduledTaskResource struct {
	Name    string
	Desired *config.ScheduledTask
	Current *types.ScheduleSummary
	Action  ScheduledTaskAction

	TaskDefinitionArn string
	RoleArn           string
	PropagationReason string // Set when action was propagated from dependency
}

func (r *ScheduledTaskResource) ScheduleExpression() string {
	if r.Desired == nil {
		return ""
	}

	switch r.Desired.ScheduleType {
	case "cron":
		return fmt.Sprintf("cron(%s)", r.Desired.ScheduleExpression)
	case "rate":
		return fmt.Sprintf("rate(%s)", r.Desired.ScheduleExpression)
	default:
		return r.Desired.ScheduleExpression
	}
}

func (r *ScheduledTaskResource) ToCreateInput(groupName string) (*scheduler.CreateScheduleInput, error) {
	if r.Desired == nil {
		return nil, fmt.Errorf("no desired state for scheduled task %s", r.Name)
	}

	task := r.Desired

	ecsParams := &types.EcsParameters{
		TaskDefinitionArn: aws.String(r.TaskDefinitionArn),
		TaskCount:         aws.Int32(int32(task.TaskCount)),
	}

	if task.LaunchType != "" {
		ecsParams.LaunchType = types.LaunchType(task.LaunchType)
	}

	if task.PlatformVersion != "" {
		ecsParams.PlatformVersion = aws.String(task.PlatformVersion)
	}
	if task.Group != "" {
		ecsParams.Group = aws.String(task.Group)
	}

	if task.NetworkConfiguration != nil {
		ecsParams.NetworkConfiguration = &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        task.NetworkConfiguration.Subnets,
				SecurityGroups: task.NetworkConfiguration.SecurityGroups,
			},
		}
		if task.NetworkConfiguration.AssignPublicIp != "" {
			ecsParams.NetworkConfiguration.AwsvpcConfiguration.AssignPublicIp = types.AssignPublicIp(task.NetworkConfiguration.AssignPublicIp)
		}
	}

	target := &types.Target{
		Arn:           aws.String(fmt.Sprintf("arn:aws:ecs:%s:cluster/%s", getRegionFromCluster(task.Cluster), task.Cluster)),
		RoleArn:       aws.String(r.RoleArn),
		EcsParameters: ecsParams,
	}

	if inputJSON := r.buildOverridesInput(); inputJSON != "" {
		target.Input = aws.String(inputJSON)
	}

	if task.DeadLetterConfig != nil && task.DeadLetterConfig.Arn != "" {
		target.DeadLetterConfig = &types.DeadLetterConfig{
			Arn: aws.String(task.DeadLetterConfig.Arn),
		}
	}
	if task.RetryPolicy != nil {
		target.RetryPolicy = &types.RetryPolicy{
			MaximumEventAgeInSeconds: aws.Int32(int32(task.RetryPolicy.MaximumEventAgeInSeconds)),
			MaximumRetryAttempts:     aws.Int32(int32(task.RetryPolicy.MaximumRetryAttempts)),
		}
	}

	input := &scheduler.CreateScheduleInput{
		Name:               aws.String(r.Name),
		GroupName:          aws.String(groupName),
		ScheduleExpression: aws.String(r.ScheduleExpression()),
		Target:             target,
		FlexibleTimeWindow: &types.FlexibleTimeWindow{
			Mode: types.FlexibleTimeWindowModeOff,
		},
		State: types.ScheduleStateEnabled,
	}

	if task.Timezone != "" {
		input.ScheduleExpressionTimezone = aws.String(task.Timezone)
	}

	return input, nil
}

func (r *ScheduledTaskResource) ToUpdateInput(groupName string) (*scheduler.UpdateScheduleInput, error) {
	if r.Desired == nil {
		return nil, fmt.Errorf("no desired state for scheduled task %s", r.Name)
	}

	task := r.Desired

	ecsParams := &types.EcsParameters{
		TaskDefinitionArn: aws.String(r.TaskDefinitionArn),
		TaskCount:         aws.Int32(int32(task.TaskCount)),
	}

	if task.LaunchType != "" {
		ecsParams.LaunchType = types.LaunchType(task.LaunchType)
	}

	if task.PlatformVersion != "" {
		ecsParams.PlatformVersion = aws.String(task.PlatformVersion)
	}
	if task.Group != "" {
		ecsParams.Group = aws.String(task.Group)
	}

	if task.NetworkConfiguration != nil {
		ecsParams.NetworkConfiguration = &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        task.NetworkConfiguration.Subnets,
				SecurityGroups: task.NetworkConfiguration.SecurityGroups,
			},
		}
		if task.NetworkConfiguration.AssignPublicIp != "" {
			ecsParams.NetworkConfiguration.AwsvpcConfiguration.AssignPublicIp = types.AssignPublicIp(task.NetworkConfiguration.AssignPublicIp)
		}
	}

	target := &types.Target{
		Arn:           aws.String(fmt.Sprintf("arn:aws:ecs:%s:cluster/%s", getRegionFromCluster(task.Cluster), task.Cluster)),
		RoleArn:       aws.String(r.RoleArn),
		EcsParameters: ecsParams,
	}

	if inputJSON := r.buildOverridesInput(); inputJSON != "" {
		target.Input = aws.String(inputJSON)
	}

	if task.DeadLetterConfig != nil && task.DeadLetterConfig.Arn != "" {
		target.DeadLetterConfig = &types.DeadLetterConfig{
			Arn: aws.String(task.DeadLetterConfig.Arn),
		}
	}
	if task.RetryPolicy != nil {
		target.RetryPolicy = &types.RetryPolicy{
			MaximumEventAgeInSeconds: aws.Int32(int32(task.RetryPolicy.MaximumEventAgeInSeconds)),
			MaximumRetryAttempts:     aws.Int32(int32(task.RetryPolicy.MaximumRetryAttempts)),
		}
	}

	input := &scheduler.UpdateScheduleInput{
		Name:               aws.String(r.Name),
		GroupName:          aws.String(groupName),
		ScheduleExpression: aws.String(r.ScheduleExpression()),
		Target:             target,
		FlexibleTimeWindow: &types.FlexibleTimeWindow{
			Mode: types.FlexibleTimeWindowModeOff,
		},
		State: types.ScheduleStateEnabled,
	}

	if task.Timezone != "" {
		input.ScheduleExpressionTimezone = aws.String(task.Timezone)
	}

	return input, nil
}

type ScheduledTaskManager struct {
	schedulerClient *awsclient.SchedulerClient
	groupName       string
}

func NewScheduledTaskManager(schedulerClient *awsclient.SchedulerClient, groupName string) *ScheduledTaskManager {
	if groupName == "" {
		groupName = "default"
	}
	return &ScheduledTaskManager{
		schedulerClient: schedulerClient,
		groupName:       groupName,
	}
}

func (m *ScheduledTaskManager) BuildResource(ctx context.Context, name string, task *config.ScheduledTask, taskDefArn, roleArn string) (*ScheduledTaskResource, error) {
	resource := &ScheduledTaskResource{
		Name:              name,
		Desired:           task,
		TaskDefinitionArn: taskDefArn,
		RoleArn:           roleArn,
	}

	if err := m.discoverScheduledTask(ctx, resource); err != nil {
		log.Debug("failed to discover scheduled task", "name", name, "error", err)
	}

	resource.determineAction()

	return resource, nil
}

func (m *ScheduledTaskManager) discoverScheduledTask(ctx context.Context, resource *ScheduledTaskResource) error {
	log.Debug("discovering scheduled task", "name", resource.Name, "group", m.groupName)

	schedules, err := m.schedulerClient.ListSchedules(ctx, m.groupName, resource.Name)
	if err != nil {
		return err
	}

	for _, schedule := range schedules {
		if aws.ToString(schedule.Name) == resource.Name {
			resource.Current = &schedule
			return nil
		}
	}

	return nil
}

func (resource *ScheduledTaskResource) determineAction() {
	if resource.Current == nil {
		resource.Action = ScheduledTaskActionCreate
		return
	}

	resource.Action = ScheduledTaskActionUpdate
}

func (m *ScheduledTaskManager) Create(ctx context.Context, resource *ScheduledTaskResource) error {
	input, err := resource.ToCreateInput(m.groupName)
	if err != nil {
		return err
	}

	if err := m.schedulerClient.CreateSchedule(ctx, input); err != nil {
		return err
	}

	return m.applyScheduleTags(ctx, resource)
}

func (m *ScheduledTaskManager) Update(ctx context.Context, resource *ScheduledTaskResource) error {
	input, err := resource.ToUpdateInput(m.groupName)
	if err != nil {
		return err
	}

	if err := m.schedulerClient.UpdateSchedule(ctx, input); err != nil {
		return err
	}

	return m.applyScheduleTags(ctx, resource)
}

func (m *ScheduledTaskManager) Delete(ctx context.Context, resource *ScheduledTaskResource) error {
	return m.schedulerClient.DeleteSchedule(ctx, resource.Name, m.groupName)
}

func (m *ScheduledTaskManager) Apply(ctx context.Context, resource *ScheduledTaskResource) error {
	switch resource.Action {
	case ScheduledTaskActionCreate:
		return m.Create(ctx, resource)
	case ScheduledTaskActionUpdate:
		return m.Update(ctx, resource)
	case ScheduledTaskActionDelete:
		return m.Delete(ctx, resource)
	case ScheduledTaskActionNoop:
		log.Debug("no changes detected, skipping scheduled task update", "name", resource.Name)
		return nil
	default:
		return fmt.Errorf("unknown action: %s", resource.Action)
	}
}

func (m *ScheduledTaskManager) applyScheduleTags(ctx context.Context, resource *ScheduledTaskResource) error {
	if resource == nil || resource.Desired == nil {
		return nil
	}
	if len(resource.Desired.Tags) == 0 {
		return nil
	}

	schedule, err := m.schedulerClient.GetSchedule(ctx, resource.Name, m.groupName)
	if err != nil {
		return err
	}
	arn := aws.ToString(schedule.Arn)
	if arn == "" {
		return nil
	}

	currentTags, err := m.schedulerClient.ListTagsForResource(ctx, arn)
	if err != nil {
		return err
	}

	desiredTags := make(map[string]string, len(resource.Desired.Tags))
	for _, tag := range resource.Desired.Tags {
		if tag.Key == "" {
			continue
		}
		desiredTags[tag.Key] = tag.Value
	}

	var addTags []types.Tag
	for key, value := range desiredTags {
		if current, ok := currentTags[key]; !ok || current != value {
			addTags = append(addTags, types.Tag{
				Key:   aws.String(key),
				Value: aws.String(value),
			})
		}
	}

	var removeKeys []string
	for key := range currentTags {
		if _, ok := desiredTags[key]; !ok {
			removeKeys = append(removeKeys, key)
		}
	}

	if err := m.schedulerClient.TagResource(ctx, arn, addTags); err != nil {
		return err
	}
	if err := m.schedulerClient.UntagResource(ctx, arn, removeKeys); err != nil {
		return err
	}

	return nil
}

func getRegionFromCluster(cluster string) string {
	if strings.HasPrefix(cluster, "arn:aws:ecs:") {
		parts := strings.Split(cluster, ":")
		if len(parts) >= 4 {
			return parts[3]
		}
	}
	return "us-east-1"
}

type ecsTaskOverrideInput struct {
	ContainerOverrides []ecsContainerOverride `json:"containerOverrides,omitempty"`
	CPU                string                 `json:"cpu,omitempty"`
	Memory             string                 `json:"memory,omitempty"`
	TaskRoleArn        string                 `json:"taskRoleArn,omitempty"`
	ExecutionRoleArn   string                 `json:"executionRoleArn,omitempty"`
}

type ecsContainerOverride struct {
	Name        string           `json:"name"`
	Command     []string         `json:"command,omitempty"`
	CPU         *int             `json:"cpu,omitempty"`
	Memory      *int             `json:"memory,omitempty"`
	Environment []ecsEnvKeyValue `json:"environment,omitempty"`
}

type ecsEnvKeyValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (r *ScheduledTaskResource) buildOverridesInput() string {
	if r.Desired == nil || r.Desired.Overrides == nil {
		return ""
	}

	overrides := r.Desired.Overrides
	if overrides.CPU == "" && overrides.Memory == "" && overrides.TaskRoleArn == "" &&
		overrides.ExecutionRoleArn == "" && len(overrides.ContainerOverrides) == 0 {
		return ""
	}

	input := ecsTaskOverrideInput{
		CPU:              overrides.CPU,
		Memory:           overrides.Memory,
		TaskRoleArn:      overrides.TaskRoleArn,
		ExecutionRoleArn: overrides.ExecutionRoleArn,
	}

	for _, co := range overrides.ContainerOverrides {
		override := ecsContainerOverride{
			Name:    co.Name,
			Command: co.Command,
		}
		if co.CPU > 0 {
			override.CPU = &co.CPU
		}
		if co.Memory > 0 {
			override.Memory = &co.Memory
		}
		for _, env := range co.Environment {
			override.Environment = append(override.Environment, ecsEnvKeyValue{
				Name:  env.Name,
				Value: env.Value,
			})
		}
		input.ContainerOverrides = append(input.ContainerOverrides, override)
	}

	data, err := json.Marshal(input)
	if err != nil {
		log.Debug("failed to marshal overrides input", "error", err)
		return ""
	}

	return string(data)
}
