package resources

import (
	"context"
	"fmt"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

type DesiredState struct {
	Manifest       *config.Manifest
	TaskDefs       map[string]*TaskDefResource
	Services       map[string]*ServiceResource
	ScheduledTasks map[string]*ScheduledTaskResource
}

type ResourceBuilder struct {
	ecsClient         *awsclient.ECSClient
	schedulerClient   *awsclient.SchedulerClient
	autoScalingClient *awsclient.AutoScalingClient
	taskDefManager    *TaskDefManager
	serviceManager    *ServiceManager
	scheduledManager  *ScheduledTaskManager
}

type ResourceBuilderConfig struct {
	ECSClient         *awsclient.ECSClient
	SchedulerClient   *awsclient.SchedulerClient
	AutoScalingClient *awsclient.AutoScalingClient
	SchedulerGroupName string
}

func NewResourceBuilderWithConfig(cfg ResourceBuilderConfig) *ResourceBuilder {
	var serviceManager *ServiceManager
	if cfg.AutoScalingClient != nil {
		serviceManager = NewServiceManagerWithAutoScaling(cfg.ECSClient, cfg.AutoScalingClient)
	} else {
		serviceManager = NewServiceManager(cfg.ECSClient)
	}

	return &ResourceBuilder{
		ecsClient:         cfg.ECSClient,
		schedulerClient:   cfg.SchedulerClient,
		autoScalingClient: cfg.AutoScalingClient,
		taskDefManager:    NewTaskDefManager(cfg.ECSClient),
		serviceManager:    serviceManager,
		scheduledManager:  NewScheduledTaskManager(cfg.SchedulerClient, cfg.SchedulerGroupName),
	}
}

// BuildDesiredState constructs the desired state from a manifest and discovers current state from AWS
func (b *ResourceBuilder) BuildDesiredState(ctx context.Context, manifest *config.Manifest, schedulerRoleArn string) (*DesiredState, error) {
	state := &DesiredState{
		Manifest:       manifest,
		TaskDefs:       make(map[string]*TaskDefResource),
		Services:       make(map[string]*ServiceResource),
		ScheduledTasks: make(map[string]*ScheduledTaskResource),
	}

	log.Info("building desired state from manifest", "name", manifest.Name)

	if err := b.buildTaskDefs(ctx, manifest, state); err != nil {
		return nil, fmt.Errorf("failed to build task definitions: %w", err)
	}

	if err := b.buildServices(ctx, manifest, state); err != nil {
		return nil, fmt.Errorf("failed to build services: %w", err)
	}

	if err := b.buildScheduledTasks(ctx, manifest, state, schedulerRoleArn); err != nil {
		return nil, fmt.Errorf("failed to build scheduled tasks: %w", err)
	}

	return state, nil
}

func (b *ResourceBuilder) buildTaskDefs(ctx context.Context, manifest *config.Manifest, state *DesiredState) error {
	for name, td := range manifest.TaskDefinitions {
		log.Debug("building task definition resource", "name", name, "type", td.Type)

		tdCopy := td
		resource, err := b.taskDefManager.BuildResource(ctx, name, &tdCopy)
		if err != nil {
			return fmt.Errorf("failed to build task definition %s: %w", name, err)
		}

		state.TaskDefs[name] = resource
		log.Debug("built task definition resource",
			"name", name,
			"action", resource.Action,
			"resolvedArn", resource.ResolvedArn)
	}

	return nil
}

func (b *ResourceBuilder) buildServices(ctx context.Context, manifest *config.Manifest, state *DesiredState) error {
	for name, svc := range manifest.Services {
		log.Debug("building service resource", "name", name)

		taskDefName := svc.TaskDefinition
		taskDefResource, ok := state.TaskDefs[taskDefName]
		if !ok {
			return fmt.Errorf("service %s references unknown task definition: %s", name, taskDefName)
		}

		taskDefArn := taskDefResource.ResolvedArn
		if taskDefArn == "" && taskDefResource.Desired != nil {
			taskDefArn = taskDefResource.Desired.Family
		}

		svcCopy := svc
		resource, err := b.serviceManager.BuildResource(ctx, name, &svcCopy, taskDefArn)
		if err != nil {
			return fmt.Errorf("failed to build service %s: %w", name, err)
		}

		state.Services[name] = resource
		log.Debug("built service resource",
			"name", name,
			"action", resource.Action,
			"taskDefArn", taskDefArn)
	}

	return nil
}

func (b *ResourceBuilder) buildScheduledTasks(ctx context.Context, manifest *config.Manifest, state *DesiredState, roleArn string) error {
	if b.schedulerClient == nil {
		if len(manifest.ScheduledTasks) > 0 {
			log.Warn("scheduler client not initialized, skipping scheduled tasks")
		}
		return nil
	}

	for name, task := range manifest.ScheduledTasks {
		log.Debug("building scheduled task resource", "name", name)

		taskDefName := task.TaskDefinition
		taskDefResource, ok := state.TaskDefs[taskDefName]
		if !ok {
			return fmt.Errorf("scheduled task %s references unknown task definition: %s", name, taskDefName)
		}

		taskDefArn := taskDefResource.ResolvedArn
		if taskDefArn == "" && taskDefResource.Desired != nil {
			taskDefArn = taskDefResource.Desired.Family
		}

		taskCopy := task
		resource, err := b.scheduledManager.BuildResource(ctx, name, &taskCopy, taskDefArn, roleArn)
		if err != nil {
			return fmt.Errorf("failed to build scheduled task %s: %w", name, err)
		}

		state.ScheduledTasks[name] = resource
		log.Debug("built scheduled task resource",
			"name", name,
			"action", resource.Action)
	}

	return nil
}

func (b *ResourceBuilder) TaskDefManager() *TaskDefManager {
	return b.taskDefManager
}

func (b *ResourceBuilder) ServiceManager() *ServiceManager {
	return b.serviceManager
}

func (b *ResourceBuilder) ScheduledTaskManager() *ScheduledTaskManager {
	return b.scheduledManager
}

// Summary provides a summary of actions to be taken
type Summary struct {
	TaskDefsCreate       int
	TaskDefsUpdate       int
	TaskDefsNoop         int
	ServicesCreate       int
	ServicesUpdate       int
	ServicesNoop         int
	ScheduledTasksCreate int
	ScheduledTasksUpdate int
	ScheduledTasksNoop   int
}

func (s *DesiredState) Summary() Summary {
	summary := Summary{}

	for _, td := range s.TaskDefs {
		switch td.Action {
		case TaskDefActionCreate:
			summary.TaskDefsCreate++
		case TaskDefActionUpdate:
			summary.TaskDefsUpdate++
		case TaskDefActionNoop:
			summary.TaskDefsNoop++
		}
	}

	for _, svc := range s.Services {
		switch svc.Action {
		case ServiceActionCreate:
			summary.ServicesCreate++
		case ServiceActionUpdate:
			summary.ServicesUpdate++
		case ServiceActionNoop:
			summary.ServicesNoop++
		}
	}

	for _, task := range s.ScheduledTasks {
		switch task.Action {
		case ScheduledTaskActionCreate:
			summary.ScheduledTasksCreate++
		case ScheduledTaskActionUpdate:
			summary.ScheduledTasksUpdate++
		case ScheduledTaskActionNoop:
			summary.ScheduledTasksNoop++
		}
	}

	return summary
}

func (s *DesiredState) HasChanges() bool {
	for _, td := range s.TaskDefs {
		if td.Action != TaskDefActionNoop {
			return true
		}
	}

	for _, svc := range s.Services {
		if svc.Action != ServiceActionNoop {
			return true
		}
	}

	for _, task := range s.ScheduledTasks {
		if task.Action != ScheduledTaskActionNoop {
			return true
		}
	}

	return false
}
