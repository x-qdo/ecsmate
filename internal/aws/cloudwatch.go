package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	"github.com/x-qdo/ecsmate/internal/log"
)

type CloudWatchLogsClient struct {
	client *cloudwatchlogs.Client
}

func NewCloudWatchLogsClient(ctx context.Context, region string) (*CloudWatchLogsClient, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &CloudWatchLogsClient{
		client: cloudwatchlogs.NewFromConfig(cfg),
	}, nil
}

type CreateLogGroupInput struct {
	Name     string
	KMSKeyID string
	Tags     map[string]string
}

type PutSubscriptionFilterInput struct {
	LogGroupName   string
	Name           string
	DestinationArn string
	FilterPattern  string
	RoleArn        string
	Distribution   string
}

func (c *CloudWatchLogsClient) CreateLogGroup(ctx context.Context, input *CreateLogGroupInput) error {
	log.Debug("creating CloudWatch log group", "name", input.Name)

	createInput := &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String(input.Name),
	}

	if input.KMSKeyID != "" {
		createInput.KmsKeyId = aws.String(input.KMSKeyID)
	}

	if len(input.Tags) > 0 {
		createInput.Tags = input.Tags
	}

	_, err := c.client.CreateLogGroup(ctx, createInput)
	if err != nil {
		var alreadyExists *types.ResourceAlreadyExistsException
		if errors.As(err, &alreadyExists) {
			log.Debug("log group already exists", "name", input.Name)
			return nil
		}
		return fmt.Errorf("failed to create log group %s: %w", input.Name, err)
	}

	log.Info("created CloudWatch log group", "name", input.Name)
	return nil
}

func (c *CloudWatchLogsClient) SetRetentionPolicy(ctx context.Context, logGroupName string, retentionDays int) error {
	log.Debug("setting retention policy", "logGroup", logGroupName, "days", retentionDays)

	_, err := c.client.PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String(logGroupName),
		RetentionInDays: aws.Int32(int32(retentionDays)),
	})
	if err != nil {
		return fmt.Errorf("failed to set retention policy for %s: %w", logGroupName, err)
	}

	log.Info("set retention policy", "logGroup", logGroupName, "days", retentionDays)
	return nil
}

func (c *CloudWatchLogsClient) DescribeLogGroup(ctx context.Context, logGroupName string) (*types.LogGroup, error) {
	log.Debug("describing log group", "name", logGroupName)

	out, err := c.client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: aws.String(logGroupName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe log group %s: %w", logGroupName, err)
	}

	for _, lg := range out.LogGroups {
		if aws.ToString(lg.LogGroupName) == logGroupName {
			return &lg, nil
		}
	}

	return nil, nil
}

func (c *CloudWatchLogsClient) DeleteLogGroup(ctx context.Context, logGroupName string) error {
	log.Debug("deleting log group", "name", logGroupName)

	_, err := c.client.DeleteLogGroup(ctx, &cloudwatchlogs.DeleteLogGroupInput{
		LogGroupName: aws.String(logGroupName),
	})
	if err != nil {
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			log.Debug("log group not found", "name", logGroupName)
			return nil
		}
		return fmt.Errorf("failed to delete log group %s: %w", logGroupName, err)
	}

	log.Info("deleted CloudWatch log group", "name", logGroupName)
	return nil
}

func (c *CloudWatchLogsClient) TagLogGroup(ctx context.Context, logGroupName string, tags map[string]string) error {
	if len(tags) == 0 {
		return nil
	}

	log.Debug("tagging log group", "name", logGroupName, "tags", len(tags))

	// Get the log group ARN first
	lg, err := c.DescribeLogGroup(ctx, logGroupName)
	if err != nil {
		return err
	}
	if lg == nil {
		return fmt.Errorf("log group %s not found", logGroupName)
	}
	resourceArn, err := logGroupTagResourceArn(lg)
	if err != nil {
		return fmt.Errorf("failed to resolve tag ARN for log group %s: %w", logGroupName, err)
	}

	_, err = c.client.TagResource(ctx, &cloudwatchlogs.TagResourceInput{
		ResourceArn: aws.String(resourceArn),
		Tags:        tags,
	})
	if err != nil {
		return fmt.Errorf("failed to tag log group %s: %w", logGroupName, err)
	}

	return nil
}

func (c *CloudWatchLogsClient) ListLogGroupTags(ctx context.Context, logGroupName string) (map[string]string, error) {
	log.Debug("listing log group tags", "name", logGroupName)

	lg, err := c.DescribeLogGroup(ctx, logGroupName)
	if err != nil {
		return nil, err
	}
	if lg == nil {
		return nil, fmt.Errorf("log group %s not found", logGroupName)
	}
	resourceArn, err := logGroupTagResourceArn(lg)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tag ARN for log group %s: %w", logGroupName, err)
	}

	out, err := c.client.ListTagsForResource(ctx, &cloudwatchlogs.ListTagsForResourceInput{
		ResourceArn: aws.String(resourceArn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for log group %s: %w", logGroupName, err)
	}

	return out.Tags, nil
}

func logGroupTagResourceArn(lg *types.LogGroup) (string, error) {
	if lg == nil {
		return "", fmt.Errorf("log group is nil")
	}
	if arn := aws.ToString(lg.LogGroupArn); arn != "" {
		return arn, nil
	}
	if arn := aws.ToString(lg.Arn); arn != "" {
		return strings.TrimSuffix(arn, ":*"), nil
	}
	return "", fmt.Errorf("log group ARN is empty")
}

func (c *CloudWatchLogsClient) DescribeSubscriptionFilters(ctx context.Context, logGroupName string) ([]types.SubscriptionFilter, error) {
	log.Debug("describing subscription filters", "logGroup", logGroupName)

	var filters []types.SubscriptionFilter
	var nextToken *string

	for {
		out, err := c.client.DescribeSubscriptionFilters(ctx, &cloudwatchlogs.DescribeSubscriptionFiltersInput{
			LogGroupName: aws.String(logGroupName),
			NextToken:    nextToken,
		})
		if err != nil {
			var notFound *types.ResourceNotFoundException
			if errors.As(err, &notFound) {
				log.Debug("log group not found while describing subscription filters", "logGroup", logGroupName)
				return nil, nil
			}
			return nil, fmt.Errorf("failed to describe subscription filters for %s: %w", logGroupName, err)
		}

		filters = append(filters, out.SubscriptionFilters...)
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		nextToken = out.NextToken
	}

	return filters, nil
}

func (c *CloudWatchLogsClient) PutSubscriptionFilter(ctx context.Context, input *PutSubscriptionFilterInput) error {
	log.Debug("putting subscription filter", "logGroup", input.LogGroupName, "name", input.Name)

	putInput := &cloudwatchlogs.PutSubscriptionFilterInput{
		LogGroupName:   aws.String(input.LogGroupName),
		FilterName:     aws.String(input.Name),
		DestinationArn: aws.String(input.DestinationArn),
		FilterPattern:  aws.String(input.FilterPattern),
	}

	if input.RoleArn != "" {
		putInput.RoleArn = aws.String(input.RoleArn)
	}
	if input.Distribution != "" {
		putInput.Distribution = types.Distribution(input.Distribution)
	}

	_, err := c.client.PutSubscriptionFilter(ctx, putInput)
	if err != nil {
		return fmt.Errorf("failed to put subscription filter %s on %s: %w", input.Name, input.LogGroupName, err)
	}

	log.Info("put subscription filter", "logGroup", input.LogGroupName, "name", input.Name)
	return nil
}

func (c *CloudWatchLogsClient) DeleteSubscriptionFilter(ctx context.Context, logGroupName, filterName string) error {
	log.Debug("deleting subscription filter", "logGroup", logGroupName, "name", filterName)

	_, err := c.client.DeleteSubscriptionFilter(ctx, &cloudwatchlogs.DeleteSubscriptionFilterInput{
		LogGroupName: aws.String(logGroupName),
		FilterName:   aws.String(filterName),
	})
	if err != nil {
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			log.Debug("subscription filter not found", "logGroup", logGroupName, "name", filterName)
			return nil
		}
		return fmt.Errorf("failed to delete subscription filter %s on %s: %w", filterName, logGroupName, err)
	}

	log.Info("deleted subscription filter", "logGroup", logGroupName, "name", filterName)
	return nil
}

// GetLogEvents fetches log events from a CloudWatch log stream.
// If limit <= 0, fetches all events. Otherwise fetches up to limit events.
// Returns log lines as strings, newest first.
func (c *CloudWatchLogsClient) GetLogEvents(ctx context.Context, logGroup, logStream string, limit int) ([]string, error) {
	log.Debug("getting log events", "logGroup", logGroup, "logStream", logStream, "limit", limit)

	var allEvents []string
	var nextToken *string

	for {
		input := &cloudwatchlogs.GetLogEventsInput{
			LogGroupName:  aws.String(logGroup),
			LogStreamName: aws.String(logStream),
			StartFromHead: aws.Bool(false), // newest first
		}
		if nextToken != nil {
			input.NextToken = nextToken
		}
		if limit > 0 {
			remaining := limit - len(allEvents)
			if remaining <= 0 {
				break
			}
			if remaining > 10000 {
				remaining = 10000
			}
			input.Limit = aws.Int32(int32(remaining))
		}

		out, err := c.client.GetLogEvents(ctx, input)
		if err != nil {
			var notFound *types.ResourceNotFoundException
			if errors.As(err, &notFound) {
				log.Debug("log stream not found", "logGroup", logGroup, "logStream", logStream)
				return nil, nil
			}
			return nil, fmt.Errorf("failed to get log events: %w", err)
		}

		for _, event := range out.Events {
			allEvents = append(allEvents, aws.ToString(event.Message))
		}

		// Check if we should continue pagination
		if out.NextForwardToken == nil || *out.NextForwardToken == "" {
			break
		}
		if nextToken != nil && *nextToken == *out.NextForwardToken {
			break // no more events
		}
		if limit > 0 && len(allEvents) >= limit {
			break
		}
		nextToken = out.NextForwardToken
	}

	return allEvents, nil
}
