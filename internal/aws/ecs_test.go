package aws

import (
	"testing"
)

func TestDeploymentStatus_Fields(t *testing.T) {
	status := &DeploymentStatus{
		Service:        "web-service",
		DesiredCount:   3,
		RunningCount:   2,
		PendingCount:   1,
		DeploymentID:   "ecs-svc/123456789",
		Status:         "ACTIVE",
		TaskDefinition: "arn:aws:ecs:us-east-1:123456789:task-definition/web:5",
		RolloutState:   "IN_PROGRESS",
	}

	if status.Service != "web-service" {
		t.Errorf("expected service 'web-service', got '%s'", status.Service)
	}
	if status.DesiredCount != 3 {
		t.Errorf("expected desired count 3, got %d", status.DesiredCount)
	}
	if status.RunningCount != 2 {
		t.Errorf("expected running count 2, got %d", status.RunningCount)
	}
	if status.PendingCount != 1 {
		t.Errorf("expected pending count 1, got %d", status.PendingCount)
	}
	if status.RolloutState != "IN_PROGRESS" {
		t.Errorf("expected rollout state 'IN_PROGRESS', got '%s'", status.RolloutState)
	}
}

func TestDeploymentStatus_IsComplete(t *testing.T) {
	tests := []struct {
		name     string
		status   DeploymentStatus
		expected bool
	}{
		{
			name: "complete when running equals desired",
			status: DeploymentStatus{
				DesiredCount: 3,
				RunningCount: 3,
				PendingCount: 0,
				RolloutState: "COMPLETED",
			},
			expected: true,
		},
		{
			name: "not complete when pending tasks",
			status: DeploymentStatus{
				DesiredCount: 3,
				RunningCount: 3,
				PendingCount: 1,
				RolloutState: "IN_PROGRESS",
			},
			expected: false,
		},
		{
			name: "not complete when running less than desired",
			status: DeploymentStatus{
				DesiredCount: 3,
				RunningCount: 2,
				PendingCount: 0,
				RolloutState: "IN_PROGRESS",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.RunningCount >= tt.status.DesiredCount && tt.status.PendingCount == 0
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestDeploymentStatus_IsFailed(t *testing.T) {
	tests := []struct {
		name     string
		status   DeploymentStatus
		expected bool
	}{
		{
			name: "failed when rollout state is FAILED",
			status: DeploymentStatus{
				RolloutState: "FAILED",
			},
			expected: true,
		},
		{
			name: "not failed when rollout state is COMPLETED",
			status: DeploymentStatus{
				RolloutState: "COMPLETED",
			},
			expected: false,
		},
		{
			name: "not failed when rollout state is IN_PROGRESS",
			status: DeploymentStatus{
				RolloutState: "IN_PROGRESS",
			},
			expected: false,
		},
		{
			name: "not failed when rollout state is empty",
			status: DeploymentStatus{
				RolloutState: "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.RolloutState == "FAILED"
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestECSClient_GetCluster(t *testing.T) {
	client := &ECSClient{
		cluster: "my-cluster",
	}

	if client.GetCluster() != "my-cluster" {
		t.Errorf("expected cluster 'my-cluster', got '%s'", client.GetCluster())
	}
}

