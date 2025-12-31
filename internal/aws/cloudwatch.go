package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	"github.com/qdo/ecsmate/internal/log"
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

	_, err = c.client.TagResource(ctx, &cloudwatchlogs.TagResourceInput{
		ResourceArn: lg.Arn,
		Tags:        tags,
	})
	if err != nil {
		return fmt.Errorf("failed to tag log group %s: %w", logGroupName, err)
	}

	return nil
}
