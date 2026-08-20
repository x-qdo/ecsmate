package engine

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/log"
	"github.com/x-qdo/ecsmate/internal/resources"
)

type Executor struct {
	ecsClient           *aws.ECSClient
	schedulerClient     *aws.SchedulerClient
	cloudwatchClient    *aws.CloudWatchLogsClient
	elbv2Client         *aws.ELBV2Client
	taskDefManager      *resources.TaskDefManager
	serviceManager      *resources.ServiceManager
	scheduledManager    *resources.ScheduledTaskManager
	logGroupManager     *resources.LogGroupManager
	targetGroupManager  *resources.TargetGroupManager
	listenerRuleManager *resources.ListenerRuleManager
	sdManager           *resources.ServiceDiscoveryManager
	ssmParamsManager    *resources.SSMParamsManager
	hookExecutor        *resources.HookExecutor
	tracker             *Tracker
	noWait              bool
	timeout             time.Duration
	maxParallel         int
	logLines            int
}

type deploymentWaitTarget struct {
	expectedID string
	previousID string
	requireNew bool
}

type ExecutorConfig struct {
	ECSClient              *aws.ECSClient
	SchedulerClient        *aws.SchedulerClient
	CloudWatchClient       *aws.CloudWatchLogsClient
	ELBV2Client            *aws.ELBV2Client
	ServiceDiscoveryClient *aws.ServiceDiscoveryClient
	TaskDefManager         *resources.TaskDefManager
	ServiceManager         *resources.ServiceManager
	ScheduledManager       *resources.ScheduledTaskManager
	SSMParamsManager       *resources.SSMParamsManager
	Output                 io.Writer
	NoColor                bool
	NoWait                 bool
	Timeout                time.Duration
	MaxParallel            int
	LogLines               int
}

func NewExecutor(cfg ExecutorConfig) *Executor {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 15 * time.Minute
	}

	maxParallel := cfg.MaxParallel
	if maxParallel < 0 {
		maxParallel = 0
	}

	e := &Executor{
		ecsClient:        cfg.ECSClient,
		schedulerClient:  cfg.SchedulerClient,
		cloudwatchClient: cfg.CloudWatchClient,
		elbv2Client:      cfg.ELBV2Client,
		taskDefManager:   cfg.TaskDefManager,
		serviceManager:   cfg.ServiceManager,
		scheduledManager: cfg.ScheduledManager,
		ssmParamsManager: cfg.SSMParamsManager,
		tracker:          NewTracker(cfg.Output, cfg.NoColor),
		noWait:           cfg.NoWait,
		timeout:          timeout,
		maxParallel:      maxParallel,
		logLines:         cfg.LogLines,
	}

	if cfg.CloudWatchClient != nil {
		e.logGroupManager = resources.NewLogGroupManager(cfg.CloudWatchClient)
	}
	if cfg.ELBV2Client != nil {
		e.targetGroupManager = resources.NewTargetGroupManager(cfg.ELBV2Client)
		e.listenerRuleManager = resources.NewListenerRuleManager(cfg.ELBV2Client)
	}
	if cfg.ServiceDiscoveryClient != nil {
		e.sdManager = resources.NewServiceDiscoveryManager(cfg.ServiceDiscoveryClient)
	}
	if cfg.ECSClient != nil {
		e.hookExecutor = resources.NewHookExecutor(cfg.ECSClient, cfg.CloudWatchClient)
	}

	return e
}

func (e *Executor) Execute(ctx context.Context, plan *ExecutionPlan, cluster string) error {
	e.tracker.PrintHeader(cluster)

	if err := e.applySSMParams(ctx); err != nil {
		return fmt.Errorf("failed to apply SSM parameters: %w", err)
	}

	if err := e.applyLogGroups(ctx, plan); err != nil {
		return fmt.Errorf("failed to apply log groups: %w", err)
	}

	sdArns, err := e.applyServiceDiscovery(ctx, plan)
	if err != nil {
		return fmt.Errorf("failed to apply service discovery: %w", err)
	}

	e.resolveServiceDiscoveryArns(plan, sdArns)

	// Apply target groups (before services)
	targetGroupArns, err := e.applyTargetGroups(ctx, plan)
	if err != nil {
		return fmt.Errorf("failed to apply target groups: %w", err)
	}

	e.resolveIngressTargetGroups(plan, targetGroupArns)

	if err := e.registerTaskDefs(ctx, plan); err != nil {
		return fmt.Errorf("failed to register task definitions: %w", err)
	}

	e.refreshTaskDefinitionRefs(plan)

	// Apply listener rules before services so target groups are attached to the load balancer.
	if err := e.applyListenerRules(ctx, plan, targetGroupArns); err != nil {
		return fmt.Errorf("failed to apply listener rules: %w", err)
	}

	if err := e.deployServices(ctx, plan); err != nil {
		return fmt.Errorf("failed to deploy services: %w", err)
	}

	if err := e.applyScheduledTasks(ctx, plan); err != nil {
		return fmt.Errorf("failed to apply scheduled tasks: %w", err)
	}

	e.tracker.PrintSummary()

	if e.tracker.HasFailures() {
		return fmt.Errorf("deployment completed with failures")
	}

	return nil
}

func (e *Executor) applySSMParams(ctx context.Context) error {
	if e.ssmParamsManager == nil {
		return nil
	}

	e.tracker.PrintSection("SSM Parameters")

	changes, err := e.ssmParamsManager.Diff(ctx)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		return nil
	}

	for _, c := range changes {
		e.tracker.AddTask(c.Name, "ssm-parameter")
		e.tracker.StartTask(c.Name)
	}

	if err := e.ssmParamsManager.Apply(ctx); err != nil {
		for _, c := range changes {
			e.tracker.FailTask(c.Name, err.Error())
		}
		return err
	}

	for _, c := range changes {
		e.tracker.CompleteTask(c.Name, c.Action)
	}

	return nil
}

func (e *Executor) applyServiceDiscovery(ctx context.Context, plan *ExecutionPlan) (map[string]string, error) {
	sdArns := make(map[string]string)

	if e.sdManager == nil || plan.ServiceDiscovery == nil || len(plan.ServiceDiscovery) == 0 {
		return sdArns, nil
	}

	e.tracker.PrintSection("\nService Discovery")

	for key, sd := range plan.ServiceDiscovery {
		e.tracker.AddTask(sd.Name, "service-discovery")

		if sd.Action == resources.ServiceDiscoveryActionNoop {
			e.tracker.SkipTask(sd.Name, "unchanged")
			if sd.Arn != "" {
				sdArns[key] = sd.Arn
			}
			continue
		}

		e.tracker.StartTask(sd.Name)

		if err := e.sdManager.Apply(ctx, sd); err != nil {
			e.tracker.FailTask(sd.Name, err.Error())
			return nil, fmt.Errorf("service discovery %s: %w", sd.Name, err)
		}

		e.tracker.CompleteTask(sd.Name, string(sd.Action))

		if sd.Arn != "" {
			sdArns[key] = sd.Arn
		}
	}

	return sdArns, nil
}

func (e *Executor) resolveServiceDiscoveryArns(plan *ExecutionPlan, sdArns map[string]string) {
	if plan == nil || plan.Graph == nil || len(sdArns) == 0 {
		return
	}

	for _, node := range plan.Graph.nodes {
		svc := node.ServiceResource()
		if svc == nil || svc.Desired == nil {
			continue
		}

		for i := range svc.Desired.ServiceRegistries {
			reg := &svc.Desired.ServiceRegistries[i]
			if reg.ServiceDiscovery == nil || reg.RegistryArn != "" {
				continue
			}

			key := fmt.Sprintf("%s-sd-%d", node.Name, i)
			if arn, ok := sdArns[key]; ok && arn != "" {
				reg.RegistryArn = arn
			}
		}
	}
}

func (e *Executor) refreshTaskDefinitionRefs(plan *ExecutionPlan) {
	if plan == nil || len(plan.TaskDefs) == 0 {
		return
	}

	taskDefArns := make(map[string]string, len(plan.TaskDefs))
	for _, td := range plan.TaskDefs {
		if td == nil || td.ResolvedArn == "" {
			continue
		}
		taskDefArns[td.Name] = td.ResolvedArn
	}

	if len(taskDefArns) == 0 {
		return
	}

	if plan.Graph != nil {
		for _, node := range plan.Graph.nodes {
			svc := node.ServiceResource()
			if svc == nil || svc.Desired == nil {
				continue
			}
			taskDefName := svc.Desired.TaskDefinition
			if taskDefName == "" {
				continue
			}
			if arn, ok := taskDefArns[taskDefName]; ok && arn != "" {
				log.Debug("refreshing service task definition", "service", node.Name, "taskDef", taskDefName, "arn", arn)
				svc.TaskDefinitionArn = arn
				svc.RecalculateAction()
			}
		}
	}

	for _, task := range plan.ScheduledTasks {
		if task == nil || task.Desired == nil {
			continue
		}
		taskDefName := task.Desired.TaskDefinition
		if taskDefName == "" {
			continue
		}
		if arn, ok := taskDefArns[taskDefName]; ok && arn != "" {
			log.Debug("refreshing scheduled task definition", "task", task.Name, "taskDef", taskDefName, "arn", arn)
			task.TaskDefinitionArn = arn
		}
	}
}

func (e *Executor) applyLogGroups(ctx context.Context, plan *ExecutionPlan) error {
	if e.logGroupManager == nil || plan.Manifest == nil {
		return nil
	}

	logGroupSpecs := resources.ExtractLogGroups(plan.Manifest)
	if len(logGroupSpecs) == 0 {
		return nil
	}

	e.tracker.PrintSection("Log Groups")

	for name, spec := range logGroupSpecs {
		resource, err := e.logGroupManager.BuildResource(ctx, spec)
		if err != nil {
			return fmt.Errorf("failed to build log group %s: %w", name, err)
		}

		e.tracker.AddTask(name, "log-group")

		needsSubscriptionReconcile, err := e.logGroupManager.NeedsSubscriptionReconcile(ctx, resource)
		if err != nil {
			return fmt.Errorf("log group %s: failed to inspect subscription filter state: %w", name, err)
		}
		if resource.Action == resources.LogGroupActionNoop && !needsSubscriptionReconcile {
			e.tracker.SkipTask(name, "unchanged")
			continue
		}

		e.tracker.StartTask(name)

		if err := e.logGroupManager.Apply(ctx, resource); err != nil {
			e.tracker.FailTask(name, err.Error())
			return fmt.Errorf("log group %s: %w", name, err)
		}

		result := string(resource.Action)
		if resource.Action == resources.LogGroupActionNoop && needsSubscriptionReconcile {
			result = "RECONCILED"
		}
		e.tracker.CompleteTask(name, result)
	}

	return nil
}

func (e *Executor) applyTargetGroups(ctx context.Context, plan *ExecutionPlan) (map[int]string, error) {
	targetGroupArns := make(map[int]string)

	if e.targetGroupManager == nil || plan.Manifest == nil || plan.Manifest.Ingress == nil {
		return targetGroupArns, nil
	}

	ingress := plan.Manifest.Ingress
	targetGroupSpecs := resources.ExtractTargetGroups(plan.Manifest, plan.Manifest.Name)

	if len(targetGroupSpecs) == 0 {
		return targetGroupArns, nil
	}

	e.tracker.PrintSection("\nTarget Groups")

	for key, spec := range targetGroupSpecs {
		resource, err := e.targetGroupManager.BuildResource(ctx, key, spec, ingress.VpcID)
		if err != nil {
			return nil, fmt.Errorf("failed to build target group %s: %w", spec.Name, err)
		}

		e.tracker.AddTask(spec.Name, "target-group")

		if resource.Action == resources.TargetGroupActionNoop {
			e.tracker.SkipTask(spec.Name, "unchanged")
		} else {
			e.tracker.StartTask(spec.Name)

			if err := e.targetGroupManager.Apply(ctx, resource); err != nil {
				e.tracker.FailTask(spec.Name, err.Error())
				return nil, fmt.Errorf("target group %s: %w", spec.Name, err)
			}

			e.tracker.CompleteTask(spec.Name, string(resource.Action))
		}

		// Store the ARN for later use in listener rules
		idx, err := strconv.Atoi(strings.TrimPrefix(key, "rule-"))
		if err != nil {
			return nil, fmt.Errorf("invalid target group key %q: %w", key, err)
		}
		targetGroupArns[idx] = resource.Arn
	}

	return targetGroupArns, nil
}

func (e *Executor) applyListenerRules(ctx context.Context, plan *ExecutionPlan, targetGroupArns map[int]string) error {
	if e.listenerRuleManager == nil || plan.Manifest == nil || plan.Manifest.Ingress == nil {
		return nil
	}

	ingress := plan.Manifest.Ingress
	if len(ingress.Rules) == 0 {
		return nil
	}

	e.tracker.PrintSection("\nListener Rules")

	ruleResources, err := e.listenerRuleManager.BuildResources(ctx, ingress.ListenerArn, ingress.Rules, targetGroupArns, plan.Manifest.Name, plan.Manifest.Tags, nil)
	if err != nil {
		return fmt.Errorf("failed to build listener rules: %w", err)
	}

	for _, resource := range ruleResources {
		name := fmt.Sprintf("rule-%d", resource.Priority)
		e.tracker.AddTask(name, "listener-rule")

		if resource.Action == resources.ListenerRuleActionNoop {
			e.tracker.SkipTask(name, "unchanged")
			continue
		}

		e.tracker.StartTask(name)

		if err := e.listenerRuleManager.Apply(ctx, resource); err != nil {
			e.tracker.FailTask(name, err.Error())
			return fmt.Errorf("listener rule priority %d: %w", resource.Priority, err)
		}

		e.tracker.CompleteTask(name, string(resource.Action))
	}

	return nil
}

func (e *Executor) registerTaskDefs(ctx context.Context, plan *ExecutionPlan) error {
	if len(plan.TaskDefs) == 0 {
		return nil
	}

	e.tracker.PrintSection("\nTask Definitions")

	var wg sync.WaitGroup
	errors := make(chan error, len(plan.TaskDefs))

	for _, td := range plan.TaskDefs {
		wg.Add(1)
		go func(td *resources.TaskDefResource) {
			defer wg.Done()

			taskName := td.Name
			if td.Desired != nil && td.Desired.Family != "" {
				taskName = td.Desired.Family
			}

			e.tracker.AddTask(taskName, "task-definition")

			if td.Action == resources.TaskDefActionNoop {
				e.tracker.SkipTask(taskName, "unchanged")
				return
			}

			if td.Type == "remote" {
				e.tracker.SkipTask(taskName, "remote")
				return
			}

			e.tracker.StartTask(taskName)

			if err := e.taskDefManager.Register(ctx, td); err != nil {
				e.tracker.FailTask(taskName, err.Error())
				errors <- fmt.Errorf("task definition %s: %w", taskName, err)
				return
			}

			revision := ""
			if td.Current != nil {
				revision = fmt.Sprintf("revision %d", td.Current.Revision)
			}
			e.tracker.CompleteTask(taskName, revision)
		}(td)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		return err
	}

	return nil
}

func (e *Executor) deployServices(ctx context.Context, plan *ExecutionPlan) error {
	if len(plan.ServiceLevels) == 0 {
		return nil
	}

	e.tracker.PrintSection("\nServices")

	for levelIdx, level := range plan.ServiceLevels {
		log.Debug("deploying service level", "level", levelIdx, "services", level)

		// Run pre-hooks for this level
		if err := e.runPreHooks(ctx, plan, level); err != nil {
			return err
		}

		var wg sync.WaitGroup
		errors := make(chan error, len(level))

		// Create semaphore for limiting parallelism if maxParallel > 0
		var sem chan struct{}
		if e.maxParallel > 0 {
			sem = make(chan struct{}, e.maxParallel)
		}

		for _, serviceName := range level {
			node, ok := plan.Graph.GetNode(serviceName)
			if !ok {
				continue
			}

			svc := node.ServiceResource()
			if svc == nil {
				continue
			}
			wg.Add(1)

			go func(svc *resources.ServiceResource) {
				defer wg.Done()

				// Acquire semaphore slot if limited
				if sem != nil {
					sem <- struct{}{}
					defer func() { <-sem }()
				}

				e.tracker.AddTask(svc.Name, "service")

				if svc.Action == resources.ServiceActionNoop {
					e.tracker.SkipTask(svc.Name, "unchanged")
					return
				}

				e.tracker.StartTask(svc.Name)

				if err := e.serviceManager.Apply(ctx, svc); err != nil {
					e.tracker.FailTask(svc.Name, err.Error())
					errors <- fmt.Errorf("service %s: %w", svc.Name, err)
					return
				}

				if !e.noWait {
					if err := e.waitForService(ctx, svc); err != nil {
						e.tracker.FailTask(svc.Name, err.Error())
						errors <- fmt.Errorf("service %s: %w", svc.Name, err)
						return
					}
				}

				e.tracker.CompleteTask(svc.Name, "deployed")
			}(svc)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			return err
		}

		// Run post-hooks for this level after successful deployment
		e.runPostHooks(ctx, plan, level)
	}

	return nil
}

func (e *Executor) waitForService(ctx context.Context, svc *resources.ServiceResource) error {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	serviceName := svc.Name
	if svc.Desired != nil && svc.Desired.Name != "" {
		serviceName = svc.Desired.Name
	}
	expectedTaskDef := svc.TaskDefinitionArn
	target := newDeploymentWaitTarget(svc)

	// Track events after deployment started
	deploymentStartTime := time.Now()
	seenEventIDs := make(map[string]bool)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for service to stabilize")
		case <-ticker.C:
			status, err := e.serviceManager.GetDeploymentStatus(ctx, serviceName)
			if err != nil {
				log.Warn("failed to get deployment status", "service", serviceName, "error", err)
				continue
			}

			// Debug logging for deployment tracking
			log.Debug("deployment status",
				"service", serviceName,
				"desired", status.DesiredCount,
				"running", status.RunningCount,
				"pending", status.PendingCount,
				"rolloutState", status.RolloutState,
				"taskDef", status.TaskDefinition,
				"deploymentCount", status.DeploymentCount,
				"isRollingBack", status.IsRollingBack,
			)
			if status.PrimaryDeployment != nil {
				log.Debug("primary deployment (NEW)",
					"id", status.PrimaryDeployment.ID,
					"taskDef", status.PrimaryDeployment.TaskDefinition,
					"running", status.PrimaryDeployment.RunningCount,
					"pending", status.PrimaryDeployment.PendingCount,
					"failed", status.PrimaryDeployment.FailedTasks,
					"rolloutState", status.PrimaryDeployment.RolloutState,
				)
			}
			if status.ActiveDeployment != nil {
				log.Debug("active deployment (OLD)",
					"id", status.ActiveDeployment.ID,
					"taskDef", status.ActiveDeployment.TaskDefinition,
					"running", status.ActiveDeployment.RunningCount,
					"pending", status.ActiveDeployment.PendingCount,
				)
			}

			if target.failedActiveDeployment(status) {
				e.printFailureLogs(ctx, serviceName, status.Events, svc, deploymentStartTime)
				return fmt.Errorf("deployment failed (circuit breaker triggered)")
			}
			if !target.matches(status.DeploymentID) {
				log.Debug("waiting for current deployment to become visible",
					"service", serviceName,
					"expectedDeploymentID", target.expectedID,
					"previousDeploymentID", target.previousID,
					"observedDeploymentID", status.DeploymentID,
				)
				continue
			}

			// Collect recent events (up to 5, newest first) that occurred after deployment started
			var recentEvents []EventInfo
			for _, event := range status.Events {
				if event.CreatedAt.After(deploymentStartTime) && !seenEventIDs[event.ID] {
					seenEventIDs[event.ID] = true
					recentEvents = append(recentEvents, EventInfo{
						Timestamp: event.CreatedAt,
						Message:   event.Message,
					})
					log.Debug("new event", "service", serviceName, "time", event.CreatedAt.Format("15:04:05"), "msg", event.Message)
				}
			}
			log.Debug("events collected", "service", serviceName, "newEvents", len(recentEvents), "totalSeen", len(seenEventIDs))

			// Build deployment progress info
			var newDep, oldDep *DeploymentProgressInfo
			if status.PrimaryDeployment != nil {
				newDep = &DeploymentProgressInfo{
					ID:             status.PrimaryDeployment.ID,
					TaskDefinition: status.PrimaryDeployment.TaskDefinition,
					RunningCount:   status.PrimaryDeployment.RunningCount,
					PendingCount:   status.PrimaryDeployment.PendingCount,
					FailedTasks:    status.PrimaryDeployment.FailedTasks,
				}
			}
			if status.ActiveDeployment != nil {
				oldDep = &DeploymentProgressInfo{
					ID:             status.ActiveDeployment.ID,
					TaskDefinition: status.ActiveDeployment.TaskDefinition,
					RunningCount:   status.ActiveDeployment.RunningCount,
					PendingCount:   status.ActiveDeployment.PendingCount,
					FailedTasks:    status.ActiveDeployment.FailedTasks,
				}
			}

			// Fetch task details
			var taskDisplayInfos []TaskDisplayInfo
			taskArns, err := e.ecsClient.ListServiceTasks(ctx, serviceName, "")
			if err == nil && len(taskArns) > 0 {
				tasks, err := e.ecsClient.DescribeTasks(ctx, taskArns)
				if err == nil {
					for _, t := range tasks {
						taskDisplayInfos = append(taskDisplayInfos, TaskDisplayInfo{
							TaskID:         t.TaskID,
							LastStatus:     t.LastStatus,
							DesiredStatus:  t.DesiredStatus,
							StartedAt:      t.StartedAt,
							TaskDefinition: t.TaskDefinitionArn,
						})
					}
				}
			}

			// Update progress display
			e.tracker.UpdateServiceProgress(ServiceProgressUpdate{
				ServiceName:    serviceName,
				DesiredCount:   status.DesiredCount,
				RunningCount:   status.RunningCount,
				PendingCount:   status.PendingCount,
				RolloutState:   status.RolloutState,
				TaskDefinition: status.TaskDefinition,
				RolloutReason:  status.RolloutStateReason,
				Events:         recentEvents,
				Tasks:          taskDisplayInfos,
				NewDeployment:  newDep,
				OldDeployment:  oldDep,
				IsRollingBack:  status.IsRollingBack,
			})

			switch status.RolloutState {
			case "COMPLETED":
				if rollbackDetected(expectedTaskDef, status) {
					e.printFailureLogs(ctx, serviceName, status.Events, svc, deploymentStartTime)
					return fmt.Errorf("deployment rolled back to %s", status.TaskDefinition)
				}
				return nil
			case "FAILED":
				e.printFailureLogs(ctx, serviceName, status.Events, svc, deploymentStartTime)
				return fmt.Errorf("deployment failed (circuit breaker triggered)")
			}

			if rollbackInProgress(expectedTaskDef, status) {
				e.printFailureLogs(ctx, serviceName, status.Events, svc, deploymentStartTime)
				return fmt.Errorf("deployment rollback started: %s", status.RolloutStateReason)
			}

			if status.RolloutState == "" &&
				status.DeploymentCount <= 1 &&
				status.RunningCount == status.DesiredCount &&
				status.PendingCount == 0 {
				if rollbackDetected(expectedTaskDef, status) {
					return fmt.Errorf("deployment rolled back to %s", status.TaskDefinition)
				}
				return nil
			}
		}
	}
}

func newDeploymentWaitTarget(svc *resources.ServiceResource) deploymentWaitTarget {
	target := deploymentWaitTarget{
		expectedID: svc.PrimaryDeploymentID(),
		previousID: svc.PreviousDeploymentID,
		requireNew: svc.RequireNewDeployment,
	}

	// UpdateService can briefly return the old primary deployment even when a
	// forced deployment was accepted. In that case, latch onto the first new ID
	// returned by DescribeServices instead of treating the old FAILED state as current.
	if target.requireNew && target.expectedID == target.previousID {
		target.expectedID = ""
	}

	return target
}

func (t *deploymentWaitTarget) matches(deploymentID string) bool {
	if deploymentID == "" {
		return !t.requireNew && t.expectedID == ""
	}
	if t.requireNew && deploymentID == t.previousID {
		return false
	}
	if t.expectedID == "" {
		t.expectedID = deploymentID
	}

	return deploymentID == t.expectedID
}

func (t *deploymentWaitTarget) failedActiveDeployment(status *aws.DeploymentStatus) bool {
	if status == nil || status.ActiveDeployment == nil ||
		status.ActiveDeployment.RolloutState != "FAILED" {
		return false
	}
	if t.expectedID == "" && !t.requireNew {
		return false
	}

	return t.matches(status.ActiveDeployment.ID)
}

func rollbackDetected(expectedTaskDef string, status *aws.DeploymentStatus) bool {
	if expectedTaskDef == "" || status == nil || status.TaskDefinition == "" {
		return false
	}
	if taskDefArnMatches(status.TaskDefinition, expectedTaskDef) {
		return false
	}
	return status.RolloutState == "COMPLETED" || status.DeploymentCount <= 1
}

func rollbackInProgress(expectedTaskDef string, status *aws.DeploymentStatus) bool {
	if status == nil || status.RolloutStateReason == "" {
		return false
	}
	reason := strings.ToLower(status.RolloutStateReason)
	if strings.Contains(reason, "rollback") || strings.Contains(reason, "circuit breaker") {
		return true
	}
	if expectedTaskDef == "" || status.TaskDefinition == "" {
		return false
	}
	if taskDefArnMatches(status.TaskDefinition, expectedTaskDef) {
		return false
	}
	return status.DeploymentCount > 1
}

func taskDefArnMatches(currentArn, desiredArn string) bool {
	currentKey := taskDefKey(currentArn)
	desiredKey := taskDefKey(desiredArn)
	if desiredKey == "" {
		return false
	}
	if strings.Contains(desiredKey, ":") {
		return currentKey == desiredKey
	}
	return taskDefFamily(currentKey) == taskDefFamily(desiredKey)
}

func taskDefKey(arn string) string {
	if arn == "" {
		return arn
	}
	family := arn
	if idx := strings.LastIndex(arn, "/"); idx >= 0 && idx < len(arn)-1 {
		family = arn[idx+1:]
	}
	return family
}

func taskDefFamily(key string) string {
	if idx := strings.LastIndex(key, ":"); idx > 0 {
		return key[:idx]
	}
	return key
}

func (e *Executor) applyScheduledTasks(ctx context.Context, plan *ExecutionPlan) error {
	if len(plan.ScheduledTasks) == 0 || e.scheduledManager == nil {
		return nil
	}

	if e.schedulerClient != nil {
		if err := e.schedulerClient.EnsureScheduleGroup(ctx, e.scheduledManager.GroupName()); err != nil {
			return fmt.Errorf("failed to ensure schedule group: %w", err)
		}
	}

	e.tracker.PrintSection("\nScheduled Tasks")

	manifestTags := make(map[string]string)
	if plan.Manifest != nil {
		manifestTags = plan.Manifest.Tags
	}

	for _, task := range plan.ScheduledTasks {
		e.tracker.AddTask(task.Name, "scheduled-task")

		if task.Action == resources.ScheduledTaskActionNoop {
			e.tracker.SkipTask(task.Name, "unchanged")
			continue
		}

		e.tracker.StartTask(task.Name)

		if err := e.scheduledManager.Apply(ctx, task); err != nil {
			e.tracker.FailTask(task.Name, err.Error())
			return fmt.Errorf("scheduled task %s: %w", task.Name, err)
		}

		if task.Action == resources.ScheduledTaskActionCreate || task.Action == resources.ScheduledTaskActionUpdate {
			if err := e.tagSchedule(ctx, task, manifestTags); err != nil {
				log.Debug("failed to tag schedule", "name", task.Name, "error", err)
			}
		}

		e.tracker.CompleteTask(task.Name, task.ScheduleExpression())
	}

	return nil
}

func (e *Executor) tagSchedule(ctx context.Context, task *resources.ScheduledTaskResource, manifestTags map[string]string) error {
	if e.schedulerClient == nil {
		return nil
	}

	arn := task.Arn
	if arn == "" && task.Current != nil {
		arn = awssdk.ToString(task.Current.Arn)
	}
	if arn == "" {
		return nil
	}

	tags := resources.BuildSchedulerTags(manifestTags)
	return e.schedulerClient.TagResource(ctx, arn, tags)
}

func (e *Executor) Tracker() *Tracker {
	return e.tracker
}

// ResolveIngressTargetGroups resolves service backend references in services to target group ARNs
func ResolveIngressTargetGroups(services map[string]config.Service, ingress *config.Ingress, targetGroupArns map[int]string) {
	if ingress == nil {
		return
	}

	for i, rule := range ingress.Rules {
		if rule.Service == nil {
			continue
		}

		arn, ok := targetGroupArns[i]
		if !ok || arn == "" {
			continue
		}

		svcName := rule.Service.Name
		svc, ok := services[svcName]
		if !ok {
			continue
		}

		containerName := rule.Service.ContainerName
		containerPort := rule.Service.ContainerPort
		if containerName == "" || containerPort <= 0 {
			continue
		}

		// Prefer filling an empty target group on a matching container/port.
		updated := false
		for i := range svc.LoadBalancers {
			lb := &svc.LoadBalancers[i]
			if lb.TargetGroupArn == arn &&
				lb.ContainerName == containerName &&
				lb.ContainerPort == containerPort {
				updated = true
				break
			}
		}
		if !updated {
			for i := range svc.LoadBalancers {
				lb := &svc.LoadBalancers[i]
				if lb.TargetGroupArn == "" &&
					lb.ContainerName == containerName &&
					lb.ContainerPort == containerPort {
					lb.TargetGroupArn = arn
					updated = true
					break
				}
			}
		}
		if !updated {
			svc.LoadBalancers = append(svc.LoadBalancers, config.LoadBalancer{
				TargetGroupArn: arn,
				ContainerName:  containerName,
				ContainerPort:  containerPort,
			})
		}

		services[svcName] = svc
	}
}

func (e *Executor) resolveIngressTargetGroups(plan *ExecutionPlan, targetGroupArns map[int]string) {
	if plan == nil || plan.Manifest == nil || plan.Manifest.Ingress == nil {
		return
	}

	if len(targetGroupArns) == 0 {
		return
	}

	ResolveIngressTargetGroups(plan.Manifest.Services, plan.Manifest.Ingress, targetGroupArns)

	for name, node := range plan.Graph.nodes {
		svc := node.ServiceResource()
		if svc == nil {
			continue
		}

		updated, ok := plan.Manifest.Services[name]
		if !ok {
			continue
		}

		desiredName := ""
		desiredCluster := ""
		desiredTaskDef := ""
		if svc.Desired != nil {
			desiredName = svc.Desired.Name
			desiredCluster = svc.Desired.Cluster
			desiredTaskDef = svc.Desired.TaskDefinition
		}

		if desiredName != "" {
			updated.Name = desiredName
		}
		if updated.Cluster == "" && desiredCluster != "" {
			updated.Cluster = desiredCluster
		}
		if updated.TaskDefinition == "" && desiredTaskDef != "" {
			updated.TaskDefinition = desiredTaskDef
		}

		svc.Desired = new(config.Service)
		*svc.Desired = updated
		svc.RecalculateAction()
	}
}

// runPreHooks runs pre-deployment hooks for services in the given level
func (e *Executor) runPreHooks(ctx context.Context, plan *ExecutionPlan, serviceNames []string) error {
	if e.hookExecutor == nil {
		return nil
	}

	for _, serviceName := range serviceNames {
		node, ok := plan.Graph.GetNode(serviceName)
		if !ok {
			continue
		}

		svc := node.ServiceResource()
		if svc == nil || svc.Desired == nil || svc.Desired.Hooks == nil {
			continue
		}

		// Skip hooks if service is not being deployed
		if svc.Action == resources.ServiceActionNoop {
			continue
		}

		hook := svc.Desired.Hooks.PreHook
		if hook == nil {
			continue
		}

		// Resolve task definition ARN
		taskDefArn := e.resolveHookTaskDefArn(plan, hook.TaskDefinition)
		if taskDefArn == "" {
			return fmt.Errorf("pre-hook task definition not found: %s", hook.TaskDefinition)
		}

		// Show hook in tracker
		hookName := fmt.Sprintf("%s/pre-hook", serviceName)
		e.tracker.AddTask(hookName, "hook")
		e.tracker.StartTask(hookName)

		// Execute hook
		result, err := e.hookExecutor.ExecuteHook(
			ctx,
			resources.HookTypePre,
			serviceName,
			hook,
			taskDefArn,
			svc.Desired.NetworkConfiguration,
			svc.Desired.LaunchType,
			svc.Desired.PlatformVersion,
		)

		if err != nil {
			e.tracker.FailTask(hookName, err.Error())
			if result != nil && len(result.Logs) > 0 {
				e.tracker.PrintLogs(hookName, result.Logs)
			}
			return fmt.Errorf("pre-hook failed for %s: %w", serviceName, err)
		}

		e.tracker.CompleteTask(hookName, fmt.Sprintf("exit 0 [%s]", result.Duration.Round(time.Second)))
	}

	return nil
}

// runPostHooks runs post-deployment hooks (logs errors but doesn't fail deployment)
func (e *Executor) runPostHooks(ctx context.Context, plan *ExecutionPlan, serviceNames []string) {
	if e.hookExecutor == nil {
		return
	}

	for _, serviceName := range serviceNames {
		node, ok := plan.Graph.GetNode(serviceName)
		if !ok {
			continue
		}

		svc := node.ServiceResource()
		if svc == nil || svc.Desired == nil || svc.Desired.Hooks == nil {
			continue
		}

		// Skip hooks if service was not deployed
		if svc.Action == resources.ServiceActionNoop {
			continue
		}

		hook := svc.Desired.Hooks.PostHook
		if hook == nil {
			continue
		}

		taskDefArn := e.resolveHookTaskDefArn(plan, hook.TaskDefinition)
		if taskDefArn == "" {
			log.Warn("post-hook task definition not found", "service", serviceName, "taskDef", hook.TaskDefinition)
			continue
		}

		hookName := fmt.Sprintf("%s/post-hook", serviceName)
		e.tracker.AddTask(hookName, "hook")
		e.tracker.StartTask(hookName)

		result, err := e.hookExecutor.ExecuteHook(
			ctx,
			resources.HookTypePost,
			serviceName,
			hook,
			taskDefArn,
			svc.Desired.NetworkConfiguration,
			svc.Desired.LaunchType,
			svc.Desired.PlatformVersion,
		)

		if err != nil {
			e.tracker.FailTask(hookName, err.Error())
			if result != nil && len(result.Logs) > 0 {
				e.tracker.PrintLogs(hookName, result.Logs)
			}
			// Post-hook failure is logged but doesn't stop deployment
			log.Warn("post-hook failed", "service", serviceName, "error", err)
			continue
		}

		e.tracker.CompleteTask(hookName, fmt.Sprintf("exit 0 [%s]", result.Duration.Round(time.Second)))
	}
}

// resolveHookTaskDefArn finds the ARN for a hook's task definition
func (e *Executor) resolveHookTaskDefArn(plan *ExecutionPlan, taskDefName string) string {
	for _, td := range plan.TaskDefs {
		if td.Name == taskDefName {
			return td.ResolvedArn
		}
	}
	return ""
}

// printFailureLogs fetches stopped tasks via API and prints their logs
func (e *Executor) printFailureLogs(
	ctx context.Context,
	serviceName string,
	events []aws.ServiceEvent,
	svc *resources.ServiceResource,
	deploymentStartTime time.Time,
) {
	if e.cloudwatchClient == nil || e.logLines == 0 || e.ecsClient == nil {
		return
	}

	// Get stopped tasks via API
	stoppedTasks, err := e.ecsClient.GetStoppedTasks(ctx, serviceName, 3)
	if err != nil {
		log.Debug("failed to get stopped tasks", "error", err, "service", serviceName)
		return
	}

	if len(stoppedTasks) == 0 {
		return
	}

	// Get log configuration from task definition
	logGroup := ""
	logStreamPrefix := ""
	containerName := ""

	taskDefArn := ""
	if len(stoppedTasks) > 0 {
		taskDefArn = stoppedTasks[0].TaskDefinitionArn
	} else if svc != nil && svc.TaskDefinitionArn != "" {
		taskDefArn = svc.TaskDefinitionArn
	}

	if taskDefArn != "" {
		taskDef, err := e.ecsClient.DescribeTaskDefinition(ctx, taskDefArn)
		if err == nil && taskDef != nil && len(taskDef.ContainerDefinitions) > 0 {
			for _, container := range taskDef.ContainerDefinitions {
				if container.LogConfiguration != nil && container.LogConfiguration.LogDriver == "awslogs" {
					opts := container.LogConfiguration.Options
					if opts != nil {
						logGroup = opts["awslogs-group"]
						logStreamPrefix = opts["awslogs-stream-prefix"]
						if container.Name != nil {
							containerName = *container.Name
						}
						break
					}
				}
			}
		}
	}

	if logGroup == "" || containerName == "" {
		log.Debug("unable to determine log configuration", "service", serviceName)
		return
	}

	// Print stop reason and logs for each failed task
	for _, task := range stoppedTasks {
		if !taskStartedForDeployment(task, deploymentStartTime) {
			continue
		}
		if task.StoppedReason != "" {
			e.tracker.PrintLogs(serviceName, []string{
				fmt.Sprintf("Task %s stopped: %s", task.TaskID[:8], task.StoppedReason),
			})
		}

		// Build log stream name: {prefix}/{container}/{taskID}
		logStream := fmt.Sprintf("%s/%s/%s", logStreamPrefix, containerName, task.TaskID)
		if logStreamPrefix == "" {
			logStream = fmt.Sprintf("ecs/%s/%s", containerName, task.TaskID)
		}

		logs := e.fetchFailureLogs(ctx, logGroup, logStream)
		if len(logs) > 0 {
			e.tracker.PrintLogs(serviceName, logs)
		}
	}
}

func taskStartedForDeployment(task aws.TaskInfo, deploymentStartTime time.Time) bool {
	cutoff := deploymentStartTime.Add(-30 * time.Second)
	if task.StartedAt != nil {
		return !task.StartedAt.Before(cutoff)
	}
	if task.StoppedAt != nil {
		return !task.StoppedAt.Before(cutoff)
	}

	return false
}

// fetchFailureLogs fetches CloudWatch logs for a failed task.
func (e *Executor) fetchFailureLogs(ctx context.Context, logGroup, logStream string) []string {
	if e.cloudwatchClient == nil || e.logLines == 0 {
		return nil
	}

	limit := e.logLines
	if limit < 0 {
		limit = 0 // fetch all
	}

	logs, err := e.cloudwatchClient.GetLogEvents(ctx, logGroup, logStream, limit)
	if err != nil {
		log.Debug("failed to fetch logs", "error", err, "logGroup", logGroup, "logStream", logStream)
		return nil
	}

	return logs
}
