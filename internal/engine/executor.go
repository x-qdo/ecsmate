package engine

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
	"github.com/qdo/ecsmate/internal/resources"
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
	tracker             *Tracker
	noWait              bool
	timeout             time.Duration
	maxParallel         int
	logLines            int
}

type ExecutorConfig struct {
	ECSClient        *aws.ECSClient
	SchedulerClient  *aws.SchedulerClient
	CloudWatchClient *aws.CloudWatchLogsClient
	ELBV2Client      *aws.ELBV2Client
	TaskDefManager   *resources.TaskDefManager
	ServiceManager   *resources.ServiceManager
	ScheduledManager *resources.ScheduledTaskManager
	Output           io.Writer
	NoColor          bool
	NoWait           bool
	Timeout          time.Duration
	MaxParallel      int
	LogLines         int // -1=all, 0=none, N=limit
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

	return e
}

func (e *Executor) Execute(ctx context.Context, plan *ExecutionPlan, cluster string) error {
	e.tracker.PrintHeader(cluster)

	// Apply log groups first (before task defs)
	if err := e.applyLogGroups(ctx, plan); err != nil {
		return fmt.Errorf("failed to apply log groups: %w", err)
	}

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
			if node == nil || node.Resource == nil || node.Resource.Desired == nil {
				continue
			}
			taskDefName := node.Resource.Desired.TaskDefinition
			if taskDefName == "" {
				continue
			}
			if arn, ok := taskDefArns[taskDefName]; ok && arn != "" {
				log.Debug("refreshing service task definition", "service", node.Name, "taskDef", taskDefName, "arn", arn)
				node.Resource.TaskDefinitionArn = arn
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

		if resource.Action == resources.LogGroupActionNoop {
			e.tracker.SkipTask(name, "unchanged")
			continue
		}

		e.tracker.StartTask(name)

		if err := e.logGroupManager.Apply(ctx, resource); err != nil {
			e.tracker.FailTask(name, err.Error())
			return fmt.Errorf("log group %s: %w", name, err)
		}

		e.tracker.CompleteTask(name, string(resource.Action))
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
		var idx int
		fmt.Sscanf(key, "rule-%d", &idx)
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

	ruleResources, err := e.listenerRuleManager.BuildResources(ctx, ingress.ListenerArn, ingress.Rules, targetGroupArns)
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

			svc := node.Resource
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
					e.printFailureLogs(ctx, serviceName, status.Events, svc)
					return fmt.Errorf("deployment rolled back to %s", status.TaskDefinition)
				}
				return nil
			case "FAILED":
				e.printFailureLogs(ctx, serviceName, status.Events, svc)
				return fmt.Errorf("deployment failed (circuit breaker triggered)")
			}

			if rollbackInProgress(expectedTaskDef, status) {
				e.printFailureLogs(ctx, serviceName, status.Events, svc)
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

	e.tracker.PrintSection("\nScheduled Tasks")

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

		e.tracker.CompleteTask(task.Name, task.ScheduleExpression())
	}

	return nil
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
		if node == nil || node.Resource == nil {
			continue
		}

		updated, ok := plan.Manifest.Services[name]
		if !ok {
			continue
		}

		desiredName := ""
		desiredCluster := ""
		desiredTaskDef := ""
		if node.Resource.Desired != nil {
			desiredName = node.Resource.Desired.Name
			desiredCluster = node.Resource.Desired.Cluster
			desiredTaskDef = node.Resource.Desired.TaskDefinition
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

		node.Resource.Desired = new(config.Service)
		*node.Resource.Desired = updated
		node.Resource.RecalculateAction()
	}
}

// printFailureLogs fetches stopped tasks via API and prints their logs
func (e *Executor) printFailureLogs(ctx context.Context, serviceName string, events []aws.ServiceEvent, svc *resources.ServiceResource) {
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
