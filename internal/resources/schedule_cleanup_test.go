package resources

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"
	"github.com/aws/aws-sdk-go-v2/service/scheduler/types"

	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/log"
)

func TestValidateScheduleCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, task, wantErr string
		prune                       bool
	}{
		{name: "unconfigured", task: "nightly"},
		{name: "prune needs scope", prune: true, wantErr: "requires scheduleCleanupPrefix"},
		{name: "stage scope", prefix: "app-stage_", task: "stage_nightly", prune: true},
		{name: "already prefixed", prefix: "app-stage_", task: "app-stage_nightly"},
		{name: "production scope", prefix: "app-", task: "nightly"},
		{name: "other manifest", prefix: "other-stage_", wantErr: "must start with"},
		{name: "missing separator", prefix: "app", wantErr: "must start with"},
		{name: "wildcard", prefix: "app-*", wantErr: "1-64 characters"},
		{name: "too long", prefix: "app-" + strings.Repeat("x", 61), wantErr: "1-64 characters"},
		{name: "other environment", prefix: "app-stage_", task: "sandbox_nightly", wantErr: "outside scheduleCleanupPrefix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &config.Manifest{Name: "app", ScheduleCleanupPrefix: tc.prefix, ScheduledTasks: map[string]config.ScheduledTask{}}
			if tc.task != "" {
				m.ScheduledTasks[tc.task] = config.ScheduledTask{}
			}
			err := ValidateScheduleCleanup(m, tc.prune)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// All unexpected operations, including writes, panic through the nil interface.
type cleanupSchedulerFake struct {
	SchedulerAPI
	schedules           []types.ScheduleSummary
	listErr             error
	group, prefix       string
	listCalls, getCalls int
}

func (f *cleanupSchedulerFake) ListSchedules(_ context.Context, group, prefix string) ([]types.ScheduleSummary, error) {
	f.listCalls++
	f.group, f.prefix = group, prefix
	return f.schedules, f.listErr
}

func (f *cleanupSchedulerFake) GetSchedule(context.Context, string, string) (*scheduler.GetScheduleOutput, error) {
	f.getCalls++
	return nil, &types.ResourceNotFoundException{}
}

func TestBuildScheduledTasksCleanup(t *testing.T) {
	readErr := errors.New("access denied")
	for _, tc := range []struct {
		name                                         string
		prune, empty, noLive, unconfigured, noClient bool
		readErr                                      error
		wantDeletes                                  int
		wantErr, wantWarning                         string
	}{
		{name: "unconfigured", unconfigured: true},
		{name: "report", wantWarning: "name=app-stage_old"},
		{name: "prune", prune: true, wantDeletes: 1},
		{name: "no orphans", noLive: true},
		{name: "empty report", empty: true, wantWarning: "cleanup requires manual review"},
		{name: "empty prune", empty: true, prune: true, wantErr: "no scheduled tasks rendered"},
		{name: "empty group", empty: true, noLive: true, prune: true},
		{name: "report read failure", readErr: readErr, wantWarning: "schedule cleanup check failed"},
		{name: "prune read failure", readErr: readErr, prune: true, wantErr: "access denied"},
		{name: "report missing client", noClient: true, wantWarning: "schedule cleanup check failed"},
		{name: "prune missing client", noClient: true, prune: true, wantErr: "scheduler client not initialized"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			log.Init(log.LevelWarn, &output)
			t.Cleanup(func() { log.Init(log.LevelInfo, nil) })
			m := &config.Manifest{Name: "app", ScheduleCleanupPrefix: "app-stage_", ScheduledTasks: map[string]config.ScheduledTask{}}
			if !tc.empty {
				m.ScheduledTasks["stage_current"] = config.ScheduledTask{TaskDefinition: "command", Cluster: "arn:cluster"}
			}
			if tc.unconfigured {
				m.ScheduleCleanupPrefix = ""
			}
			f := &cleanupSchedulerFake{listErr: tc.readErr}
			if !tc.noLive {
				for _, name := range []string{"app-stage_current", "app-stage_old", "app-sandbox_other", "app-other"} {
					f.schedules = append(f.schedules, types.ScheduleSummary{Name: aws.String(name), Arn: aws.String("arn:" + name)})
				}
			}
			b := &ResourceBuilder{scheduledManager: NewScheduledTaskManager(f, "app"), pruneSchedules: tc.prune}
			if tc.noClient {
				b.scheduledManager = nil
			}
			state := &DesiredState{TaskDefs: map[string]*TaskDefResource{"command": {ResolvedArn: "arn:taskdef"}}, ScheduledTasks: map[string]*ScheduledTaskResource{}}
			err := b.buildScheduledTasks(context.Background(), m, state, "arn:role")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
			deletes := 0
			for _, task := range state.ScheduledTasks {
				if task.Action == ScheduledTaskActionDelete {
					deletes++
					if task.Name != "app-stage_old" {
						t.Fatalf("unexpected deletion: %s", task.Name)
					}
				}
			}
			if deletes != tc.wantDeletes {
				t.Fatalf("deletes = %d, want %d", deletes, tc.wantDeletes)
			}
			wantLists := 1
			if tc.unconfigured || tc.noClient {
				wantLists = 0
			}
			if f.listCalls != wantLists {
				t.Fatalf("list calls = %d, want %d", f.listCalls, wantLists)
			}
			if wantLists > 0 && (f.group != "app" || f.prefix != "app-stage_") {
				t.Fatalf("wrong cleanup scope: %s/%s", f.group, f.prefix)
			}
			wantGets := len(m.ScheduledTasks)
			if tc.noClient {
				wantGets = 0
			}
			if f.getCalls != wantGets {
				t.Fatalf("get calls = %d, want only %d desired-task reads", f.getCalls, wantGets)
			}
			if tc.wantWarning != "" && !strings.Contains(output.String(), tc.wantWarning) {
				t.Fatalf("missing warning %q in %s", tc.wantWarning, output.String())
			}
		})
	}
}
