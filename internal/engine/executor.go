package engine

import (
	"context"
	"fmt"
	"io"
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

	if err := e.deployServices(ctx, plan); err != nil {
		return fmt.Errorf("failed to deploy services: %w", err)
	}

	// Apply listener rules (after services)
	if err := e.applyListenerRules(ctx, plan, targetGroupArns); err != nil {
		return fmt.Errorf("failed to apply listener rules: %w", err)
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

			// Display live progress
			e.tracker.PrintDeploymentProgress(DeploymentProgress{
				ServiceName:  serviceName,
				DesiredCount: status.DesiredCount,
				RunningCount: status.RunningCount,
				PendingCount: status.PendingCount,
				RolloutState: status.RolloutState,
				Healthy:      status.RunningCount,
			})

			switch status.RolloutState {
			case "COMPLETED":
				return nil
			case "FAILED":
				return fmt.Errorf("deployment failed (circuit breaker triggered)")
			}

			if status.RunningCount == status.DesiredCount && status.PendingCount == 0 {
				return nil
			}
		}
	}
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

		node.Resource.Desired = new(config.Service)
		*node.Resource.Desired = updated
		node.Resource.RecalculateAction()
	}
}
