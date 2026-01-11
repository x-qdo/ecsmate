package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

type HookType string

const (
	HookTypePre  HookType = "pre"
	HookTypePost HookType = "post"
)

// HookResult contains the result of hook execution
type HookResult struct {
	TaskArn    string
	TaskID     string
	ExitCode   int
	StoppedAt  *time.Time
	StopReason string
	Duration   time.Duration
	Logs       []string
}

// HookExecutor runs deployment hooks
type HookExecutor struct {
	ecsClient        *awsclient.ECSClient
	cloudwatchClient *awsclient.CloudWatchLogsClient
}

func NewHookExecutor(ecsClient *awsclient.ECSClient, cwClient *awsclient.CloudWatchLogsClient) *HookExecutor {
	return &HookExecutor{
		ecsClient:        ecsClient,
		cloudwatchClient: cwClient,
	}
}

// ExecuteHook runs a hook task and waits for completion
func (e *HookExecutor) ExecuteHook(
	ctx context.Context,
	hookType HookType,
	serviceName string,
	hook *config.Hook,
	taskDefArn string,
	networkConfig *config.NetworkConfiguration,
	launchType string,
	platformVersion string,
) (*HookResult, error) {

	log.Info("executing hook",
		"type", hookType,
		"service", serviceName,
		"taskDef", hook.TaskDefinition)

	// Build task overrides from hook config
	overrides := e.buildTaskOverrides(hook)

	// Build network configuration
	var netConfig *types.NetworkConfiguration
	if networkConfig != nil {
		netConfig = &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        networkConfig.Subnets,
				SecurityGroups: networkConfig.SecurityGroups,
			},
		}
		if networkConfig.AssignPublicIp != "" {
			netConfig.AwsvpcConfiguration.AssignPublicIp = types.AssignPublicIp(networkConfig.AssignPublicIp)
		}
	}

	// Run the task
	startTime := time.Now()
	runOutput, err := e.ecsClient.RunTask(ctx, &awsclient.RunTaskInput{
		TaskDefinition:       taskDefArn,
		LaunchType:           launchType,
		PlatformVersion:      platformVersion,
		NetworkConfiguration: netConfig,
		Overrides:            overrides,
		Count:                1,
		Group:                fmt.Sprintf("ecsmate:hook:%s:%s", hookType, serviceName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to run %s hook: %w", hookType, err)
	}

	if len(runOutput.TaskArns) == 0 {
		return nil, fmt.Errorf("%s hook: no tasks started", hookType)
	}

	taskArn := runOutput.TaskArns[0]
	taskID := awsclient.ExtractTaskIDFromArn(taskArn)

	// Show truncated ID for readability
	shortID := taskID
	if len(taskID) > 8 {
		shortID = taskID[:8]
	}

	log.Info("hook task started",
		"type", hookType,
		"taskID", shortID)

	// Wait for task to complete
	timeout := time.Duration(hook.Timeout) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Minute
	}

	taskInfo, err := e.ecsClient.WaitForTaskStopped(ctx, taskArn, timeout)
	if err != nil {
		return nil, fmt.Errorf("%s hook: %w", hookType, err)
	}

	duration := time.Since(startTime)

	// Get exit code
	exitCode, _ := awsclient.GetTaskExitCode(taskInfo)

	result := &HookResult{
		TaskArn:    taskArn,
		TaskID:     taskID,
		ExitCode:   exitCode,
		StoppedAt:  taskInfo.StoppedAt,
		StopReason: taskInfo.StoppedReason,
		Duration:   duration,
	}

	// Fetch logs if available
	if e.cloudwatchClient != nil {
		result.Logs = e.fetchHookLogs(ctx, taskDefArn, taskID)
	}

	if exitCode != 0 {
		return result, fmt.Errorf("%s hook failed with exit code %d", hookType, exitCode)
	}

	log.Info("hook completed successfully",
		"type", hookType,
		"taskID", shortID,
		"duration", duration.Round(time.Second))

	return result, nil
}

func (e *HookExecutor) buildTaskOverrides(hook *config.Hook) *types.TaskOverride {
	if len(hook.ContainerOverrides) == 0 {
		return nil
	}

	overrides := &types.TaskOverride{}

	for _, co := range hook.ContainerOverrides {
		containerOverride := types.ContainerOverride{
			Name: aws.String(co.Name),
		}

		if len(co.Command) > 0 {
			containerOverride.Command = co.Command
		}
		if len(co.Environment) > 0 {
			for _, env := range co.Environment {
				containerOverride.Environment = append(containerOverride.Environment,
					types.KeyValuePair{
						Name:  aws.String(env.Name),
						Value: aws.String(env.Value),
					})
			}
		}

		overrides.ContainerOverrides = append(overrides.ContainerOverrides, containerOverride)
	}

	return overrides
}

func (e *HookExecutor) fetchHookLogs(ctx context.Context, taskDefArn, taskID string) []string {
	if e.cloudwatchClient == nil || e.ecsClient == nil {
		return nil
	}

	// Get task definition to find log configuration
	taskDef, err := e.ecsClient.DescribeTaskDefinition(ctx, taskDefArn)
	if err != nil {
		log.Debug("failed to describe task definition for logs", "error", err)
		return nil
	}

	if taskDef == nil || len(taskDef.ContainerDefinitions) == 0 {
		return nil
	}

	// Find first container with awslogs configuration
	var logGroup, logStreamPrefix, containerName string
	for _, container := range taskDef.ContainerDefinitions {
		if container.LogConfiguration != nil && container.LogConfiguration.LogDriver == types.LogDriverAwslogs {
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

	if logGroup == "" || containerName == "" {
		return nil
	}

	// Build log stream name: {prefix}/{container}/{taskID}
	logStream := fmt.Sprintf("%s/%s/%s", logStreamPrefix, containerName, taskID)
	if logStreamPrefix == "" {
		logStream = fmt.Sprintf("ecs/%s/%s", containerName, taskID)
	}

	logs, err := e.cloudwatchClient.GetLogEvents(ctx, logGroup, logStream, 50)
	if err != nil {
		log.Debug("failed to fetch hook logs", "error", err, "logGroup", logGroup, "logStream", logStream)
		return nil
	}

	return logs
}
