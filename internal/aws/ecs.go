package aws

import (
	"context"
	"fmt"
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

type DeploymentStatus struct {
	Service        string
	DesiredCount   int32
	RunningCount   int32
	PendingCount   int32
	DeploymentID   string
	Status         string
	TaskDefinition string
	RolloutState   string
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
		Service:        serviceName,
		DesiredCount:   svc.DesiredCount,
		RunningCount:   svc.RunningCount,
		PendingCount:   svc.PendingCount,
		TaskDefinition: aws.ToString(svc.TaskDefinition),
		Status:         aws.ToString(svc.Status),
	}

	for _, d := range svc.Deployments {
		if aws.ToString(d.Status) == "PRIMARY" {
			status.DeploymentID = aws.ToString(d.Id)
			if d.RolloutState != "" {
				status.RolloutState = string(d.RolloutState)
			}
			break
		}
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
