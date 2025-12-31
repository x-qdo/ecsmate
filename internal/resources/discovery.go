package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

type DesiredState struct {
	Manifest       *config.Manifest
	TaskDefs       map[string]*TaskDefResource
	Services       map[string]*ServiceResource
	ScheduledTasks map[string]*ScheduledTaskResource
	TargetGroups   map[string]*TargetGroupResource
	ListenerRules  []*ListenerRuleResource
}

type ResourceBuilder struct {
	ecsClient          *awsclient.ECSClient
	schedulerClient    *awsclient.SchedulerClient
	autoScalingClient  *awsclient.AutoScalingClient
	elbv2Client        *awsclient.ELBV2Client
	taskDefManager     *TaskDefManager
	serviceManager     *ServiceManager
	scheduledManager   *ScheduledTaskManager
	targetGroupManager *TargetGroupManager
	listenerRuleMgr    *ListenerRuleManager
}

type ResourceBuilderConfig struct {
	ECSClient          *awsclient.ECSClient
	SchedulerClient    *awsclient.SchedulerClient
	AutoScalingClient  *awsclient.AutoScalingClient
	ELBV2Client        *awsclient.ELBV2Client
	SchedulerGroupName string
}

func NewResourceBuilderWithConfig(cfg ResourceBuilderConfig) *ResourceBuilder {
	var serviceManager *ServiceManager
	if cfg.AutoScalingClient != nil {
		serviceManager = NewServiceManagerWithAutoScaling(cfg.ECSClient, cfg.AutoScalingClient)
	} else {
		serviceManager = NewServiceManager(cfg.ECSClient)
	}

	var targetGroupManager *TargetGroupManager
	var listenerRuleMgr *ListenerRuleManager
	if cfg.ELBV2Client != nil {
		targetGroupManager = NewTargetGroupManager(cfg.ELBV2Client)
		listenerRuleMgr = NewListenerRuleManager(cfg.ELBV2Client)
	}

	return &ResourceBuilder{
		ecsClient:          cfg.ECSClient,
		schedulerClient:    cfg.SchedulerClient,
		autoScalingClient:  cfg.AutoScalingClient,
		elbv2Client:        cfg.ELBV2Client,
		taskDefManager:     NewTaskDefManager(cfg.ECSClient),
		serviceManager:     serviceManager,
		scheduledManager:   NewScheduledTaskManager(cfg.SchedulerClient, cfg.SchedulerGroupName),
		targetGroupManager: targetGroupManager,
		listenerRuleMgr:    listenerRuleMgr,
	}
}

// BuildDesiredState constructs the desired state from a manifest and discovers current state from AWS
func (b *ResourceBuilder) BuildDesiredState(ctx context.Context, manifest *config.Manifest, schedulerRoleArn string) (*DesiredState, error) {
	state := &DesiredState{
		Manifest:       manifest,
		TaskDefs:       make(map[string]*TaskDefResource),
		Services:       make(map[string]*ServiceResource),
		ScheduledTasks: make(map[string]*ScheduledTaskResource),
		TargetGroups:   make(map[string]*TargetGroupResource),
		ListenerRules:  make([]*ListenerRuleResource, 0),
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

	if err := b.buildIngress(ctx, manifest, state); err != nil {
		return nil, fmt.Errorf("failed to build ingress: %w", err)
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
	clusterArns := make(map[string]string)

	for name, svc := range manifest.Services {
		log.Debug("building service resource", "name", name)

		ecsName := resolveServiceName(manifest.Name, name)
		clusterArn := resolveClusterArn(ctx, b.ecsClient, svc.Cluster, clusterArns)

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
		svcCopy.Name = ecsName
		resource, err := b.serviceManager.BuildResource(ctx, name, &svcCopy, taskDefArn)
		if err != nil {
			return fmt.Errorf("failed to build service %s: %w", name, err)
		}
		resource.ClusterArn = clusterArn

		state.Services[name] = resource
		log.Debug("built service resource",
			"name", name,
			"action", resource.Action,
			"taskDefArn", taskDefArn)
	}

	return nil
}

func resolveServiceName(namespace, service string) string {
	if namespace == "" || service == "" {
		return service
	}

	prefix := namespace + "-"
	if strings.HasPrefix(service, prefix) {
		return service
	}

	return prefix + service
}

func resolveClusterArn(ctx context.Context, ecsClient *awsclient.ECSClient, cluster string, cache map[string]string) string {
	if cluster == "" {
		return ""
	}

	if strings.HasPrefix(cluster, "arn:") {
		cache[cluster] = cluster
		return cluster
	}

	if arn, ok := cache[cluster]; ok {
		return arn
	}

	arn, err := ecsClient.DescribeClusterArn(ctx, cluster)
	if err != nil {
		log.Warn("failed to resolve cluster ARN", "cluster", cluster, "error", err)
		cache[cluster] = ""
		return ""
	}

	cache[cluster] = arn
	return arn
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

func (b *ResourceBuilder) buildIngress(ctx context.Context, manifest *config.Manifest, state *DesiredState) error {
	if manifest.Ingress == nil {
		return nil
	}

	if b.targetGroupManager == nil || b.listenerRuleMgr == nil {
		log.Warn("ELBV2 client not initialized, skipping ingress resources")
		return nil
	}

	vpcID := manifest.Ingress.VpcID
	listenerArn := manifest.Ingress.ListenerArn

	existingRules := []types.Rule{}
	if b.listenerRuleMgr != nil {
		rules, err := b.listenerRuleMgr.DescribeExistingRules(ctx, listenerArn)
		if err != nil {
			log.Warn("failed to describe listener rules", "listener", listenerArn, "error", err)
		} else {
			existingRules = rules
		}
	}

	existingRuleMatches := matchExistingListenerRules(manifest.Ingress.Rules, existingRules)
	existingTargetGroupArns := make(map[int]string)
	for idx, rule := range existingRuleMatches {
		if rule == nil {
			continue
		}
		if arn := extractTargetGroupArn(rule); arn != "" {
			existingTargetGroupArns[idx] = arn
		}
	}

	// Build target groups for rules with service backends
	tgSpecs := ExtractTargetGroups(manifest, manifest.Name)
	targetGroupArns := make(map[int]string)

	for key, spec := range tgSpecs {
		log.Debug("building target group resource", "name", spec.Name)

		existingArn := existingTargetGroupArns[spec.RuleIndex]
		resource, err := b.targetGroupManager.BuildResourceWithExisting(ctx, key, spec, vpcID, existingArn)
		if err != nil {
			return fmt.Errorf("failed to build target group %s: %w", spec.Name, err)
		}

		state.TargetGroups[key] = resource

		if resource.Arn != "" {
			targetGroupArns[spec.RuleIndex] = resource.Arn
		} else if existingArn != "" {
			targetGroupArns[spec.RuleIndex] = existingArn
		}
	}

	// Build listener rules
	state.ListenerRules = b.listenerRuleMgr.BuildResourcesWithExisting(listenerArn, manifest.Ingress.Rules, targetGroupArns, existingRules)

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
