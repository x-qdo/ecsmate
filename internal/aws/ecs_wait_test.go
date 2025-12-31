package aws

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

func TestIsServiceSteady(t *testing.T) {
	tests := []struct {
		name     string
		service  *types.Service
		expected bool
	}{
		{
			name:     "nil service",
			service:  nil,
			expected: false,
		},
		{
			name: "running equals desired",
			service: &types.Service{
				RunningCount: 3,
				DesiredCount: 3,
				PendingCount: 0,
			},
			expected: true,
		},
		{
			name: "deployment completed",
			service: &types.Service{
				Deployments: []types.Deployment{
					{
						Status:       aws.String("PRIMARY"),
						RolloutState: types.DeploymentRolloutStateCompleted,
					},
				},
			},
			expected: true,
		},
		{
			name: "deployment in progress with pending",
			service: &types.Service{
				RunningCount: 1,
				DesiredCount: 3,
				PendingCount: 2,
				Deployments: []types.Deployment{
					{
						Status:        aws.String("PRIMARY"),
						RolloutState:  types.DeploymentRolloutStateInProgress,
						RunningCount:  1,
						DesiredCount:  3,
					},
				},
			},
			expected: false,
		},
		{
			name: "deployment running equals desired even if in progress",
			service: &types.Service{
				RunningCount: 3,
				DesiredCount: 3,
				PendingCount: 0,
				Deployments: []types.Deployment{
					{
						Status:       aws.String("PRIMARY"),
						RolloutState: types.DeploymentRolloutStateInProgress,
						RunningCount: 3,
						DesiredCount: 3,
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isServiceSteady(tt.service)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
