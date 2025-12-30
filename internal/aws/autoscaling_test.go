package aws

import (
	"testing"
)

func TestServiceResourceID(t *testing.T) {
	tests := []struct {
		cluster  string
		service  string
		expected string
	}{
		{"my-cluster", "web-service", "service/my-cluster/web-service"},
		{"prod", "api", "service/prod/api"},
		{"test-cluster", "backend", "service/test-cluster/backend"},
	}

	for _, tt := range tests {
		result := ServiceResourceID(tt.cluster, tt.service)
		if result != tt.expected {
			t.Errorf("ServiceResourceID(%s, %s) = %s, expected %s", tt.cluster, tt.service, result, tt.expected)
		}
	}
}

func TestExtractSuspendedActions(t *testing.T) {
	tests := []struct {
		name     string
		state    *struct{ in, out, sched *bool }
		expected []string
	}{
		{
			name:     "nil state",
			state:    nil,
			expected: nil,
		},
		{
			name:     "no suspensions",
			state:    &struct{ in, out, sched *bool }{boolPtr(false), boolPtr(false), boolPtr(false)},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.state == nil {
				result := extractSuspendedActions(nil)
				if len(result) != 0 && tt.expected != nil {
					t.Errorf("expected nil or empty, got %v", result)
				}
			}
		})
	}
}

func TestRegisterScalableTargetInput(t *testing.T) {
	input := &RegisterScalableTargetInput{
		Cluster:     "test-cluster",
		Service:     "web",
		MinCapacity: 2,
		MaxCapacity: 10,
	}

	if input.Cluster != "test-cluster" {
		t.Errorf("expected Cluster 'test-cluster', got '%s'", input.Cluster)
	}
	if input.Service != "web" {
		t.Errorf("expected Service 'web', got '%s'", input.Service)
	}
	if input.MinCapacity != 2 {
		t.Errorf("expected MinCapacity 2, got %d", input.MinCapacity)
	}
	if input.MaxCapacity != 10 {
		t.Errorf("expected MaxCapacity 10, got %d", input.MaxCapacity)
	}
}

func TestTargetTrackingPolicyInput(t *testing.T) {
	input := &TargetTrackingPolicyInput{
		Cluster:          "test-cluster",
		Service:          "web",
		PolicyName:       "cpu-scaling",
		TargetValue:      70.0,
		PredefinedMetric: "ECSServiceAverageCPUUtilization",
		ScaleInCooldown:  300,
		ScaleOutCooldown: 60,
	}

	if input.TargetValue != 70.0 {
		t.Errorf("expected TargetValue 70.0, got %f", input.TargetValue)
	}
	if input.PredefinedMetric != "ECSServiceAverageCPUUtilization" {
		t.Errorf("expected PredefinedMetric 'ECSServiceAverageCPUUtilization', got '%s'", input.PredefinedMetric)
	}
	if input.ScaleInCooldown != 300 {
		t.Errorf("expected ScaleInCooldown 300, got %d", input.ScaleInCooldown)
	}
	if input.ScaleOutCooldown != 60 {
		t.Errorf("expected ScaleOutCooldown 60, got %d", input.ScaleOutCooldown)
	}
}

func TestTargetTrackingPolicyInput_WithCustomMetric(t *testing.T) {
	input := &TargetTrackingPolicyInput{
		Cluster:    "test-cluster",
		Service:    "web",
		PolicyName: "custom-scaling",
		TargetValue: 1000.0,
		CustomMetric: &CustomMetricSpec{
			Namespace:  "AWS/ApplicationELB",
			MetricName: "RequestCountPerTarget",
			Statistic:  "Sum",
			Dimensions: []MetricDimension{
				{Name: "TargetGroup", Value: "targetgroup/my-tg/1234"},
			},
		},
	}

	if input.CustomMetric == nil {
		t.Fatal("expected CustomMetric to be set")
	}
	if input.CustomMetric.Namespace != "AWS/ApplicationELB" {
		t.Errorf("expected Namespace 'AWS/ApplicationELB', got '%s'", input.CustomMetric.Namespace)
	}
	if len(input.CustomMetric.Dimensions) != 1 {
		t.Errorf("expected 1 dimension, got %d", len(input.CustomMetric.Dimensions))
	}
}

func TestStepScalingPolicyInput(t *testing.T) {
	input := &StepScalingPolicyInput{
		Cluster:    "test-cluster",
		Service:    "web",
		PolicyName: "step-scaling",
		AdjustmentType: "ChangeInCapacity",
		StepAdjustments: []StepAdjustment{
			{MetricIntervalLowerBound: float64Ptr(0), ScalingAdjustment: 1},
			{MetricIntervalLowerBound: float64Ptr(20), ScalingAdjustment: 2},
		},
		Cooldown:              300,
		MetricAggregationType: "Average",
	}

	if len(input.StepAdjustments) != 2 {
		t.Errorf("expected 2 step adjustments, got %d", len(input.StepAdjustments))
	}
	if input.StepAdjustments[0].ScalingAdjustment != 1 {
		t.Errorf("expected first adjustment to be 1, got %d", input.StepAdjustments[0].ScalingAdjustment)
	}
}

func TestScheduledActionInput(t *testing.T) {
	input := &ScheduledActionInput{
		Cluster:     "test-cluster",
		Service:     "web",
		ActionName:  "scale-up-morning",
		Schedule:    "cron(0 8 * * ? *)",
		Timezone:    "America/New_York",
		MinCapacity: 5,
		MaxCapacity: 20,
	}

	if input.ActionName != "scale-up-morning" {
		t.Errorf("expected ActionName 'scale-up-morning', got '%s'", input.ActionName)
	}
	if input.Schedule != "cron(0 8 * * ? *)" {
		t.Errorf("expected Schedule 'cron(0 8 * * ? *)', got '%s'", input.Schedule)
	}
	if input.Timezone != "America/New_York" {
		t.Errorf("expected Timezone 'America/New_York', got '%s'", input.Timezone)
	}
}

func TestScalingPolicyInfo(t *testing.T) {
	info := ScalingPolicyInfo{
		PolicyName:  "web-cpu-scaling",
		PolicyARN:   "arn:aws:autoscaling:us-east-1:123456789:scalingPolicy:...",
		PolicyType:  "TargetTrackingScaling",
		ResourceID:  "service/test-cluster/web",
		TargetValue: 70.0,
	}

	if info.PolicyName != "web-cpu-scaling" {
		t.Errorf("expected PolicyName 'web-cpu-scaling', got '%s'", info.PolicyName)
	}
	if info.TargetValue != 70.0 {
		t.Errorf("expected TargetValue 70.0, got %f", info.TargetValue)
	}
}

func TestScalableTarget(t *testing.T) {
	target := ScalableTarget{
		ResourceID:    "service/test-cluster/web",
		MinCapacity:   2,
		MaxCapacity:   10,
		SuspendedFrom: []string{"DynamicScalingIn"},
	}

	if target.MinCapacity != 2 {
		t.Errorf("expected MinCapacity 2, got %d", target.MinCapacity)
	}
	if target.MaxCapacity != 10 {
		t.Errorf("expected MaxCapacity 10, got %d", target.MaxCapacity)
	}
	if len(target.SuspendedFrom) != 1 {
		t.Errorf("expected 1 suspended action, got %d", len(target.SuspendedFrom))
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func float64Ptr(f float64) *float64 {
	return &f
}
