package resources

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/qdo/ecsmate/internal/config"
)

func TestServiceResource_ToCreateInput(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:         "web-service",
			Cluster:      "test-cluster",
			DesiredCount: 3,
			LaunchType:   "FARGATE",
			NetworkConfiguration: &config.NetworkConfiguration{
				Subnets:        []string{"subnet-1", "subnet-2"},
				SecurityGroups: []string{"sg-1"},
				AssignPublicIp: "ENABLED",
			},
			LoadBalancers: []config.LoadBalancer{{
				TargetGroupArn: "arn:aws:elasticloadbalancing:...:targetgroup/test",
				ContainerName:  "app",
				ContainerPort:  8080,
			}},
			Deployment: config.DeploymentConfig{
				Strategy:               "rolling",
				MinimumHealthyPercent:  50,
				MaximumPercent:         200,
				CircuitBreakerEnable:   true,
				CircuitBreakerRollback: true,
			},
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	input, err := resource.ToCreateInput()
	if err != nil {
		t.Fatalf("failed to create input: %v", err)
	}

	if aws.ToString(input.ServiceName) != "web-service" {
		t.Errorf("expected service name 'web-service', got '%s'", aws.ToString(input.ServiceName))
	}
	if aws.ToString(input.Cluster) != "test-cluster" {
		t.Errorf("expected cluster 'test-cluster', got '%s'", aws.ToString(input.Cluster))
	}
	if aws.ToInt32(input.DesiredCount) != 3 {
		t.Errorf("expected desired count 3, got %d", aws.ToInt32(input.DesiredCount))
	}
	if input.LaunchType != types.LaunchTypeFargate {
		t.Errorf("expected launch type FARGATE, got %s", input.LaunchType)
	}
	if input.NetworkConfiguration == nil {
		t.Fatal("expected network configuration")
	}
	if len(input.NetworkConfiguration.AwsvpcConfiguration.Subnets) != 2 {
		t.Errorf("expected 2 subnets, got %d", len(input.NetworkConfiguration.AwsvpcConfiguration.Subnets))
	}
	if len(input.LoadBalancers) != 1 {
		t.Errorf("expected 1 load balancer, got %d", len(input.LoadBalancers))
	}
	if input.DeploymentConfiguration == nil {
		t.Fatal("expected deployment configuration")
	}
	if aws.ToInt32(input.DeploymentConfiguration.MinimumHealthyPercent) != 50 {
		t.Errorf("expected minimum healthy percent 50, got %d", aws.ToInt32(input.DeploymentConfiguration.MinimumHealthyPercent))
	}
}

func TestServiceResource_ToCreateInput_NoDesired(t *testing.T) {
	resource := &ServiceResource{
		Name:    "web",
		Desired: nil,
	}

	_, err := resource.ToCreateInput()
	if err == nil {
		t.Error("expected error for nil desired state")
	}
}

func TestServiceResource_ToUpdateInput(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:            "web-service",
			Cluster:         "test-cluster",
			DesiredCount:    5,
			PlatformVersion: "1.4.0",
			Deployment: config.DeploymentConfig{
				MinimumHealthyPercent: 100,
				MaximumPercent:        200,
			},
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:2",
	}

	input, err := resource.ToUpdateInput()
	if err != nil {
		t.Fatalf("failed to create update input: %v", err)
	}

	if aws.ToString(input.Service) != "web-service" {
		t.Errorf("expected service 'web-service', got '%s'", aws.ToString(input.Service))
	}
	if aws.ToString(input.TaskDefinition) != resource.TaskDefinitionArn {
		t.Errorf("expected task definition '%s', got '%s'", resource.TaskDefinitionArn, aws.ToString(input.TaskDefinition))
	}
	if aws.ToInt32(input.DesiredCount) != 5 {
		t.Errorf("expected desired count 5, got %d", aws.ToInt32(input.DesiredCount))
	}
	if aws.ToString(input.PlatformVersion) != "1.4.0" {
		t.Errorf("expected platform version '1.4.0', got '%s'", aws.ToString(input.PlatformVersion))
	}
}

func TestServiceResource_DetermineAction_Create(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:         "web",
			Cluster:      "test",
			DesiredCount: 1,
		},
		Current: nil,
	}

	resource.determineAction()

	if resource.Action != ServiceActionCreate {
		t.Errorf("expected action CREATE, got %s", resource.Action)
	}
}

func TestServiceResource_DetermineAction_Update_TaskDefChange(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:         "web",
			Cluster:      "test",
			DesiredCount: 3,
		},
		Current: &types.Service{
			ServiceName:    aws.String("web"),
			TaskDefinition: aws.String("arn:aws:ecs:us-east-1:123456789:task-definition/web:1"),
			DesiredCount:   3,
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:2",
	}

	resource.determineAction()

	if resource.Action != ServiceActionUpdate {
		t.Errorf("expected action UPDATE, got %s", resource.Action)
	}
}

func TestServiceResource_DetermineAction_Update_DesiredCountChange(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:         "web",
			Cluster:      "test",
			DesiredCount: 5,
		},
		Current: &types.Service{
			ServiceName:    aws.String("web"),
			TaskDefinition: aws.String("arn:aws:ecs:us-east-1:123456789:task-definition/web:1"),
			DesiredCount:   3,
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	resource.determineAction()

	if resource.Action != ServiceActionUpdate {
		t.Errorf("expected action UPDATE, got %s", resource.Action)
	}
}

func TestServiceResource_DetermineAction_Noop(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:         "web",
			Cluster:      "test",
			DesiredCount: 3,
		},
		Current: &types.Service{
			ServiceName:    aws.String("web"),
			TaskDefinition: aws.String("arn:aws:ecs:us-east-1:123456789:task-definition/web:1"),
			DesiredCount:   3,
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	resource.determineAction()

	if resource.Action != ServiceActionNoop {
		t.Errorf("expected action NOOP, got %s", resource.Action)
	}
}

func TestServiceResource_HasChanges_LaunchType(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:       "web",
			LaunchType: "FARGATE",
		},
		Current: &types.Service{
			ServiceName: aws.String("web"),
			LaunchType:  types.LaunchTypeEc2,
		},
		TaskDefinitionArn: "",
	}

	if !resource.hasChanges() {
		t.Error("expected changes for launch type difference")
	}
}

func TestServiceResource_HasChanges_NetworkConfig(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name: "web",
			NetworkConfiguration: &config.NetworkConfiguration{
				Subnets:        []string{"subnet-1", "subnet-2"},
				SecurityGroups: []string{"sg-1"},
			},
		},
		Current: &types.Service{
			ServiceName: aws.String("web"),
			NetworkConfiguration: &types.NetworkConfiguration{
				AwsvpcConfiguration: &types.AwsVpcConfiguration{
					Subnets:        []string{"subnet-1"},
					SecurityGroups: []string{"sg-1"},
				},
			},
		},
		TaskDefinitionArn: "",
	}

	if !resource.hasChanges() {
		t.Error("expected changes for subnet difference")
	}
}

func TestServiceResource_HasChanges_CircuitBreaker(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name: "web",
			Deployment: config.DeploymentConfig{
				CircuitBreakerEnable: true,
			},
		},
		Current: &types.Service{
			ServiceName: aws.String("web"),
			DeploymentConfiguration: &types.DeploymentConfiguration{
				DeploymentCircuitBreaker: &types.DeploymentCircuitBreaker{
					Enable:   false,
					Rollback: false,
				},
			},
		},
		TaskDefinitionArn: "",
	}

	if !resource.hasChanges() {
		t.Error("expected changes for circuit breaker difference")
	}
}

func TestServiceResource_HasChanges_EnableExecuteCommand(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:                 "web",
			EnableExecuteCommand: true,
		},
		Current: &types.Service{
			ServiceName:          aws.String("web"),
			EnableExecuteCommand: false,
		},
		TaskDefinitionArn: "",
	}

	if !resource.hasChanges() {
		t.Error("expected changes for EnableExecuteCommand difference")
	}
}

func TestServiceResource_HasChanges_DeploymentPercents(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name: "web",
			Deployment: config.DeploymentConfig{
				MinimumHealthyPercent: 50,
				MaximumPercent:        200,
			},
		},
		Current: &types.Service{
			ServiceName: aws.String("web"),
			DeploymentConfiguration: &types.DeploymentConfiguration{
				MinimumHealthyPercent: aws.Int32(100),
				MaximumPercent:        aws.Int32(200),
			},
		},
		TaskDefinitionArn: "",
	}

	if !resource.hasChanges() {
		t.Error("expected changes for MinimumHealthyPercent difference")
	}
}

func TestTaskDefArnMatches(t *testing.T) {
	tests := []struct {
		name       string
		currentArn string
		desiredArn string
		expected   bool
	}{
		{
			name:       "same ARN",
			currentArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:5",
			desiredArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:5",
			expected:   true,
		},
		{
			name:       "different revision",
			currentArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:5",
			desiredArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:6",
			expected:   false,
		},
		{
			name:       "different family",
			currentArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:5",
			desiredArn: "arn:aws:ecs:us-east-1:123456789:task-definition/api:5",
			expected:   false,
		},
		{
			name:       "family only vs full ARN",
			currentArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:5",
			desiredArn: "web:5",
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := taskDefArnMatches(tt.currentArn, tt.desiredArn)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestExtractRevision(t *testing.T) {
	tests := []struct {
		arn      string
		expected string
	}{
		{"arn:aws:ecs:us-east-1:123456789:task-definition/web:5", "5"},
		{"web:10", "10"},
		{"web", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.arn, func(t *testing.T) {
			result := extractRevision(tt.arn)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}


func TestBuildNetworkConfiguration(t *testing.T) {
	nc := &config.NetworkConfiguration{
		Subnets:        []string{"subnet-1", "subnet-2"},
		SecurityGroups: []string{"sg-1", "sg-2"},
		AssignPublicIp: "ENABLED",
	}

	result := buildNetworkConfiguration(nc)

	if result == nil || result.AwsvpcConfiguration == nil {
		t.Fatal("expected network configuration")
	}

	if len(result.AwsvpcConfiguration.Subnets) != 2 {
		t.Errorf("expected 2 subnets, got %d", len(result.AwsvpcConfiguration.Subnets))
	}
	if len(result.AwsvpcConfiguration.SecurityGroups) != 2 {
		t.Errorf("expected 2 security groups, got %d", len(result.AwsvpcConfiguration.SecurityGroups))
	}
	if result.AwsvpcConfiguration.AssignPublicIp != types.AssignPublicIpEnabled {
		t.Errorf("expected AssignPublicIp ENABLED, got %s", result.AwsvpcConfiguration.AssignPublicIp)
	}
}

func TestServiceResource_DetermineAction_Recreate_LaunchTypeChanged(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:       "web",
			Cluster:    "test",
			LaunchType: "FARGATE",
		},
		Current: &types.Service{
			ServiceName: aws.String("web"),
			LaunchType:  types.LaunchTypeEc2,
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	resource.determineAction()

	if resource.Action != ServiceActionRecreate {
		t.Errorf("expected action RECREATE, got %s", resource.Action)
	}
	if len(resource.RecreateReasons) == 0 {
		t.Error("expected recreate reasons to be set")
	}
	if resource.RecreateReasons[0] != `launchType changed from "EC2" to "FARGATE"` {
		t.Errorf("unexpected recreate reason: %s", resource.RecreateReasons[0])
	}
}

func TestServiceResource_DetermineAction_Recreate_SchedulingStrategyChanged(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:               "web",
			Cluster:            "test",
			SchedulingStrategy: "DAEMON",
		},
		Current: &types.Service{
			ServiceName:        aws.String("web"),
			SchedulingStrategy: types.SchedulingStrategyReplica,
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	resource.determineAction()

	if resource.Action != ServiceActionRecreate {
		t.Errorf("expected action RECREATE, got %s", resource.Action)
	}
	foundReason := false
	for _, reason := range resource.RecreateReasons {
		if reason == `schedulingStrategy changed from "REPLICA" to "DAEMON"` {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Errorf("expected scheduling strategy change reason, got: %v", resource.RecreateReasons)
	}
}

func TestServiceResource_DetermineAction_Recreate_LoadBalancersChanged(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:    "web",
			Cluster: "test",
			LoadBalancers: []config.LoadBalancer{
				{
					TargetGroupArn: "arn:aws:elasticloadbalancing:...:new-tg",
					ContainerName:  "app",
					ContainerPort:  8080,
				},
			},
		},
		Current: &types.Service{
			ServiceName: aws.String("web"),
			LoadBalancers: []types.LoadBalancer{
				{
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:...:old-tg"),
					ContainerName:  aws.String("app"),
					ContainerPort:  aws.Int32(8080),
				},
			},
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	resource.determineAction()

	if resource.Action != ServiceActionRecreate {
		t.Errorf("expected action RECREATE, got %s", resource.Action)
	}
	foundReason := false
	for _, reason := range resource.RecreateReasons {
		if reason == "loadBalancers changed (immutable after creation)" {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Errorf("expected load balancer change reason, got: %v", resource.RecreateReasons)
	}
}

func TestServiceResource_DetermineAction_Recreate_DeploymentControllerChanged(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:                 "web",
			Cluster:              "test",
			DeploymentController: "CODE_DEPLOY",
		},
		Current: &types.Service{
			ServiceName: aws.String("web"),
			DeploymentController: &types.DeploymentController{
				Type: types.DeploymentControllerTypeEcs,
			},
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	resource.determineAction()

	if resource.Action != ServiceActionRecreate {
		t.Errorf("expected action RECREATE, got %s", resource.Action)
	}
	foundReason := false
	for _, reason := range resource.RecreateReasons {
		if reason == "deploymentController changed (immutable after creation)" {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Errorf("expected deployment controller change reason, got: %v", resource.RecreateReasons)
	}
}

func TestServiceResource_DetermineAction_Recreate_ServiceRegistriesChanged(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:    "web",
			Cluster: "test",
			ServiceRegistries: []config.ServiceRegistry{
				{RegistryArn: "arn:aws:servicediscovery:...:new-registry"},
			},
		},
		Current: &types.Service{
			ServiceName: aws.String("web"),
			ServiceRegistries: []types.ServiceRegistry{
				{RegistryArn: aws.String("arn:aws:servicediscovery:...:old-registry")},
			},
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	resource.determineAction()

	if resource.Action != ServiceActionRecreate {
		t.Errorf("expected action RECREATE, got %s", resource.Action)
	}
	foundReason := false
	for _, reason := range resource.RecreateReasons {
		if reason == "serviceRegistries changed (immutable after creation)" {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Errorf("expected service registries change reason, got: %v", resource.RecreateReasons)
	}
}

func TestServiceResource_DetermineAction_Recreate_MultipleReasons(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:               "web",
			Cluster:            "test",
			LaunchType:         "FARGATE",
			SchedulingStrategy: "DAEMON",
		},
		Current: &types.Service{
			ServiceName:        aws.String("web"),
			LaunchType:         types.LaunchTypeEc2,
			SchedulingStrategy: types.SchedulingStrategyReplica,
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	resource.determineAction()

	if resource.Action != ServiceActionRecreate {
		t.Errorf("expected action RECREATE, got %s", resource.Action)
	}
	if len(resource.RecreateReasons) != 2 {
		t.Errorf("expected 2 recreate reasons, got %d: %v", len(resource.RecreateReasons), resource.RecreateReasons)
	}
}

func TestServiceResource_CheckRecreateRequired_NoChanges(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:       "web",
			Cluster:    "test",
			LaunchType: "FARGATE",
		},
		Current: &types.Service{
			ServiceName: aws.String("web"),
			LaunchType:  types.LaunchTypeFargate,
		},
	}

	reasons := resource.checkRecreateRequired()

	if len(reasons) != 0 {
		t.Errorf("expected no recreate reasons, got: %v", reasons)
	}
}

func TestServiceResource_LoadBalancersChanged_SameConfig(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			LoadBalancers: []config.LoadBalancer{
				{
					TargetGroupArn: "arn:aws:elasticloadbalancing:...:tg",
					ContainerName:  "app",
					ContainerPort:  8080,
				},
			},
		},
		Current: &types.Service{
			LoadBalancers: []types.LoadBalancer{
				{
					TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:...:tg"),
					ContainerName:  aws.String("app"),
					ContainerPort:  aws.Int32(8080),
				},
			},
		},
	}

	if resource.loadBalancersChanged() {
		t.Error("expected load balancers to be considered unchanged")
	}
}

func TestServiceResource_LoadBalancersChanged_AddedLB(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			LoadBalancers: []config.LoadBalancer{
				{TargetGroupArn: "arn:1", ContainerName: "app", ContainerPort: 8080},
				{TargetGroupArn: "arn:2", ContainerName: "app", ContainerPort: 8081},
			},
		},
		Current: &types.Service{
			LoadBalancers: []types.LoadBalancer{
				{TargetGroupArn: aws.String("arn:1"), ContainerName: aws.String("app"), ContainerPort: aws.Int32(8080)},
			},
		},
	}

	if !resource.loadBalancersChanged() {
		t.Error("expected load balancers to be considered changed when adding LB")
	}
}

func TestServiceResource_LoadBalancersChanged_RemovedLB(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			LoadBalancers: []config.LoadBalancer{},
		},
		Current: &types.Service{
			LoadBalancers: []types.LoadBalancer{
				{TargetGroupArn: aws.String("arn:1"), ContainerName: aws.String("app"), ContainerPort: aws.Int32(8080)},
			},
		},
	}

	if !resource.loadBalancersChanged() {
		t.Error("expected load balancers to be considered changed when removing LB")
	}
}

func TestServiceResource_DeploymentControllerChanged_SameDefault(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			DeploymentController: "", // defaults to ECS
		},
		Current: &types.Service{
			DeploymentController: &types.DeploymentController{
				Type: types.DeploymentControllerTypeEcs,
			},
		},
	}

	if resource.deploymentControllerChanged() {
		t.Error("expected deployment controller to be considered unchanged")
	}
}

func TestServiceResource_DeploymentControllerChanged_NilCurrent(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			DeploymentController: "ECS",
		},
		Current: &types.Service{
			DeploymentController: nil, // defaults to ECS
		},
	}

	if resource.deploymentControllerChanged() {
		t.Error("expected deployment controller to be considered unchanged (nil defaults to ECS)")
	}
}

func TestServiceResource_ToCreateInput_WithServiceRegistries(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:         "web-service",
			Cluster:      "test-cluster",
			DesiredCount: 2,
			LaunchType:   "FARGATE",
			ServiceRegistries: []config.ServiceRegistry{
				{
					RegistryArn:   "arn:aws:servicediscovery:us-east-1:123456789:service/srv-xxx",
					ContainerName: "app",
					ContainerPort: 8080,
				},
			},
			Deployment: config.DeploymentConfig{
				Strategy: "rolling",
			},
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	input, err := resource.ToCreateInput()
	if err != nil {
		t.Fatalf("failed to create input: %v", err)
	}

	if len(input.ServiceRegistries) != 1 {
		t.Fatalf("expected 1 service registry, got %d", len(input.ServiceRegistries))
	}

	sr := input.ServiceRegistries[0]
	if aws.ToString(sr.RegistryArn) != "arn:aws:servicediscovery:us-east-1:123456789:service/srv-xxx" {
		t.Errorf("expected registry ARN, got %s", aws.ToString(sr.RegistryArn))
	}
	if aws.ToString(sr.ContainerName) != "app" {
		t.Errorf("expected container name 'app', got %s", aws.ToString(sr.ContainerName))
	}
	if aws.ToInt32(sr.ContainerPort) != 8080 {
		t.Errorf("expected container port 8080, got %d", aws.ToInt32(sr.ContainerPort))
	}
}

func TestServiceResource_ToCreateInput_WithServiceRegistries_PortOnly(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:         "web-service",
			Cluster:      "test-cluster",
			DesiredCount: 1,
			ServiceRegistries: []config.ServiceRegistry{
				{
					RegistryArn: "arn:aws:servicediscovery:us-east-1:123456789:service/srv-xxx",
					Port:        8080,
				},
			},
			Deployment: config.DeploymentConfig{
				Strategy: "rolling",
			},
		},
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/web:1",
	}

	input, err := resource.ToCreateInput()
	if err != nil {
		t.Fatalf("failed to create input: %v", err)
	}

	if len(input.ServiceRegistries) != 1 {
		t.Fatalf("expected 1 service registry, got %d", len(input.ServiceRegistries))
	}

	sr := input.ServiceRegistries[0]
	if sr.ContainerName != nil {
		t.Errorf("expected nil container name, got %s", aws.ToString(sr.ContainerName))
	}
	if sr.ContainerPort != nil {
		t.Errorf("expected nil container port, got %d", aws.ToInt32(sr.ContainerPort))
	}
	if aws.ToInt32(sr.Port) != 8080 {
		t.Errorf("expected port 8080, got %d", aws.ToInt32(sr.Port))
	}
}

