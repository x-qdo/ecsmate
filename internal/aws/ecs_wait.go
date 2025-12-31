package aws

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// ServiceDescribeFn describes a service for polling
type ServiceDescribeFn func(ctx context.Context) (*types.Service, error)

// ErrServiceNotFound indicates the service was not found
var ErrServiceNotFound = errors.New("service not found")

// ErrServiceWaitTimeout indicates waiting for service timed out
var ErrServiceWaitTimeout = errors.New("timeout waiting for service")

// WaitForServiceSteady polls the service until it reaches a steady state
// This is useful for testing where you want to inject a custom describe function
func WaitForServiceSteady(ctx context.Context, describeFn ServiceDescribeFn, timeout time.Duration) (*types.Service, error) {
	if timeout == 0 {
		timeout = 10 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %v", ErrServiceWaitTimeout, ctx.Err())
		case <-ticker.C:
			svc, err := describeFn(ctx)
			if err != nil {
				continue
			}
			if svc == nil {
				continue
			}

			// Check if service has reached steady state
			if isServiceSteady(svc) {
				return svc, nil
			}
		}
	}
}

// WaitForServiceInactive polls the service until it becomes inactive or is deleted
func WaitForServiceInactive(ctx context.Context, describeFn ServiceDescribeFn, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 10 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %v", ErrServiceWaitTimeout, ctx.Err())
		case <-ticker.C:
			svc, err := describeFn(ctx)
			if err != nil {
				// Service not found means it's deleted - success
				if errors.Is(err, ErrServiceNotFound) {
					return nil
				}
				continue
			}
			if svc == nil {
				return nil
			}

			// Check if service is inactive
			if svc.Status != nil && *svc.Status == "INACTIVE" {
				return nil
			}
		}
	}
}

// isServiceSteady checks if a service has reached steady state
func isServiceSteady(svc *types.Service) bool {
	if svc == nil {
		return false
	}

	// Check deployment state
	for _, deployment := range svc.Deployments {
		if deployment.Status != nil && *deployment.Status == "PRIMARY" {
			// Check if rollout is complete
			if deployment.RolloutState == types.DeploymentRolloutStateCompleted {
				return true
			}
			// Check if deployment-level running count matches desired count
			if deployment.RunningCount == deployment.DesiredCount && deployment.DesiredCount > 0 {
				return true
			}
		}
	}

	// Fallback: check service-level counts (no pending tasks and running equals desired)
	return svc.RunningCount == svc.DesiredCount && svc.PendingCount == 0
}

// WaitForDeploymentComplete polls until a specific deployment completes or fails
func WaitForDeploymentComplete(ctx context.Context, describeFn ServiceDescribeFn, deploymentID string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 15 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %v", ErrServiceWaitTimeout, ctx.Err())
		case <-ticker.C:
			svc, err := describeFn(ctx)
			if err != nil {
				continue
			}
			if svc == nil {
				continue
			}

			for _, deployment := range svc.Deployments {
				if deployment.Id == nil || *deployment.Id != deploymentID {
					continue
				}

				switch deployment.RolloutState {
				case types.DeploymentRolloutStateCompleted:
					return nil
				case types.DeploymentRolloutStateFailed:
					reason := ""
					if deployment.RolloutStateReason != nil {
						reason = *deployment.RolloutStateReason
					}
					return fmt.Errorf("deployment %s failed: %s", deploymentID, reason)
				}
			}
		}
	}
}

// DefaultTimeout returns the default wait timeout
func DefaultTimeout() time.Duration {
	return 10 * time.Minute
}
