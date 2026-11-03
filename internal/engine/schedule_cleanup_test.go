package engine

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"

	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/diff"
	"github.com/x-qdo/ecsmate/internal/resources"
)

type scheduleCleanupFake struct {
	resources.SchedulerAPI
	names                           []string
	group                           string
	createErr, updateErr, deleteErr error
}

func (f *scheduleCleanupFake) CreateSchedule(_ context.Context, input *scheduler.CreateScheduleInput) (string, error) {
	f.names = append(f.names, aws.ToString(input.Name))
	f.group = aws.ToString(input.GroupName)
	return "arn:schedule", f.createErr
}

func (f *scheduleCleanupFake) UpdateSchedule(_ context.Context, input *scheduler.UpdateScheduleInput) error {
	f.names = append(f.names, aws.ToString(input.Name))
	f.group = aws.ToString(input.GroupName)
	return f.updateErr
}

func (f *scheduleCleanupFake) DeleteSchedule(_ context.Context, name, group string) error {
	f.names = append(f.names, name)
	f.group = group
	return f.deleteErr
}

func TestScheduledTaskCleanupPlanAndExecution(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "delete failure"}[fail], func(t *testing.T) {
			state := &resources.DesiredState{
				Manifest: &config.Manifest{Name: "app"},
				ScheduledTasks: map[string]*resources.ScheduledTaskResource{
					"orphan-app-stage_old": {Name: "app-stage_old", Action: resources.ScheduledTaskActionDelete},
				},
			}
			plan := NewPlanner().GeneratePlan(state)
			if !plan.HasChanges() || plan.Summary.Deletes != 1 || len(plan.Entries) != 1 || plan.Entries[0].Type != diff.DiffTypeDelete {
				t.Fatalf("expected one DELETE entry: %+v", plan)
			}
			execution, err := BuildExecutionPlan(state)
			if err != nil {
				t.Fatal(err)
			}
			f := &scheduleCleanupFake{}
			if fail {
				f.deleteErr = errors.New("delete denied")
			}
			executor := NewExecutor(ExecutorConfig{ScheduledManager: resources.NewScheduledTaskManager(f, "app"), Output: io.Discard, NoColor: true})
			err = executor.Execute(context.Background(), execution, "test-cluster")
			if !errors.Is(err, f.deleteErr) {
				t.Fatalf("expected %v, got %v", f.deleteErr, err)
			}
			if len(f.names) != 1 || f.names[0] != "app-stage_old" || f.group != "app" {
				t.Fatalf("wrong deletion: %+v", f)
			}
		})
	}
}

func TestScheduledTaskCleanupRunsAfterSuccessfulWrites(t *testing.T) {
	writeErr := errors.New("schedule write denied")
	for _, tc := range []struct {
		name       string
		failAction resources.ScheduledTaskAction
		wantCalls  []string
	}{
		{name: "success", wantCalls: []string{"app-stage_new", "app-stage_current", "app-stage_old_a", "app-stage_old_b"}},
		{name: "create failure", failAction: resources.ScheduledTaskActionCreate, wantCalls: []string{"app-stage_new"}},
		{name: "update failure", failAction: resources.ScheduledTaskActionUpdate, wantCalls: []string{"app-stage_new", "app-stage_current"}},
		{name: "delete failure", failAction: resources.ScheduledTaskActionDelete, wantCalls: []string{"app-stage_new", "app-stage_current", "app-stage_old_a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &scheduleCleanupFake{}
			var wantErr error
			switch tc.failAction {
			case resources.ScheduledTaskActionCreate:
				f.createErr, wantErr = writeErr, writeErr
			case resources.ScheduledTaskActionUpdate:
				f.updateErr, wantErr = writeErr, writeErr
			case resources.ScheduledTaskActionDelete:
				f.deleteErr, wantErr = writeErr, writeErr
			}
			desired := &config.ScheduledTask{Cluster: "arn:cluster", ScheduleType: "rate", ScheduleExpression: "1 hour", TaskCount: 1}
			// Plan order is arbitrary: put deletions before and between desired writes.
			plan := &ExecutionPlan{ScheduledTasks: []*resources.ScheduledTaskResource{
				{Name: "app-stage_old_a", Action: resources.ScheduledTaskActionDelete},
				{Name: "app-stage_new", Action: resources.ScheduledTaskActionCreate, Desired: desired},
				{Name: "app-stage_old_b", Action: resources.ScheduledTaskActionDelete},
				{Name: "app-stage_current", Action: resources.ScheduledTaskActionUpdate, Desired: desired},
				{Name: "app-stage_unchanged", Action: resources.ScheduledTaskActionNoop},
			}}
			executor := NewExecutor(ExecutorConfig{ScheduledManager: resources.NewScheduledTaskManager(f, "app"), Output: io.Discard, NoColor: true})
			err := executor.applyScheduledTasks(context.Background(), plan)
			if !errors.Is(err, wantErr) {
				t.Fatalf("expected %v, got %v", wantErr, err)
			}
			if !slices.Equal(f.names, tc.wantCalls) {
				t.Fatalf("calls = %v, want %v", f.names, tc.wantCalls)
			}
			if f.group != "app" {
				t.Fatalf("wrong schedule group: %s", f.group)
			}
		})
	}
}
