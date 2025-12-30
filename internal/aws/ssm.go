package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/qdo/ecsmate/internal/log"
)

type SSMClient struct {
	client *ssm.Client
}

func NewSSMClient(ctx context.Context, region string) (*SSMClient, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &SSMClient{
		client: ssm.NewFromConfig(cfg),
	}, nil
}

func (c *SSMClient) GetParameter(ctx context.Context, name string) (string, error) {
	log.Debug("getting SSM parameter", "name", name)

	out, err := c.client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get SSM parameter %s: %w", name, err)
	}

	return aws.ToString(out.Parameter.Value), nil
}

func (c *SSMClient) GetParameters(ctx context.Context, names []string) (map[string]string, error) {
	if len(names) == 0 {
		return map[string]string{}, nil
	}

	log.Debug("getting SSM parameters", "count", len(names))

	result := make(map[string]string)

	// SSM GetParameters has a limit of 10 parameters per call
	const batchSize = 10
	for i := 0; i < len(names); i += batchSize {
		end := i + batchSize
		if end > len(names) {
			end = len(names)
		}
		batch := names[i:end]

		out, err := c.client.GetParameters(ctx, &ssm.GetParametersInput{
			Names:          batch,
			WithDecryption: aws.Bool(true),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get SSM parameters: %w", err)
		}

		for _, p := range out.Parameters {
			result[aws.ToString(p.Name)] = aws.ToString(p.Value)
		}

		if len(out.InvalidParameters) > 0 {
			log.Warn("some SSM parameters not found", "invalid", out.InvalidParameters)
		}
	}

	return result, nil
}

// ResolveSSMReferences finds and resolves SSM references in the format {{ssm:/path/to/param}}
func (c *SSMClient) ResolveSSMReferences(ctx context.Context, input string) (string, error) {
	const prefix = "{{ssm:"
	const suffix = "}}"

	result := input
	for {
		start := strings.Index(result, prefix)
		if start == -1 {
			break
		}

		end := strings.Index(result[start:], suffix)
		if end == -1 {
			return "", fmt.Errorf("unclosed SSM reference starting at position %d", start)
		}
		end += start

		paramName := strings.TrimSpace(result[start+len(prefix) : end])
		value, err := c.GetParameter(ctx, paramName)
		if err != nil {
			return "", err
		}

		result = result[:start] + value + result[end+len(suffix):]
	}

	return result, nil
}
