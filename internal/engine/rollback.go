package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/log"
)

// RollbackManager handles service rollbacks
type RollbackManager struct {
	ecsClient *awsclient.ECSClient
}

// NewRollbackManager creates a new rollback manager
func NewRollbackManager(ecsClient *awsclient.ECSClient) *RollbackManager {
	return &RollbackManager{ecsClient: ecsClient}
}

// RollbackResult contains the result of a rollback operation
type RollbackResult struct {
	ServiceName       string
	PreviousTaskDef   string
	TargetTaskDef     string
	TargetRevision    int
	Success           bool
	Message           string
}

// Rollback rolls back a service to a specific task definition revision
// If revision is negative, it's treated as relative (e.g., -1 = previous)
// If revision is positive, it's treated as absolute revision number
func (m *RollbackManager) Rollback(ctx context.Context, serviceName string, revision int, waitForStable bool) (*RollbackResult, error) {
	log.Info("starting rollback", "service", serviceName, "revision", revision)

	// Get current service status
	status, err := m.ecsClient.GetDeploymentStatus(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get service status: %w", err)
	}

	// Extract family and current revision from task definition ARN
	family, currentRev := parseTaskDefArn(status.TaskDefinition)
	if family == "" {
		return nil, fmt.Errorf("failed to parse task definition ARN: %s", status.TaskDefinition)
	}

	// Determine target revision
	var targetRev int
	if revision < 0 {
		targetRev = currentRev + revision
	} else {
		targetRev = revision
	}

	if targetRev < 1 {
		return nil, fmt.Errorf("invalid target revision: %d (current: %d)", targetRev, currentRev)
	}

	if targetRev == currentRev {
		return &RollbackResult{
			ServiceName:     serviceName,
			PreviousTaskDef: status.TaskDefinition,
			TargetTaskDef:   status.TaskDefinition,
			TargetRevision:  targetRev,
			Success:         true,
			Message:         "service already at target revision",
		}, nil
	}

	// Build target task definition ARN
	targetTaskDef := fmt.Sprintf("%s:%d", family, targetRev)

	// Verify target task definition exists
	td, err := m.ecsClient.DescribeTaskDefinition(ctx, targetTaskDef)
	if err != nil {
		return nil, fmt.Errorf("target revision %d not found for family %s: %w", targetRev, family, err)
	}

	log.Info("rolling back service",
		"service", serviceName,
		"from", status.TaskDefinition,
		"to", aws.ToString(td.TaskDefinitionArn))

	// Update service with target task definition
	_, err = m.ecsClient.UpdateServiceTaskDefinition(ctx, serviceName, aws.ToString(td.TaskDefinitionArn))
	if err != nil {
		return nil, fmt.Errorf("failed to update service: %w", err)
	}

	result := &RollbackResult{
		ServiceName:     serviceName,
		PreviousTaskDef: status.TaskDefinition,
		TargetTaskDef:   aws.ToString(td.TaskDefinitionArn),
		TargetRevision:  targetRev,
		Success:         true,
		Message:         fmt.Sprintf("rolled back from revision %d to %d", currentRev, targetRev),
	}

	// Wait for deployment to complete if requested
	if waitForStable {
		log.Info("waiting for deployment to stabilize", "service", serviceName)
		if err := m.ecsClient.WaitForSteadyState(ctx, serviceName); err != nil {
			result.Success = false
			result.Message = fmt.Sprintf("rollback initiated but deployment failed: %v", err)
			return result, nil
		}
		result.Message = fmt.Sprintf("successfully rolled back from revision %d to %d", currentRev, targetRev)
	}

	return result, nil
}

// ListRevisions returns available task definition revisions for a service
func (m *RollbackManager) ListRevisions(ctx context.Context, serviceName string, limit int) ([]RevisionInfo, error) {
	// Get current service status
	status, err := m.ecsClient.GetDeploymentStatus(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get service status: %w", err)
	}

	family, currentRev := parseTaskDefArn(status.TaskDefinition)
	if family == "" {
		return nil, fmt.Errorf("failed to parse task definition ARN: %s", status.TaskDefinition)
	}

	// List task definitions for this family
	arns, err := m.ecsClient.ListTaskDefinitions(ctx, family)
	if err != nil {
		return nil, err
	}

	var revisions []RevisionInfo
	for i, arn := range arns {
		if limit > 0 && i >= limit {
			break
		}

		_, rev := parseTaskDefArn(arn)
		info := RevisionInfo{
			Arn:       arn,
			Revision:  rev,
			IsCurrent: rev == currentRev,
		}
		revisions = append(revisions, info)
	}

	return revisions, nil
}

// RevisionInfo contains information about a task definition revision
type RevisionInfo struct {
	Arn       string
	Revision  int
	IsCurrent bool
}

// parseTaskDefArn extracts family and revision from a task definition ARN
// e.g., "arn:aws:ecs:us-east-1:123456789:task-definition/my-family:42" -> "my-family", 42
func parseTaskDefArn(arn string) (string, int) {
	// Handle both full ARN and family:revision formats
	parts := strings.Split(arn, "/")
	familyRev := arn
	if len(parts) >= 2 {
		familyRev = parts[len(parts)-1]
	}

	colonIdx := strings.LastIndex(familyRev, ":")
	if colonIdx == -1 {
		return familyRev, 0
	}

	family := familyRev[:colonIdx]
	revStr := familyRev[colonIdx+1:]
	rev, err := strconv.Atoi(revStr)
	if err != nil {
		return family, 0
	}

	return family, rev
}
