package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type STSClient struct {
	client *sts.Client
}

func NewSTSClient(ctx context.Context, region string) (*STSClient, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &STSClient{
		client: sts.NewFromConfig(cfg),
	}, nil
}

func (c *STSClient) GetAccountID(ctx context.Context) (string, error) {
	out, err := c.client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get caller identity: %w", err)
	}

	return *out.Account, nil
}
