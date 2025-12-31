package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

type DeploymentManager struct {
	ecsClient *awsclient.ECSClient
}

func NewDeploymentManager(ecsClient *awsclient.ECSClient) *DeploymentManager {
	return &DeploymentManager{ecsClient: ecsClient}
}

// BuildDeploymentConfiguration creates ECS deployment configuration from config
func BuildDeploymentConfiguration(dc config.DeploymentConfig) *types.DeploymentConfiguration {
	deployConfig := &types.DeploymentConfiguration{}

	if dc.MinimumHealthyPercentSet {
		deployConfig.MinimumHealthyPercent = aws.Int32(int32(dc.MinimumHealthyPercent))
	}
	if dc.MaximumPercentSet {
		deployConfig.MaximumPercent = aws.Int32(int32(dc.MaximumPercent))
	}

	if dc.CircuitBreakerEnable {
		deployConfig.DeploymentCircuitBreaker = &types.DeploymentCircuitBreaker{
			Enable:   dc.CircuitBreakerEnable,
			Rollback: dc.CircuitBreakerRollback,
		}
	}

	if len(dc.Alarms) > 0 {
		deployConfig.Alarms = &types.DeploymentAlarms{
			AlarmNames: dc.Alarms,
			Enable:     true,
			Rollback:   dc.AlarmRollbackEnable,
		}
	}

	return deployConfig
}

// Deploy executes the deployment based on strategy
func (m *DeploymentManager) Deploy(ctx context.Context, resource *ServiceResource) error {
	strategy := resource.Desired.Deployment.Strategy
	if strategy == "" {
		strategy = "rolling"
	}

	switch strategy {
	case "rolling":
		return m.deployRolling(ctx, resource)
	case "gradual":
		return m.deployGradual(ctx, resource)
	case "blue-green":
		return fmt.Errorf("blue-green deployment requires AWS CodeDeploy integration (deployment controller type CODE_DEPLOY); use 'rolling' or 'gradual' strategy instead")
	case "canary":
		return fmt.Errorf("canary deployment requires AWS CodeDeploy integration; use 'gradual' strategy for similar canary-like behavior with ECS native deployment")
	default:
		return fmt.Errorf("unsupported deployment strategy: %s; supported strategies: rolling, gradual", strategy)
	}
}

func (m *DeploymentManager) deployRolling(ctx context.Context, resource *ServiceResource) error {
	log.Info("starting rolling deployment", "service", resource.Name)

	input, err := resource.ToUpdateInput()
	if err != nil {
		return err
	}

	// Apply deployment configuration
	dc := resource.Desired.Deployment
	input.DeploymentConfiguration = BuildDeploymentConfiguration(dc)

	_, err = m.ecsClient.UpdateService(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to update service for rolling deployment: %w", err)
	}

	return nil
}

func (m *DeploymentManager) deployGradual(ctx context.Context, resource *ServiceResource) error {
	log.Info("starting gradual deployment", "service", resource.Name)

	dc := resource.Desired.Deployment
	steps := dc.GradualSteps
	if len(steps) == 0 {
		steps = defaultGradualSteps()
	}

	targetCount := resource.Desired.DesiredCount
	serviceName := resource.Desired.Name
	cluster := resource.Desired.Cluster

	for i, step := range steps {
		stepCount := (targetCount * step.Percent) / 100
		if stepCount < 1 {
			stepCount = 1
		}

		log.Info("gradual deployment step",
			"step", i+1,
			"total", len(steps),
			"percent", step.Percent,
			"count", stepCount)

		input := &ecs.UpdateServiceInput{
			Service:        aws.String(serviceName),
			Cluster:        aws.String(cluster),
			TaskDefinition: aws.String(resource.TaskDefinitionArn),
			DesiredCount:   aws.Int32(int32(stepCount)),
		}

		// Enable circuit breaker for safety
		input.DeploymentConfiguration = &types.DeploymentConfiguration{
			DeploymentCircuitBreaker: &types.DeploymentCircuitBreaker{
				Enable:   true,
				Rollback: true,
			},
		}

		if dc.MinimumHealthyPercent > 0 {
			input.DeploymentConfiguration.MinimumHealthyPercent = aws.Int32(int32(dc.MinimumHealthyPercent))
		}
		if dc.MaximumPercent > 0 {
			input.DeploymentConfiguration.MaximumPercent = aws.Int32(int32(dc.MaximumPercent))
		}

		_, err := m.ecsClient.UpdateService(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to update service at step %d: %w", i+1, err)
		}

		// Wait for this step to stabilize
		if err := m.waitForDeploymentHealth(ctx, serviceName, stepCount); err != nil {
			return fmt.Errorf("deployment failed at step %d: %w", i+1, err)
		}

		// Wait before next step (unless it's the last step)
		if i < len(steps)-1 && step.WaitSeconds > 0 {
			log.Info("waiting before next step", "seconds", step.WaitSeconds)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(step.WaitSeconds) * time.Second):
			}
		}
	}

	// Final step: ensure we're at target count
	if targetCount > 0 {
		input := &ecs.UpdateServiceInput{
			Service:        aws.String(serviceName),
			Cluster:        aws.String(cluster),
			TaskDefinition: aws.String(resource.TaskDefinitionArn),
			DesiredCount:   aws.Int32(int32(targetCount)),
		}

		_, err := m.ecsClient.UpdateService(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to set final desired count: %w", err)
		}
	}

	log.Info("gradual deployment complete", "service", serviceName)
	return nil
}

func (m *DeploymentManager) waitForDeploymentHealth(ctx context.Context, serviceName string, expectedCount int) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	timeout := time.After(10 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for deployment health")
		case <-ticker.C:
			status, err := m.ecsClient.GetDeploymentStatus(ctx, serviceName)
			if err != nil {
				log.Warn("failed to get deployment status", "error", err)
				continue
			}

			if status.RolloutState == "FAILED" {
				return fmt.Errorf("deployment failed: rollout state is FAILED")
			}

			if status.RunningCount >= int32(expectedCount) && status.RolloutState == "COMPLETED" {
				log.Debug("deployment step healthy",
					"running", status.RunningCount,
					"expected", expectedCount)
				return nil
			}

			if status.RunningCount >= int32(expectedCount) && status.PendingCount == 0 {
				log.Debug("deployment step reached target",
					"running", status.RunningCount,
					"expected", expectedCount)
				return nil
			}

			log.Debug("waiting for deployment",
				"running", status.RunningCount,
				"pending", status.PendingCount,
				"expected", expectedCount,
				"rolloutState", status.RolloutState)
		}
	}
}

func defaultGradualSteps() []config.GradualStep {
	return []config.GradualStep{
		{Percent: 25, WaitSeconds: 60},
		{Percent: 50, WaitSeconds: 60},
		{Percent: 75, WaitSeconds: 60},
		{Percent: 100, WaitSeconds: 0},
	}
}

// WaitForSteadyState waits for the service to reach steady state
func (m *DeploymentManager) WaitForSteadyState(ctx context.Context, serviceName string) error {
	return m.ecsClient.WaitForSteadyState(ctx, serviceName)
}
