package resources

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

type LogGroupAction string

const (
	LogGroupActionCreate LogGroupAction = "CREATE"
	LogGroupActionUpdate LogGroupAction = "UPDATE"
	LogGroupActionDelete LogGroupAction = "DELETE"
	LogGroupActionNoop   LogGroupAction = "NOOP"
)

// LogGroupSpec represents the desired state for a log group (extracted from LogConfiguration)
type LogGroupSpec struct {
	Name            string
	RetentionInDays int
	KMSKeyID        string
	Tags            map[string]string
}

type LogGroupResource struct {
	Name    string
	Desired *LogGroupSpec
	Current *types.LogGroup
	Action  LogGroupAction
}

type LogGroupManager struct {
	client *awsclient.CloudWatchLogsClient
}

func NewLogGroupManager(client *awsclient.CloudWatchLogsClient) *LogGroupManager {
	return &LogGroupManager{
		client: client,
	}
}

// ExtractLogGroups extracts log group specs from task definitions where createLogGroup is true
func ExtractLogGroups(manifest *config.Manifest) map[string]*LogGroupSpec {
	logGroups := make(map[string]*LogGroupSpec)

	for _, td := range manifest.TaskDefinitions {
		for _, container := range td.ContainerDefinitions {
			if container.LogConfiguration == nil {
				continue
			}
			lc := container.LogConfiguration

			// Only process awslogs driver with createLogGroup enabled
			if lc.LogDriver != "awslogs" || !lc.CreateLogGroup {
				continue
			}

			logGroupName := lc.Options["awslogs-group"]
			if logGroupName == "" {
				continue
			}

			// Use log group name as key to dedupe
			if _, exists := logGroups[logGroupName]; exists {
				continue
			}

			logGroups[logGroupName] = &LogGroupSpec{
				Name:            logGroupName,
				RetentionInDays: lc.RetentionInDays,
				KMSKeyID:        lc.KMSKeyID,
				Tags:            lc.LogGroupTags,
			}
		}
	}

	return logGroups
}

func (m *LogGroupManager) BuildResource(ctx context.Context, spec *LogGroupSpec) (*LogGroupResource, error) {
	resource := &LogGroupResource{
		Name:    spec.Name,
		Desired: spec,
	}

	if err := m.discoverLogGroup(ctx, resource); err != nil {
		log.Debug("failed to discover log group", "name", spec.Name, "error", err)
	}

	resource.determineAction()

	return resource, nil
}

func (m *LogGroupManager) discoverLogGroup(ctx context.Context, resource *LogGroupResource) error {
	if resource.Desired == nil {
		return nil
	}

	log.Debug("discovering log group", "name", resource.Desired.Name)

	lg, err := m.client.DescribeLogGroup(ctx, resource.Desired.Name)
	if err != nil {
		return err
	}

	resource.Current = lg
	return nil
}

func (resource *LogGroupResource) determineAction() {
	if resource.Desired == nil {
		if resource.Current != nil {
			resource.Action = LogGroupActionDelete
		} else {
			resource.Action = LogGroupActionNoop
		}
		return
	}

	if resource.Current == nil {
		resource.Action = LogGroupActionCreate
		return
	}

	// Check if retention needs update
	currentRetention := 0
	if resource.Current.RetentionInDays != nil {
		currentRetention = int(aws.ToInt32(resource.Current.RetentionInDays))
	}

	if resource.Desired.RetentionInDays != currentRetention && resource.Desired.RetentionInDays > 0 {
		resource.Action = LogGroupActionUpdate
		return
	}

	resource.Action = LogGroupActionNoop
}

func (m *LogGroupManager) Create(ctx context.Context, resource *LogGroupResource) error {
	log.Info("creating log group", "name", resource.Desired.Name)

	if err := m.client.CreateLogGroup(ctx, &awsclient.CreateLogGroupInput{
		Name:     resource.Desired.Name,
		KMSKeyID: resource.Desired.KMSKeyID,
		Tags:     resource.Desired.Tags,
	}); err != nil {
		return err
	}

	// Set retention policy if specified
	if resource.Desired.RetentionInDays > 0 {
		if err := m.client.SetRetentionPolicy(ctx, resource.Desired.Name, resource.Desired.RetentionInDays); err != nil {
			return fmt.Errorf("failed to set retention policy: %w", err)
		}
	}

	return nil
}

func (m *LogGroupManager) Update(ctx context.Context, resource *LogGroupResource) error {
	log.Info("updating log group", "name", resource.Desired.Name)

	// Update retention policy
	if resource.Desired.RetentionInDays > 0 {
		if err := m.client.SetRetentionPolicy(ctx, resource.Desired.Name, resource.Desired.RetentionInDays); err != nil {
			return fmt.Errorf("failed to update retention policy: %w", err)
		}
	}

	// Update tags if needed
	if len(resource.Desired.Tags) > 0 {
		if err := m.client.TagLogGroup(ctx, resource.Desired.Name, resource.Desired.Tags); err != nil {
			return fmt.Errorf("failed to update tags: %w", err)
		}
	}

	return nil
}

func (m *LogGroupManager) Delete(ctx context.Context, resource *LogGroupResource) error {
	log.Info("deleting log group", "name", resource.Current.LogGroupName)
	return m.client.DeleteLogGroup(ctx, aws.ToString(resource.Current.LogGroupName))
}

func (m *LogGroupManager) Apply(ctx context.Context, resource *LogGroupResource) error {
	switch resource.Action {
	case LogGroupActionCreate:
		return m.Create(ctx, resource)
	case LogGroupActionUpdate:
		return m.Update(ctx, resource)
	case LogGroupActionDelete:
		return m.Delete(ctx, resource)
	case LogGroupActionNoop:
		log.Debug("no changes detected, skipping log group", "name", resource.Name)
		return nil
	default:
		return fmt.Errorf("unknown action: %s", resource.Action)
	}
}
