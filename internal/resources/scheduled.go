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
	Current *scheduler.GetScheduleOutput
	Action  ScheduledTaskAction

	TaskDefinitionArn string
	RoleArn           string
	Arn               string
	PropagationReason string
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
		Arn:           aws.String(getClusterArn(task.Cluster)),
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

func BuildSchedulerTags(manifestTags map[string]string) []types.Tag {
	tags := []types.Tag{
		{Key: aws.String(TagKeyManagedBy), Value: aws.String(TagValueEcsmate)},
	}
	for k, v := range manifestTags {
		tags = append(tags, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return tags
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
		Arn:           aws.String(getClusterArn(task.Cluster)),
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

func (m *ScheduledTaskManager) GroupName() string {
	return m.groupName
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

	schedule, err := m.schedulerClient.GetSchedule(ctx, resource.Name, m.groupName)
	if err != nil {
		return err
	}

	resource.Current = schedule
	return nil
}

func (resource *ScheduledTaskResource) determineAction() {
	if resource.Current == nil {
		resource.Action = ScheduledTaskActionCreate
		return
	}

	if resource.hasChanges() {
		resource.Action = ScheduledTaskActionUpdate
	} else {
		resource.Action = ScheduledTaskActionNoop
	}
}

func (resource *ScheduledTaskResource) hasChanges() bool {
	if resource.Current == nil || resource.Desired == nil {
		return true
	}

	current := resource.Current
	desired := resource.Desired

	if resource.ScheduleExpression() != aws.ToString(current.ScheduleExpression) {
		return true
	}

	if current.Target == nil || current.Target.EcsParameters == nil {
		return true
	}

	ecsParams := current.Target.EcsParameters

	if aws.ToString(ecsParams.TaskDefinitionArn) != resource.TaskDefinitionArn {
		return true
	}

	if int(aws.ToInt32(ecsParams.TaskCount)) != desired.TaskCount {
		return true
	}

	if string(ecsParams.LaunchType) != desired.LaunchType {
		return true
	}

	if aws.ToString(ecsParams.PlatformVersion) != desired.PlatformVersion {
		return true
	}

	if aws.ToString(ecsParams.Group) != desired.Group {
		return true
	}

	if !networkConfigMatches(ecsParams.NetworkConfiguration, desired.NetworkConfiguration) {
		return true
	}

	return false
}

func networkConfigMatches(current *types.NetworkConfiguration, desired *config.NetworkConfiguration) bool {
	if current == nil && desired == nil {
		return true
	}
	if current == nil || desired == nil {
		return false
	}
	if current.AwsvpcConfiguration == nil {
		return desired == nil
	}

	awsvpc := current.AwsvpcConfiguration

	if !stringSlicesEqual(awsvpc.Subnets, desired.Subnets) {
		return false
	}
	if !stringSlicesEqual(awsvpc.SecurityGroups, desired.SecurityGroups) {
		return false
	}
	if string(awsvpc.AssignPublicIp) != desired.AssignPublicIp {
		return false
	}

	return true
}

func (m *ScheduledTaskManager) Create(ctx context.Context, resource *ScheduledTaskResource) error {
	input, err := resource.ToCreateInput(m.groupName)
	if err != nil {
		return err
	}

	arn, err := m.schedulerClient.CreateSchedule(ctx, input)
	if err != nil {
		return err
	}

	resource.Arn = arn
	return nil
}

func (m *ScheduledTaskManager) Update(ctx context.Context, resource *ScheduledTaskResource) error {
	input, err := resource.ToUpdateInput(m.groupName)
	if err != nil {
		return err
	}

	return m.schedulerClient.UpdateSchedule(ctx, input)
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

func getClusterArn(cluster string) string {
	if strings.HasPrefix(cluster, "arn:") {
		return cluster
	}
	log.Warn("scheduled task cluster should be a full ARN", "cluster", cluster)
	return cluster
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

func (m *ScheduledTaskManager) FindOrphans(ctx context.Context, manifestName string, desiredNames map[string]bool, manifestTags map[string]string) ([]*ScheduledTaskResource, error) {
	if manifestName == "" {
		return nil, nil
	}

	namePrefix := manifestName + "-"
	schedules, err := m.schedulerClient.ListSchedules(ctx, m.groupName, namePrefix)
	if err != nil {
		log.Debug("failed to list schedules for orphan detection", "prefix", namePrefix, "error", err)
		return nil, nil
	}

	var orphans []*ScheduledTaskResource
	for _, sched := range schedules {
		scheduleName := aws.ToString(sched.Name)
		if desiredNames[scheduleName] {
			continue
		}

		schedArn := aws.ToString(sched.Arn)
		if schedArn == "" {
			continue
		}

		tags, err := m.schedulerClient.ListTagsForResource(ctx, schedArn)
		if err != nil {
			log.Debug("failed to list schedule tags", "arn", schedArn, "error", err)
			continue
		}

		if !matchesManifestTags(tags, manifestTags) {
			log.Debug("schedule not owned by manifest", "name", scheduleName, "tags", tags)
			continue
		}

		current, err := m.schedulerClient.GetSchedule(ctx, scheduleName, m.groupName)
		if err != nil {
			log.Debug("failed to get schedule details for orphan", "name", scheduleName, "error", err)
		}

		orphans = append(orphans, &ScheduledTaskResource{
			Name:    scheduleName,
			Desired: nil,
			Current: current,
			Action:  ScheduledTaskActionDelete,
			Arn:     schedArn,
		})

		log.Debug("found orphan schedule", "name", scheduleName)
	}

	return orphans, nil
}

func ResolveScheduledTaskName(manifestName, taskName string) string {
	if manifestName == "" {
		return taskName
	}
	prefix := manifestName + "-"
	if strings.HasPrefix(taskName, prefix) {
		return taskName
	}
	return prefix + taskName
}
