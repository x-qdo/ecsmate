package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"
	"github.com/aws/aws-sdk-go-v2/service/scheduler/types"

	"github.com/qdo/ecsmate/internal/log"
)

type SchedulerClient struct {
	client *scheduler.Client
	region string
}

func NewSchedulerClient(ctx context.Context, region string) (*SchedulerClient, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &SchedulerClient{
		client: scheduler.NewFromConfig(cfg),
		region: region,
	}, nil
}

func (c *SchedulerClient) GetSchedule(ctx context.Context, name, groupName string) (*scheduler.GetScheduleOutput, error) {
	log.Debug("getting schedule", "name", name, "group", groupName)

	input := &scheduler.GetScheduleInput{
		Name:      aws.String(name),
		GroupName: aws.String(groupName),
	}

	out, err := c.client.GetSchedule(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %s: %w", name, err)
	}

	return out, nil
}

func (c *SchedulerClient) ListSchedules(ctx context.Context, groupName, namePrefix string) ([]types.ScheduleSummary, error) {
	log.Debug("listing schedules", "group", groupName, "namePrefix", namePrefix)

	var schedules []types.ScheduleSummary
	paginator := scheduler.NewListSchedulesPaginator(c.client, &scheduler.ListSchedulesInput{
		GroupName:  aws.String(groupName),
		NamePrefix: aws.String(namePrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list schedules: %w", err)
		}
		schedules = append(schedules, page.Schedules...)
	}

	return schedules, nil
}

func (c *SchedulerClient) CreateSchedule(ctx context.Context, input *scheduler.CreateScheduleInput) error {
	name := aws.ToString(input.Name)
	log.Debug("creating schedule", "name", name)

	_, err := c.client.CreateSchedule(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to create schedule %s: %w", name, err)
	}

	log.Info("created schedule", "name", name, "expression", aws.ToString(input.ScheduleExpression))
	return nil
}

func (c *SchedulerClient) UpdateSchedule(ctx context.Context, input *scheduler.UpdateScheduleInput) error {
	name := aws.ToString(input.Name)
	log.Debug("updating schedule", "name", name)

	_, err := c.client.UpdateSchedule(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to update schedule %s: %w", name, err)
	}

	log.Info("updated schedule", "name", name, "expression", aws.ToString(input.ScheduleExpression))
	return nil
}

func (c *SchedulerClient) DeleteSchedule(ctx context.Context, name, groupName string) error {
	log.Debug("deleting schedule", "name", name, "group", groupName)

	_, err := c.client.DeleteSchedule(ctx, &scheduler.DeleteScheduleInput{
		Name:      aws.String(name),
		GroupName: aws.String(groupName),
	})
	if err != nil {
		return fmt.Errorf("failed to delete schedule %s: %w", name, err)
	}

	log.Info("deleted schedule", "name", name)
	return nil
}

func (c *SchedulerClient) ListTagsForResource(ctx context.Context, arn string) (map[string]string, error) {
	log.Debug("listing schedule tags", "arn", arn)

	out, err := c.client.ListTagsForResource(ctx, &scheduler.ListTagsForResourceInput{
		ResourceArn: aws.String(arn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for %s: %w", arn, err)
	}

	tags := make(map[string]string)
	for _, tag := range out.Tags {
		key := aws.ToString(tag.Key)
		tags[key] = aws.ToString(tag.Value)
	}
	return tags, nil
}

func (c *SchedulerClient) TagResource(ctx context.Context, arn string, tags []types.Tag) error {
	if len(tags) == 0 {
		return nil
	}

	log.Debug("tagging schedule", "arn", arn, "count", len(tags))
	_, err := c.client.TagResource(ctx, &scheduler.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        tags,
	})
	if err != nil {
		return fmt.Errorf("failed to tag %s: %w", arn, err)
	}
	return nil
}

func (c *SchedulerClient) UntagResource(ctx context.Context, arn string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	log.Debug("untagging schedule", "arn", arn, "count", len(keys))
	_, err := c.client.UntagResource(ctx, &scheduler.UntagResourceInput{
		ResourceArn: aws.String(arn),
		TagKeys:     keys,
	})
	if err != nil {
		return fmt.Errorf("failed to untag %s: %w", arn, err)
	}
	return nil
}

func (c *SchedulerClient) CreateScheduleGroup(ctx context.Context, name string) error {
	log.Debug("creating schedule group", "name", name)

	_, err := c.client.CreateScheduleGroup(ctx, &scheduler.CreateScheduleGroupInput{
		Name: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("failed to create schedule group %s: %w", name, err)
	}

	log.Info("created schedule group", "name", name)
	return nil
}
