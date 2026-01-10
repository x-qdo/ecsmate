package resources

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/scheduler/types"

	"github.com/qdo/ecsmate/internal/config"
)

func TestScheduledTaskResource_ScheduleExpression_Cron(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name: "daily-report",
		Desired: &config.ScheduledTask{
			ScheduleType:       "cron",
			ScheduleExpression: "0 2 * * ? *",
		},
	}

	expr := resource.ScheduleExpression()
	expected := "cron(0 2 * * ? *)"

	if expr != expected {
		t.Errorf("expected '%s', got '%s'", expected, expr)
	}
}

func TestScheduledTaskResource_ScheduleExpression_Rate(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name: "hourly-sync",
		Desired: &config.ScheduledTask{
			ScheduleType:       "rate",
			ScheduleExpression: "1 hour",
		},
	}

	expr := resource.ScheduleExpression()
	expected := "rate(1 hour)"

	if expr != expected {
		t.Errorf("expected '%s', got '%s'", expected, expr)
	}
}

func TestScheduledTaskResource_ScheduleExpression_Raw(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name: "custom",
		Desired: &config.ScheduledTask{
			ScheduleType:       "",
			ScheduleExpression: "at(2023-01-01T00:00:00)",
		},
	}

	expr := resource.ScheduleExpression()
	expected := "at(2023-01-01T00:00:00)"

	if expr != expected {
		t.Errorf("expected '%s', got '%s'", expected, expr)
	}
}

func TestScheduledTaskResource_ScheduleExpression_NilDesired(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name:    "test",
		Desired: nil,
	}

	expr := resource.ScheduleExpression()
	if expr != "" {
		t.Errorf("expected empty string for nil desired, got '%s'", expr)
	}
}

func TestScheduledTaskResource_ToCreateInput(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name: "daily-report",
		Desired: &config.ScheduledTask{
			TaskDefinition:     "cron",
			Cluster:            "prod-cluster",
			TaskCount:          1,
			ScheduleType:       "cron",
			ScheduleExpression: "0 2 * * ? *",
			Timezone:           "America/New_York",
			LaunchType:         "FARGATE",
			PlatformVersion:    "1.4.0",
			Group:              "reporting",
			DeadLetterConfig: &config.DeadLetterConfig{
				Arn: "arn:aws:sqs:us-east-1:123456789:dead-letter",
			},
			RetryPolicy: &config.RetryPolicy{
				MaximumEventAgeInSeconds: 300,
				MaximumRetryAttempts:     2,
			},
			NetworkConfiguration: &config.NetworkConfiguration{
				Subnets:        []string{"subnet-1", "subnet-2"},
				SecurityGroups: []string{"sg-1"},
				AssignPublicIp: "DISABLED",
			},
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/cron:5",
		RoleArn:           "arn:aws:iam::123456789:role/scheduler-role",
	}

	input, err := resource.ToCreateInput("ecsmate")
	if err != nil {
		t.Fatalf("failed to create input: %v", err)
	}

	if aws.ToString(input.Name) != "daily-report" {
		t.Errorf("expected name 'daily-report', got '%s'", aws.ToString(input.Name))
	}
	if aws.ToString(input.GroupName) != "ecsmate" {
		t.Errorf("expected group 'ecsmate', got '%s'", aws.ToString(input.GroupName))
	}
	if aws.ToString(input.ScheduleExpression) != "cron(0 2 * * ? *)" {
		t.Errorf("unexpected schedule expression: %s", aws.ToString(input.ScheduleExpression))
	}
	if aws.ToString(input.ScheduleExpressionTimezone) != "America/New_York" {
		t.Errorf("expected timezone 'America/New_York', got '%s'", aws.ToString(input.ScheduleExpressionTimezone))
	}
	if input.State != types.ScheduleStateEnabled {
		t.Errorf("expected state ENABLED, got %s", input.State)
	}
	if input.Target == nil {
		t.Fatal("expected target")
	}
	if aws.ToString(input.Target.RoleArn) != resource.RoleArn {
		t.Errorf("expected role ARN '%s', got '%s'", resource.RoleArn, aws.ToString(input.Target.RoleArn))
	}
	if input.Target.EcsParameters == nil {
		t.Fatal("expected ECS parameters")
	}
	if aws.ToString(input.Target.EcsParameters.TaskDefinitionArn) != resource.TaskDefinitionArn {
		t.Errorf("expected task def ARN '%s', got '%s'", resource.TaskDefinitionArn, aws.ToString(input.Target.EcsParameters.TaskDefinitionArn))
	}
	if aws.ToInt32(input.Target.EcsParameters.TaskCount) != 1 {
		t.Errorf("expected task count 1, got %d", aws.ToInt32(input.Target.EcsParameters.TaskCount))
	}
	if input.Target.EcsParameters.LaunchType != types.LaunchTypeFargate {
		t.Errorf("expected launch type FARGATE, got %s", input.Target.EcsParameters.LaunchType)
	}
	if aws.ToString(input.Target.EcsParameters.Group) != "reporting" {
		t.Errorf("expected group 'reporting', got '%s'", aws.ToString(input.Target.EcsParameters.Group))
	}
	if input.Target.DeadLetterConfig == nil || aws.ToString(input.Target.DeadLetterConfig.Arn) != "arn:aws:sqs:us-east-1:123456789:dead-letter" {
		t.Errorf("expected dead letter ARN to be set")
	}
	if input.Target.RetryPolicy == nil || aws.ToInt32(input.Target.RetryPolicy.MaximumRetryAttempts) != 2 {
		t.Errorf("expected retry policy maximum retry attempts 2")
	}
}

func TestScheduledTaskResource_ToCreateInput_NoDesired(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name:    "test",
		Desired: nil,
	}

	_, err := resource.ToCreateInput("default")
	if err == nil {
		t.Error("expected error for nil desired state")
	}
}

func TestScheduledTaskResource_ToUpdateInput(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name: "hourly-sync",
		Desired: &config.ScheduledTask{
			TaskDefinition:     "sync",
			Cluster:            "prod",
			TaskCount:          2,
			ScheduleType:       "rate",
			ScheduleExpression: "30 minutes",
			Group:              "sync-group",
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/sync:3",
		RoleArn:           "arn:aws:iam::123456789:role/scheduler-role",
	}

	input, err := resource.ToUpdateInput("default")
	if err != nil {
		t.Fatalf("failed to create update input: %v", err)
	}

	if aws.ToString(input.Name) != "hourly-sync" {
		t.Errorf("expected name 'hourly-sync', got '%s'", aws.ToString(input.Name))
	}
	if aws.ToString(input.ScheduleExpression) != "rate(30 minutes)" {
		t.Errorf("unexpected schedule expression: %s", aws.ToString(input.ScheduleExpression))
	}
	if aws.ToInt32(input.Target.EcsParameters.TaskCount) != 2 {
		t.Errorf("expected task count 2, got %d", aws.ToInt32(input.Target.EcsParameters.TaskCount))
	}
	if aws.ToString(input.Target.EcsParameters.Group) != "sync-group" {
		t.Errorf("expected group 'sync-group', got '%s'", aws.ToString(input.Target.EcsParameters.Group))
	}
}

func TestScheduledTaskResource_DetermineAction_Create(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name: "new-task",
		Desired: &config.ScheduledTask{
			TaskDefinition: "cron",
			Cluster:        "test",
		},
		Current: nil,
	}

	resource.determineAction()

	if resource.Action != ScheduledTaskActionCreate {
		t.Errorf("expected action CREATE, got %s", resource.Action)
	}
}

func TestScheduledTaskResource_DetermineAction_Update(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name: "existing-task",
		Desired: &config.ScheduledTask{
			TaskDefinition: "cron",
			Cluster:        "test",
		},
		Current: &types.ScheduleSummary{
			Name: aws.String("existing-task"),
		},
	}

	resource.determineAction()

	if resource.Action != ScheduledTaskActionUpdate {
		t.Errorf("expected action UPDATE, got %s", resource.Action)
	}
}

func TestGetRegionFromCluster(t *testing.T) {
	tests := []struct {
		cluster  string
		expected string
	}{
		{
			cluster:  "arn:aws:ecs:us-west-2:123456789:cluster/my-cluster",
			expected: "us-west-2",
		},
		{
			cluster:  "arn:aws:ecs:eu-central-1:123456789:cluster/prod",
			expected: "eu-central-1",
		},
		{
			cluster:  "my-cluster",
			expected: "us-east-1",
		},
		{
			cluster:  "simple-name",
			expected: "us-east-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.cluster, func(t *testing.T) {
			result := getRegionFromCluster(tt.cluster)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestScheduledTaskAction_Values(t *testing.T) {
	if ScheduledTaskActionCreate != "CREATE" {
		t.Errorf("expected 'CREATE', got '%s'", ScheduledTaskActionCreate)
	}
	if ScheduledTaskActionUpdate != "UPDATE" {
		t.Errorf("expected 'UPDATE', got '%s'", ScheduledTaskActionUpdate)
	}
	if ScheduledTaskActionDelete != "DELETE" {
		t.Errorf("expected 'DELETE', got '%s'", ScheduledTaskActionDelete)
	}
	if ScheduledTaskActionNoop != "NOOP" {
		t.Errorf("expected 'NOOP', got '%s'", ScheduledTaskActionNoop)
	}
}

func TestNewScheduledTaskManager_DefaultGroup(t *testing.T) {
	manager := NewScheduledTaskManager(nil, "")
	if manager.groupName != "default" {
		t.Errorf("expected default group 'default', got '%s'", manager.groupName)
	}
}

func TestNewScheduledTaskManager_CustomGroup(t *testing.T) {
	manager := NewScheduledTaskManager(nil, "my-group")
	if manager.groupName != "my-group" {
		t.Errorf("expected group 'my-group', got '%s'", manager.groupName)
	}
}

func TestScheduledTaskResource_NetworkConfiguration(t *testing.T) {
	resource := &ScheduledTaskResource{
		Name: "net-task",
		Desired: &config.ScheduledTask{
			TaskDefinition: "test",
			Cluster:        "test",
			TaskCount:      1,
			NetworkConfiguration: &config.NetworkConfiguration{
				Subnets:        []string{"subnet-a", "subnet-b"},
				SecurityGroups: []string{"sg-1", "sg-2"},
				AssignPublicIp: "ENABLED",
			},
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/test:1",
		RoleArn:           "arn:aws:iam::123456789:role/role",
	}

	input, err := resource.ToCreateInput("default")
	if err != nil {
		t.Fatalf("failed to create input: %v", err)
	}

	ecsParams := input.Target.EcsParameters
	if ecsParams.NetworkConfiguration == nil {
		t.Fatal("expected network configuration")
	}

	awsVpc := ecsParams.NetworkConfiguration.AwsvpcConfiguration
	if len(awsVpc.Subnets) != 2 {
		t.Errorf("expected 2 subnets, got %d", len(awsVpc.Subnets))
	}
	if len(awsVpc.SecurityGroups) != 2 {
		t.Errorf("expected 2 security groups, got %d", len(awsVpc.SecurityGroups))
	}
	if awsVpc.AssignPublicIp != types.AssignPublicIpEnabled {
		t.Errorf("expected AssignPublicIp ENABLED, got %s", awsVpc.AssignPublicIp)
	}
}
