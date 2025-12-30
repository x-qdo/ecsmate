package resources

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/qdo/ecsmate/internal/config"
)

func TestBuildDeploymentConfiguration_Rolling(t *testing.T) {
	dc := config.DeploymentConfig{
		Strategy:               "rolling",
		MinimumHealthyPercent:  50,
		MaximumPercent:         200,
		CircuitBreakerEnable:   true,
		CircuitBreakerRollback: true,
	}

	result := BuildDeploymentConfiguration(dc)

	if result == nil {
		t.Fatal("expected deployment configuration, got nil")
	}

	if aws.ToInt32(result.MinimumHealthyPercent) != 50 {
		t.Errorf("expected MinimumHealthyPercent 50, got %d", aws.ToInt32(result.MinimumHealthyPercent))
	}

	if aws.ToInt32(result.MaximumPercent) != 200 {
		t.Errorf("expected MaximumPercent 200, got %d", aws.ToInt32(result.MaximumPercent))
	}

	if result.DeploymentCircuitBreaker == nil {
		t.Fatal("expected circuit breaker, got nil")
	}

	if !result.DeploymentCircuitBreaker.Enable {
		t.Error("expected circuit breaker to be enabled")
	}

	if !result.DeploymentCircuitBreaker.Rollback {
		t.Error("expected circuit breaker rollback to be enabled")
	}
}

func TestBuildDeploymentConfiguration_WithAlarms(t *testing.T) {
	dc := config.DeploymentConfig{
		Strategy:            "rolling",
		Alarms:              []string{"high-cpu-alarm", "error-rate-alarm"},
		AlarmRollbackEnable: true,
	}

	result := BuildDeploymentConfiguration(dc)

	if result == nil {
		t.Fatal("expected deployment configuration, got nil")
	}

	if result.Alarms == nil {
		t.Fatal("expected alarms configuration, got nil")
	}

	if len(result.Alarms.AlarmNames) != 2 {
		t.Errorf("expected 2 alarms, got %d", len(result.Alarms.AlarmNames))
	}

	if !result.Alarms.Enable {
		t.Error("expected alarms to be enabled")
	}

	if !result.Alarms.Rollback {
		t.Error("expected alarm rollback to be enabled")
	}
}

func TestBuildDeploymentConfiguration_Empty(t *testing.T) {
	dc := config.DeploymentConfig{}

	result := BuildDeploymentConfiguration(dc)

	if result == nil {
		t.Fatal("expected deployment configuration, got nil")
	}

	// Should return empty config without nil pointer issues
	if result.DeploymentCircuitBreaker != nil {
		t.Error("expected no circuit breaker when not configured")
	}

	if result.Alarms != nil {
		t.Error("expected no alarms when not configured")
	}
}

func TestDefaultGradualSteps(t *testing.T) {
	steps := defaultGradualSteps()

	if len(steps) != 4 {
		t.Errorf("expected 4 default steps, got %d", len(steps))
	}

	expectedPercents := []int{25, 50, 75, 100}
	for i, expected := range expectedPercents {
		if steps[i].Percent != expected {
			t.Errorf("step %d: expected percent %d, got %d", i, expected, steps[i].Percent)
		}
	}

	// First three steps should have wait time
	for i := 0; i < 3; i++ {
		if steps[i].WaitSeconds != 60 {
			t.Errorf("step %d: expected WaitSeconds 60, got %d", i, steps[i].WaitSeconds)
		}
	}

	// Last step should have no wait
	if steps[3].WaitSeconds != 0 {
		t.Errorf("last step: expected WaitSeconds 0, got %d", steps[3].WaitSeconds)
	}
}

func TestGradualStep(t *testing.T) {
	step := config.GradualStep{
		Percent:     50,
		WaitSeconds: 120,
	}

	if step.Percent != 50 {
		t.Errorf("expected Percent 50, got %d", step.Percent)
	}

	if step.WaitSeconds != 120 {
		t.Errorf("expected WaitSeconds 120, got %d", step.WaitSeconds)
	}
}

func TestBuildDeploymentConfiguration_CircuitBreakerOnly(t *testing.T) {
	dc := config.DeploymentConfig{
		CircuitBreakerEnable:   true,
		CircuitBreakerRollback: false, // Rollback disabled
	}

	result := BuildDeploymentConfiguration(dc)

	if result.DeploymentCircuitBreaker == nil {
		t.Fatal("expected circuit breaker")
	}

	if !result.DeploymentCircuitBreaker.Enable {
		t.Error("expected circuit breaker enabled")
	}

	if result.DeploymentCircuitBreaker.Rollback {
		t.Error("expected circuit breaker rollback disabled")
	}
}

func TestBuildDeploymentConfiguration_AlarmsWithoutRollback(t *testing.T) {
	dc := config.DeploymentConfig{
		Alarms:              []string{"my-alarm"},
		AlarmRollbackEnable: false,
	}

	result := BuildDeploymentConfiguration(dc)

	if result.Alarms == nil {
		t.Fatal("expected alarms configuration")
	}

	if !result.Alarms.Enable {
		t.Error("expected alarms enabled")
	}

	if result.Alarms.Rollback {
		t.Error("expected alarm rollback disabled")
	}
}
