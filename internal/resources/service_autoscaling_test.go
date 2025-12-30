package resources

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
)

func TestServiceResource_DetermineAutoScalingAction_Create(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:    "web",
			Cluster: "test-cluster",
			AutoScaling: &config.AutoScalingConfig{
				MinCapacity: 2,
				MaxCapacity: 10,
				Policies: []config.ScalingPolicy{
					{Name: "cpu-scaling", TargetValue: 70, PredefinedMetric: "ECSServiceAverageCPUUtilization"},
				},
			},
		},
		Current:            &types.Service{ServiceName: aws.String("web")},
		CurrentAutoScaling: nil,
	}

	resource.determineAutoScalingAction()

	if resource.AutoScalingAction != AutoScalingActionCreate {
		t.Errorf("expected AutoScalingActionCreate, got %s", resource.AutoScalingAction)
	}
}

func TestServiceResource_DetermineAutoScalingAction_Update(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:    "web",
			Cluster: "test-cluster",
			AutoScaling: &config.AutoScalingConfig{
				MinCapacity: 2,
				MaxCapacity: 20, // Changed from 10
				Policies: []config.ScalingPolicy{
					{Name: "cpu-scaling", TargetValue: 70, PredefinedMetric: "ECSServiceAverageCPUUtilization"},
				},
			},
		},
		Current: &types.Service{ServiceName: aws.String("web")},
		CurrentAutoScaling: &CurrentAutoScalingState{
			Target: &awsclient.ScalableTarget{
				ResourceID:  "service/test-cluster/web",
				MinCapacity: 2,
				MaxCapacity: 10,
			},
			Policies: []awsclient.ScalingPolicyInfo{
				{PolicyName: "web-cpu-scaling", TargetValue: 70},
			},
		},
	}

	resource.determineAutoScalingAction()

	if resource.AutoScalingAction != AutoScalingActionUpdate {
		t.Errorf("expected AutoScalingActionUpdate, got %s", resource.AutoScalingAction)
	}
}

func TestServiceResource_DetermineAutoScalingAction_Delete(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:        "web",
			Cluster:     "test-cluster",
			AutoScaling: nil, // No auto-scaling desired
		},
		Current: &types.Service{ServiceName: aws.String("web")},
		CurrentAutoScaling: &CurrentAutoScalingState{
			Target: &awsclient.ScalableTarget{
				ResourceID:  "service/test-cluster/web",
				MinCapacity: 2,
				MaxCapacity: 10,
			},
		},
	}

	resource.determineAutoScalingAction()

	if resource.AutoScalingAction != AutoScalingActionDelete {
		t.Errorf("expected AutoScalingActionDelete, got %s", resource.AutoScalingAction)
	}
}

func TestServiceResource_DetermineAutoScalingAction_Noop(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:    "web",
			Cluster: "test-cluster",
			AutoScaling: &config.AutoScalingConfig{
				MinCapacity: 2,
				MaxCapacity: 10,
				Policies: []config.ScalingPolicy{
					{Name: "cpu-scaling", TargetValue: 70, PredefinedMetric: "ECSServiceAverageCPUUtilization"},
				},
			},
		},
		Current: &types.Service{ServiceName: aws.String("web")},
		CurrentAutoScaling: &CurrentAutoScalingState{
			Target: &awsclient.ScalableTarget{
				ResourceID:  "service/test-cluster/web",
				MinCapacity: 2,
				MaxCapacity: 10,
			},
			Policies: []awsclient.ScalingPolicyInfo{
				{PolicyName: "web-cpu-scaling", TargetValue: 70},
			},
		},
	}

	resource.determineAutoScalingAction()

	if resource.AutoScalingAction != AutoScalingActionNoop {
		t.Errorf("expected AutoScalingActionNoop, got %s", resource.AutoScalingAction)
	}
}

func TestServiceResource_DetermineAutoScalingAction_NoopWhenBothNil(t *testing.T) {
	resource := &ServiceResource{
		Name: "web",
		Desired: &config.Service{
			Name:        "web",
			Cluster:     "test-cluster",
			AutoScaling: nil,
		},
		Current:            &types.Service{ServiceName: aws.String("web")},
		CurrentAutoScaling: nil,
	}

	resource.determineAutoScalingAction()

	if resource.AutoScalingAction != AutoScalingActionNoop {
		t.Errorf("expected AutoScalingActionNoop, got %s", resource.AutoScalingAction)
	}
}

func TestServiceResource_HasAutoScalingChanges_MinCapacity(t *testing.T) {
	resource := &ServiceResource{
		Desired: &config.Service{
			AutoScaling: &config.AutoScalingConfig{
				MinCapacity: 5,
				MaxCapacity: 10,
			},
		},
		CurrentAutoScaling: &CurrentAutoScalingState{
			Target: &awsclient.ScalableTarget{
				MinCapacity: 2,
				MaxCapacity: 10,
			},
		},
	}

	if !resource.hasAutoScalingChanges() {
		t.Error("expected changes when MinCapacity differs")
	}
}

func TestServiceResource_HasAutoScalingChanges_MaxCapacity(t *testing.T) {
	resource := &ServiceResource{
		Desired: &config.Service{
			AutoScaling: &config.AutoScalingConfig{
				MinCapacity: 2,
				MaxCapacity: 20,
			},
		},
		CurrentAutoScaling: &CurrentAutoScalingState{
			Target: &awsclient.ScalableTarget{
				MinCapacity: 2,
				MaxCapacity: 10,
			},
		},
	}

	if !resource.hasAutoScalingChanges() {
		t.Error("expected changes when MaxCapacity differs")
	}
}

func TestServiceResource_HasAutoScalingChanges_PolicyCount(t *testing.T) {
	resource := &ServiceResource{
		Desired: &config.Service{
			AutoScaling: &config.AutoScalingConfig{
				MinCapacity: 2,
				MaxCapacity: 10,
				Policies: []config.ScalingPolicy{
					{Name: "cpu-scaling", TargetValue: 70},
					{Name: "memory-scaling", TargetValue: 80},
				},
			},
		},
		CurrentAutoScaling: &CurrentAutoScalingState{
			Target: &awsclient.ScalableTarget{
				MinCapacity: 2,
				MaxCapacity: 10,
			},
			Policies: []awsclient.ScalingPolicyInfo{
				{PolicyName: "web-cpu-scaling", TargetValue: 70},
			},
		},
	}

	if !resource.hasAutoScalingChanges() {
		t.Error("expected changes when policy count differs")
	}
}

func TestServiceResource_HasAutoScalingChanges_PolicyTargetValue(t *testing.T) {
	resource := &ServiceResource{
		Desired: &config.Service{
			AutoScaling: &config.AutoScalingConfig{
				MinCapacity: 2,
				MaxCapacity: 10,
				Policies: []config.ScalingPolicy{
					{Name: "cpu-scaling", TargetValue: 80}, // Changed from 70
				},
			},
		},
		CurrentAutoScaling: &CurrentAutoScalingState{
			Target: &awsclient.ScalableTarget{
				MinCapacity: 2,
				MaxCapacity: 10,
			},
			Policies: []awsclient.ScalingPolicyInfo{
				{PolicyName: "web-cpu-scaling", TargetValue: 70},
			},
		},
	}

	if !resource.hasAutoScalingChanges() {
		t.Error("expected changes when policy target value differs")
	}
}

func TestServiceResource_HasAutoScalingChanges_NewPolicy(t *testing.T) {
	resource := &ServiceResource{
		Desired: &config.Service{
			AutoScaling: &config.AutoScalingConfig{
				MinCapacity: 2,
				MaxCapacity: 10,
				Policies: []config.ScalingPolicy{
					{Name: "new-policy", TargetValue: 70},
				},
			},
		},
		CurrentAutoScaling: &CurrentAutoScalingState{
			Target: &awsclient.ScalableTarget{
				MinCapacity: 2,
				MaxCapacity: 10,
			},
			Policies: []awsclient.ScalingPolicyInfo{
				{PolicyName: "web-cpu-scaling", TargetValue: 70},
			},
		},
	}

	if !resource.hasAutoScalingChanges() {
		t.Error("expected changes when a new policy is desired")
	}
}

func TestExtractPolicyName(t *testing.T) {
	tests := []struct {
		fullName    string
		serviceName string
		expected    string
	}{
		{"web-cpu-scaling", "web", "cpu-scaling"},
		{"web-memory-scaling", "web", "memory-scaling"},
		{"other-policy", "web", "other-policy"},
		{"cpu-scaling", "web", "cpu-scaling"},
	}

	for _, tt := range tests {
		result := extractPolicyName(tt.fullName, tt.serviceName)
		if result != tt.expected {
			t.Errorf("extractPolicyName(%s, %s) = %s, expected %s", tt.fullName, tt.serviceName, result, tt.expected)
		}
	}
}

func TestNormalizePredefinedMetric(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"cpu", "ECSServiceAverageCPUUtilization"},
		{"CPU", "ECSServiceAverageCPUUtilization"},
		{"memory", "ECSServiceAverageMemoryUtilization"},
		{"Memory", "ECSServiceAverageMemoryUtilization"},
		{"alb", "ALBRequestCountPerTarget"},
		{"ALB", "ALBRequestCountPerTarget"},
		{"ECSServiceAverageCPUUtilization", "ECSServiceAverageCPUUtilization"},
		{"custom-metric", "custom-metric"},
	}

	for _, tt := range tests {
		result := normalizePredefinedMetric(tt.input)
		if result != tt.expected {
			t.Errorf("normalizePredefinedMetric(%s) = %s, expected %s", tt.input, result, tt.expected)
		}
	}
}
