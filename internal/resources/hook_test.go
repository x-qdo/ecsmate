package resources

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	awsclient "github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
)

type fakeHookECSClient struct {
	waitCalls  int
	stoppedArn string
	stopReason string
}

func (f *fakeHookECSClient) RunTask(context.Context, *awsclient.RunTaskInput) (*awsclient.RunTaskOutput, error) {
	return &awsclient.RunTaskOutput{
		TaskArns: []string{"arn:aws:ecs:eu-west-1:123456789012:task/cluster/task-id"},
	}, nil
}

func (f *fakeHookECSClient) WaitForTaskStopped(context.Context, string, time.Duration) (*awsclient.TaskInfo, error) {
	f.waitCalls++
	if f.waitCalls == 1 {
		return nil, errors.New("timeout waiting for task to stop")
	}
	return &awsclient.TaskInfo{LastStatus: "STOPPED"}, nil
}

func (f *fakeHookECSClient) StopTask(_ context.Context, taskArn, reason string) error {
	f.stoppedArn = taskArn
	f.stopReason = reason
	return nil
}

func (f *fakeHookECSClient) DescribeTaskDefinition(context.Context, string) (*types.TaskDefinition, error) {
	return nil, nil
}

func TestHookExecutor_StopsTaskWhenWaitFails(t *testing.T) {
	ecsClient := &fakeHookECSClient{}
	executor := &HookExecutor{ecsClient: ecsClient}

	result, err := executor.ExecuteHook(
		context.Background(),
		HookTypePre,
		"api",
		&config.Hook{TaskDefinition: "migration", Timeout: 60},
		"arn:aws:ecs:eu-west-1:123456789012:task-definition/migration:2",
		nil,
		"",
		"",
	)

	if err == nil || !strings.Contains(err.Error(), "timeout waiting for task to stop") {
		t.Fatalf("expected original wait error, got %v", err)
	}
	if result == nil || result.TaskID != "task-id" {
		t.Fatalf("expected timed-out task diagnostics, got %+v", result)
	}
	if ecsClient.stoppedArn != result.TaskArn {
		t.Fatalf("expected timed-out task %q to be stopped, got %q", result.TaskArn, ecsClient.stoppedArn)
	}
	if ecsClient.stopReason != "ecsmate pre-hook did not complete" {
		t.Fatalf("unexpected stop reason %q", ecsClient.stopReason)
	}
	if ecsClient.waitCalls != 2 {
		t.Fatalf("expected one deployment wait and one cleanup wait, got %d", ecsClient.waitCalls)
	}
}
