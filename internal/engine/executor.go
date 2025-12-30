package engine

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/log"
	"github.com/qdo/ecsmate/internal/resources"
)

type Executor struct {
	ecsClient        *aws.ECSClient
	schedulerClient  *aws.SchedulerClient
	taskDefManager   *resources.TaskDefManager
	serviceManager   *resources.ServiceManager
	scheduledManager *resources.ScheduledTaskManager
	tracker          *Tracker
	noWait           bool
	timeout          time.Duration
}

type ExecutorConfig struct {
	ECSClient        *aws.ECSClient
	SchedulerClient  *aws.SchedulerClient
	TaskDefManager   *resources.TaskDefManager
	ServiceManager   *resources.ServiceManager
	ScheduledManager *resources.ScheduledTaskManager
	Output           io.Writer
	NoColor          bool
	NoWait           bool
	Timeout          time.Duration
}

func NewExecutor(cfg ExecutorConfig) *Executor {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 15 * time.Minute
	}

	return &Executor{
		ecsClient:        cfg.ECSClient,
		schedulerClient:  cfg.SchedulerClient,
		taskDefManager:   cfg.TaskDefManager,
		serviceManager:   cfg.ServiceManager,
		scheduledManager: cfg.ScheduledManager,
		tracker:          NewTracker(cfg.Output, cfg.NoColor),
		noWait:           cfg.NoWait,
		timeout:          timeout,
	}
}

func (e *Executor) Execute(ctx context.Context, plan *ExecutionPlan, cluster string) error {
	e.tracker.PrintHeader(cluster)

	if err := e.registerTaskDefs(ctx, plan); err != nil {
		return fmt.Errorf("failed to register task definitions: %w", err)
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

func (e *Executor) registerTaskDefs(ctx context.Context, plan *ExecutionPlan) error {
	if len(plan.TaskDefs) == 0 {
		return nil
	}

	e.tracker.PrintSection("Task Definitions")

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

		for _, serviceName := range level {
			node, ok := plan.Graph.GetNode(serviceName)
			if !ok {
				continue
			}

			svc := node.Resource
			wg.Add(1)

			go func(svc *resources.ServiceResource) {
				defer wg.Done()

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

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for service to stabilize")
		case <-ticker.C:
			status, err := e.serviceManager.GetDeploymentStatus(ctx, svc.Name)
			if err != nil {
				log.Warn("failed to get deployment status", "service", svc.Name, "error", err)
				continue
			}

			// Display live progress
			e.tracker.PrintDeploymentProgress(DeploymentProgress{
				ServiceName:  svc.Name,
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
