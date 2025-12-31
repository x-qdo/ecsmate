package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// TaskDefinitionRegistrar abstracts task definition registration for testing
type TaskDefinitionRegistrar interface {
	RegisterTaskDefinition(ctx context.Context, input *ecs.RegisterTaskDefinitionInput) (*types.TaskDefinition, error)
}

// TaskDefinitionDescriber abstracts task definition discovery for testing
type TaskDefinitionDescriber interface {
	DescribeTaskDefinition(ctx context.Context, taskDefinition string) (*types.TaskDefinition, error)
}

// ServiceCreator abstracts service creation for testing
type ServiceCreator interface {
	CreateService(ctx context.Context, input *ecs.CreateServiceInput) (*types.Service, error)
}

// ServiceUpdater abstracts service updates for testing
type ServiceUpdater interface {
	UpdateService(ctx context.Context, input *ecs.UpdateServiceInput) (*types.Service, error)
}

// ServiceDescriber abstracts service description for testing
type ServiceDescriber interface {
	DescribeServices(ctx context.Context, services []string) ([]types.Service, error)
	GetDeploymentStatus(ctx context.Context, serviceName string) (*DeploymentStatus, error)
}

// ServiceDeleter abstracts service deletion for testing
type ServiceDeleter interface {
	DeleteService(ctx context.Context, serviceName string, force bool) error
}

// ServiceWaiter abstracts service waiting for testing
type ServiceWaiter interface {
	WaitForSteadyState(ctx context.Context, serviceName string) error
	WaitForServiceInactive(ctx context.Context, serviceName string) error
}

// ECSOperator combines all ECS operation interfaces
type ECSOperator interface {
	TaskDefinitionRegistrar
	TaskDefinitionDescriber
	ServiceCreator
	ServiceUpdater
	ServiceDescriber
	ServiceDeleter
	ServiceWaiter
}

// Ensure ECSClient implements these interfaces
var _ TaskDefinitionRegistrar = (*ECSClient)(nil)
var _ TaskDefinitionDescriber = (*ECSClient)(nil)
var _ ServiceCreator = (*ECSClient)(nil)
var _ ServiceUpdater = (*ECSClient)(nil)
var _ ServiceDescriber = (*ECSClient)(nil)
var _ ServiceDeleter = (*ECSClient)(nil)
var _ ServiceWaiter = (*ECSClient)(nil)
var _ ECSOperator = (*ECSClient)(nil)
