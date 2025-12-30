package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/qdo/ecsmate/internal/log"
)

type IAMClient struct {
	client *iam.Client
}

func NewIAMClient(ctx context.Context, region string) (*IAMClient, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &IAMClient{
		client: iam.NewFromConfig(cfg),
	}, nil
}

const (
	EventBridgeSchedulerRoleName = "ecsmate-eventbridge-scheduler-role"
	EventBridgeSchedulerPolicyName = "ecsmate-eventbridge-scheduler-policy"
)

type AssumeRolePolicyDocument struct {
	Version   string                   `json:"Version"`
	Statement []AssumeRoleStatement    `json:"Statement"`
}

type AssumeRoleStatement struct {
	Effect    string                 `json:"Effect"`
	Principal map[string]string      `json:"Principal"`
	Action    string                 `json:"Action"`
}

type PolicyDocument struct {
	Version   string            `json:"Version"`
	Statement []PolicyStatement `json:"Statement"`
}

type PolicyStatement struct {
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource string   `json:"Resource"`
}

func (c *IAMClient) GetOrCreateEventBridgeRole(ctx context.Context) (string, error) {
	roleName := EventBridgeSchedulerRoleName

	roleArn, err := c.getRole(ctx, roleName)
	if err == nil && roleArn != "" {
		log.Debug("found existing EventBridge scheduler role", "arn", roleArn)
		return roleArn, nil
	}

	log.Info("creating EventBridge scheduler role", "name", roleName)

	roleArn, err = c.createEventBridgeRole(ctx, roleName)
	if err != nil {
		return "", err
	}

	if err := c.attachECSPolicy(ctx, roleName); err != nil {
		return "", err
	}

	return roleArn, nil
}

func (c *IAMClient) getRole(ctx context.Context, roleName string) (string, error) {
	out, err := c.client.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchEntity") {
			return "", nil
		}
		return "", fmt.Errorf("failed to get role %s: %w", roleName, err)
	}

	return aws.ToString(out.Role.Arn), nil
}

func (c *IAMClient) createEventBridgeRole(ctx context.Context, roleName string) (string, error) {
	assumeRolePolicy := AssumeRolePolicyDocument{
		Version: "2012-10-17",
		Statement: []AssumeRoleStatement{{
			Effect: "Allow",
			Principal: map[string]string{
				"Service": "scheduler.amazonaws.com",
			},
			Action: "sts:AssumeRole",
		}},
	}

	policyJSON, err := json.Marshal(assumeRolePolicy)
	if err != nil {
		return "", fmt.Errorf("failed to marshal assume role policy: %w", err)
	}

	out, err := c.client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(string(policyJSON)),
		Description:              aws.String("Role for EventBridge Scheduler to invoke ECS tasks (managed by ecsmate)"),
		Tags: []types.Tag{
			{Key: aws.String("ManagedBy"), Value: aws.String("ecsmate")},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create role %s: %w", roleName, err)
	}

	log.Info("created IAM role", "name", roleName, "arn", aws.ToString(out.Role.Arn))
	return aws.ToString(out.Role.Arn), nil
}

func (c *IAMClient) attachECSPolicy(ctx context.Context, roleName string) error {
	policy := PolicyDocument{
		Version: "2012-10-17",
		Statement: []PolicyStatement{
			{
				Effect: "Allow",
				Action: []string{
					"ecs:RunTask",
				},
				Resource: "*",
			},
			{
				Effect: "Allow",
				Action: []string{
					"iam:PassRole",
				},
				Resource: "*",
			},
		},
	}

	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("failed to marshal policy: %w", err)
	}

	_, err = c.client.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       aws.String(roleName),
		PolicyName:     aws.String(EventBridgeSchedulerPolicyName),
		PolicyDocument: aws.String(string(policyJSON)),
	})
	if err != nil {
		return fmt.Errorf("failed to attach policy to role %s: %w", roleName, err)
	}

	log.Debug("attached ECS policy to role", "role", roleName, "policy", EventBridgeSchedulerPolicyName)
	return nil
}

