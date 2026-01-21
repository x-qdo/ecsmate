package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"

	"github.com/x-qdo/ecsmate/internal/log"
)

type KMSClient struct {
	client *kms.Client
}

func NewKMSClient(ctx context.Context, region string) (*KMSClient, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &KMSClient{
		client: kms.NewFromConfig(cfg),
	}, nil
}

func (c *KMSClient) GenerateDataKey(ctx context.Context, keyArn string) (plaintext, ciphertext []byte, err error) {
	log.Debug("generating data key", "keyArn", keyArn)

	out, err := c.client.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:   aws.String(keyArn),
		KeySpec: types.DataKeySpecAes256,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate data key: %w", err)
	}

	return out.Plaintext, out.CiphertextBlob, nil
}

func (c *KMSClient) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	log.Debug("decrypting data key")

	out, err := c.client.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob: ciphertext,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data key: %w", err)
	}

	return out.Plaintext, nil
}
