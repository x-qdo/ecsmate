package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/qdo/ecsmate/internal/log"
)

type ECSClient struct {
	client  *ecs.Client
	cluster string
}

func NewECSClient(ctx context.Context, region, cluster string) (*ECSClient, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &ECSClient{
		client:  ecs.NewFromConfig(cfg),
		cluster: cluster,
	}, nil
}

func (c *ECSClient) DescribeTaskDefinition(ctx context.Context, taskDefinition string) (*types.TaskDefinition, error) {
	log.Debug("describing task definition", "taskDefinition", taskDefinition)

	out, err := c.client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(taskDefinition),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe task definition %s: %w", taskDefinition, err)
	}

	return out.TaskDefinition, nil
}

func (c *ECSClient) RegisterTaskDefinition(ctx context.Context, input *ecs.RegisterTaskDefinitionInput) (*types.TaskDefinition, error) {
	family := aws.ToString(input.Family)
	log.Debug("registering task definition", "family", family)

	out, err := c.client.RegisterTaskDefinition(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to register task definition %s: %w", family, err)
	}

	log.Info("registered task definition",
		"family", family,
		"revision", out.TaskDefinition.Revision,
		"arn", aws.ToString(out.TaskDefinition.TaskDefinitionArn))

	return out.TaskDefinition, nil
}

func (c *ECSClient) DescribeServices(ctx context.Context, services []string) ([]types.Service, error) {
	log.Debug("describing services", "cluster", c.cluster, "services", services)

	out, err := c.client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(c.cluster),
		Services: services,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe services: %w", err)
	}

	return out.Services, nil
}

func (c *ECSClient) DescribeClusterArn(ctx context.Context, cluster string) (string, error) {
	log.Debug("describing cluster", "cluster", cluster)

	out, err := c.client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: []string{cluster},
	})
	if err != nil {
		return "", fmt.Errorf("failed to describe cluster %s: %w", cluster, err)
	}

	if len(out.Clusters) == 0 {
		return "", fmt.Errorf("cluster %s not found", cluster)
	}

	return aws.ToString(out.Clusters[0].ClusterArn), nil
}

func (c *ECSClient) UpdateService(ctx context.Context, input *ecs.UpdateServiceInput) (*types.Service, error) {
	if input.Cluster == nil {
		input.Cluster = aws.String(c.cluster)
	}

	serviceName := aws.ToString(input.Service)
	log.Debug("updating service", "service", serviceName, "cluster", c.cluster)

	out, err := c.client.UpdateService(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to update service %s: %w", serviceName, err)
	}

	log.Info("updated service", "service", serviceName, "taskDefinition", aws.ToString(out.Service.TaskDefinition))

	return out.Service, nil
}

func (c *ECSClient) CreateService(ctx context.Context, input *ecs.CreateServiceInput) (*types.Service, error) {
	if input.Cluster == nil {
		input.Cluster = aws.String(c.cluster)
	}

	serviceName := aws.ToString(input.ServiceName)
	log.Debug("creating service", "service", serviceName, "cluster", c.cluster)

	out, err := c.client.CreateService(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create service %s: %w", serviceName, err)
	}

	log.Info("created service", "service", serviceName)

	return out.Service, nil
}

func (c *ECSClient) DeleteService(ctx context.Context, serviceName string, force bool) error {
	log.Debug("deleting service", "service", serviceName, "cluster", c.cluster, "force", force)

	// First, scale down to 0 if force is true
	if force {
		_, err := c.client.UpdateService(ctx, &ecs.UpdateServiceInput{
			Service:      aws.String(serviceName),
			Cluster:      aws.String(c.cluster),
			DesiredCount: aws.Int32(0),
		})
		if err != nil {
			return fmt.Errorf("failed to scale down service %s: %w", serviceName, err)
		}
		log.Debug("scaled down service to 0", "service", serviceName)
	}

	_, err := c.client.DeleteService(ctx, &ecs.DeleteServiceInput{
		Service: aws.String(serviceName),
		Cluster: aws.String(c.cluster),
		Force:   aws.Bool(force),
	})
	if err != nil {
		return fmt.Errorf("failed to delete service %s: %w", serviceName, err)
	}

	log.Info("deleted service", "service", serviceName)
	return nil
}

func (c *ECSClient) WaitForServiceInactive(ctx context.Context, serviceName string) error {
	log.Debug("waiting for service to become inactive", "service", serviceName)

	waiter := ecs.NewServicesInactiveWaiter(c.client)
	err := waiter.Wait(ctx, &ecs.DescribeServicesInput{
		Services: []string{serviceName},
		Cluster:  aws.String(c.cluster),
	}, 10*time.Minute)
	if err != nil {
		return fmt.Errorf("failed waiting for service %s to become inactive: %w", serviceName, err)
	}

	log.Debug("service is now inactive", "service", serviceName)
	return nil
}

func (c *ECSClient) ListTaskDefinitions(ctx context.Context, familyPrefix string) ([]string, error) {
	log.Debug("listing task definitions", "familyPrefix", familyPrefix)

	var arns []string
	paginator := ecs.NewListTaskDefinitionsPaginator(c.client, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String(familyPrefix),
		Sort:         types.SortOrderDesc,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list task definitions: %w", err)
		}
		arns = append(arns, page.TaskDefinitionArns...)
	}

	return arns, nil
}

type ServiceEvent struct {
	ID        string
	Message   string
	CreatedAt time.Time
}

type DeploymentInfo struct {
	ID             string
	Status         string
	TaskDefinition string
	DesiredCount   int32
	RunningCount   int32
	PendingCount   int32
	FailedTasks    int32
	RolloutState   string
}

type DeploymentStatus struct {
	Service            string
	DesiredCount       int32
	RunningCount       int32
	PendingCount       int32
	DeploymentCount    int
	DeploymentID       string
	Status             string
	TaskDefinition     string
	RolloutState       string
	RolloutStateReason string
	Events             []ServiceEvent

	// Deployment tracking
	PrimaryDeployment *DeploymentInfo // New deployment being rolled out
	ActiveDeployment  *DeploymentInfo // Previous deployment being replaced
	IsRollingBack     bool
}

func (c *ECSClient) GetDeploymentStatus(ctx context.Context, serviceName string) (*DeploymentStatus, error) {
	services, err := c.DescribeServices(ctx, []string{serviceName})
	if err != nil {
		return nil, err
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("service %s not found", serviceName)
	}

	svc := services[0]
	status := &DeploymentStatus{
		Service:         serviceName,
		DesiredCount:    svc.DesiredCount,
		RunningCount:    svc.RunningCount,
		PendingCount:    svc.PendingCount,
		DeploymentCount: len(svc.Deployments),
		TaskDefinition:  aws.ToString(svc.TaskDefinition),
		Status:          aws.ToString(svc.Status),
	}

	for _, d := range svc.Deployments {
		depInfo := &DeploymentInfo{
			ID:             aws.ToString(d.Id),
			Status:         aws.ToString(d.Status),
			TaskDefinition: aws.ToString(d.TaskDefinition),
			DesiredCount:   d.DesiredCount,
			RunningCount:   d.RunningCount,
			PendingCount:   d.PendingCount,
			FailedTasks:    d.FailedTasks,
			RolloutState:   string(d.RolloutState),
		}

		switch aws.ToString(d.Status) {
		case "PRIMARY":
			status.PrimaryDeployment = depInfo
			status.DeploymentID = depInfo.ID
			if d.RolloutState != "" {
				status.RolloutState = string(d.RolloutState)
			}
			status.RolloutStateReason = aws.ToString(d.RolloutStateReason)

			// Detect rollback from reason
			reason := strings.ToLower(status.RolloutStateReason)
			if strings.Contains(reason, "rollback") || strings.Contains(reason, "circuit breaker") {
				status.IsRollingBack = true
			}
		case "ACTIVE":
			status.ActiveDeployment = depInfo
		}
	}

	// Collect recent events (AWS returns them newest first, limited to 100)
	for _, e := range svc.Events {
		event := ServiceEvent{
			ID:      aws.ToString(e.Id),
			Message: aws.ToString(e.Message),
		}
		if e.CreatedAt != nil {
			event.CreatedAt = *e.CreatedAt
		}
		status.Events = append(status.Events, event)
	}

	return status, nil
}

func (c *ECSClient) WaitForSteadyState(ctx context.Context, serviceName string) error {
	log.Info("waiting for service to reach steady state", "service", serviceName)

	waiter := ecs.NewServicesStableWaiter(c.client)
	return waiter.Wait(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(c.cluster),
		Services: []string{serviceName},
	}, 0) // 0 means use default timeout
}

// UpdateServiceTaskDefinition updates a service to use a specific task definition
func (c *ECSClient) UpdateServiceTaskDefinition(ctx context.Context, serviceName, taskDefinition string) (*types.Service, error) {
	log.Debug("updating service task definition", "service", serviceName, "taskDefinition", taskDefinition)

	return c.UpdateService(ctx, &ecs.UpdateServiceInput{
		Service:        aws.String(serviceName),
		TaskDefinition: aws.String(taskDefinition),
	})
}

// GetCluster returns the cluster name
func (c *ECSClient) GetCluster() string {
	return c.cluster
}

// TaskInfo contains information about an ECS task
type TaskInfo struct {
	TaskArn           string
	TaskID            string
	TaskDefinitionArn string
	LastStatus        string
	DesiredStatus     string
	StopCode          string
	StoppedReason     string
	StartedAt         *time.Time
	StoppedAt         *time.Time
	Containers        []ContainerInfo
}

// ContainerInfo contains information about a container in a task
type ContainerInfo struct {
	Name         string
	LastStatus   string
	ExitCode     *int32
	Reason       string
	RuntimeID    string
	LogGroupName string
	LogStreamPrefix string
}

// ListServiceTasks lists tasks for a service, optionally filtering by status
func (c *ECSClient) ListServiceTasks(ctx context.Context, serviceName string, desiredStatus string) ([]string, error) {
	log.Debug("listing tasks for service", "service", serviceName, "desiredStatus", desiredStatus)

	input := &ecs.ListTasksInput{
		Cluster:     aws.String(c.cluster),
		ServiceName: aws.String(serviceName),
	}
	if desiredStatus != "" {
		input.DesiredStatus = types.DesiredStatus(desiredStatus)
	}

	var taskArns []string
	paginator := ecs.NewListTasksPaginator(c.client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list tasks: %w", err)
		}
		taskArns = append(taskArns, page.TaskArns...)
	}

	return taskArns, nil
}

// DescribeTasks describes the specified tasks
func (c *ECSClient) DescribeTasks(ctx context.Context, taskArns []string) ([]TaskInfo, error) {
	if len(taskArns) == 0 {
		return nil, nil
	}

	log.Debug("describing tasks", "count", len(taskArns))

	out, err := c.client.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Cluster: aws.String(c.cluster),
		Tasks:   taskArns,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe tasks: %w", err)
	}

	var tasks []TaskInfo
	for _, task := range out.Tasks {
		taskArn := aws.ToString(task.TaskArn)
		taskID := extractTaskIDFromArn(taskArn)

		info := TaskInfo{
			TaskArn:           taskArn,
			TaskID:            taskID,
			TaskDefinitionArn: aws.ToString(task.TaskDefinitionArn),
			LastStatus:        aws.ToString(task.LastStatus),
			DesiredStatus:     aws.ToString(task.DesiredStatus),
			StopCode:          string(task.StopCode),
			StoppedReason:     aws.ToString(task.StoppedReason),
			StartedAt:         task.StartedAt,
			StoppedAt:         task.StoppedAt,
		}

		for _, container := range task.Containers {
			ci := ContainerInfo{
				Name:       aws.ToString(container.Name),
				LastStatus: aws.ToString(container.LastStatus),
				ExitCode:   container.ExitCode,
				Reason:     aws.ToString(container.Reason),
				RuntimeID:  aws.ToString(container.RuntimeId),
			}
			info.Containers = append(info.Containers, ci)
		}

		tasks = append(tasks, info)
	}

	return tasks, nil
}

// GetStoppedTasks returns recently stopped tasks for a service
func (c *ECSClient) GetStoppedTasks(ctx context.Context, serviceName string, limit int) ([]TaskInfo, error) {
	taskArns, err := c.ListServiceTasks(ctx, serviceName, "STOPPED")
	if err != nil {
		return nil, err
	}

	if len(taskArns) == 0 {
		return nil, nil
	}

	if limit > 0 && len(taskArns) > limit {
		taskArns = taskArns[:limit]
	}

	return c.DescribeTasks(ctx, taskArns)
}

func extractTaskIDFromArn(arn string) string {
	// ARN format: arn:aws:ecs:region:account:task/cluster/taskID
	if idx := strings.LastIndex(arn, "/"); idx >= 0 && idx < len(arn)-1 {
		return arn[idx+1:]
	}
	return arn
}
