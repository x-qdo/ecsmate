package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"
	"github.com/aws/aws-sdk-go-v2/service/scheduler/types"

	"github.com/x-qdo/ecsmate/internal/log"
)

type SchedulerClient struct {
	client *scheduler.Client
	region string
}

func (c *SchedulerClient) Region() string {
	return c.region
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
			var notFound *types.ResourceNotFoundException
			if errors.As(err, &notFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to list schedules: %w", err)
		}
		schedules = append(schedules, page.Schedules...)
	}

	return schedules, nil
}

func (c *SchedulerClient) CreateSchedule(ctx context.Context, input *scheduler.CreateScheduleInput) (string, error) {
	name := aws.ToString(input.Name)
	log.Debug("creating schedule", "name", name)

	out, err := c.client.CreateSchedule(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to create schedule %s: %w", name, err)
	}

	log.Info("created schedule", "name", name, "expression", aws.ToString(input.ScheduleExpression))
	return aws.ToString(out.ScheduleArn), nil
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
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("failed to delete schedule %s: %w", name, err)
	}

	log.Info("deleted schedule", "name", name)
	return nil
}

func (c *SchedulerClient) EnsureScheduleGroup(ctx context.Context, name string) error {
	if name == "" || name == "default" {
		return nil
	}

	log.Debug("ensuring schedule group exists", "name", name)

	_, err := c.client.CreateScheduleGroup(ctx, &scheduler.CreateScheduleGroupInput{
		Name: aws.String(name),
	})
	if err != nil {
		var conflict *types.ConflictException
		if errors.As(err, &conflict) {
			log.Debug("schedule group already exists", "name", name)
			return nil
		}
		return fmt.Errorf("failed to create schedule group %s: %w", name, err)
	}

	log.Info("created schedule group", "name", name)
	return nil
}
