package config

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/log"
	"github.com/qdo/ecsmate/internal/secrets"
)

type ManagedSecrets struct {
	Decrypted map[string]string
	SSMPrefix string
	KMSKeyArn string
	Region    string
	AccountID string
}

func LoadManagedSecrets(ctx context.Context, manifestPath string, cfg *ManagedSecretsConfig, region string) (*ManagedSecrets, error) {
	if cfg == nil {
		return nil, nil
	}

	secretsPath := filepath.Join(manifestPath, cfg.File)
	log.Debug("loading managed secrets", "path", secretsPath)

	ef, err := secrets.LoadEncryptedFile(secretsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load secrets file: %w", err)
	}

	kmsClient, err := aws.NewKMSClient(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("failed to create KMS client: %w", err)
	}

	envelope := secrets.NewEnvelope(kmsClient)
	decrypted, err := envelope.DecryptFile(ctx, ef)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secrets: %w", err)
	}

	stsClient, err := aws.NewSTSClient(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("failed to create STS client: %w", err)
	}

	accountID, err := stsClient.GetAccountID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get account ID: %w", err)
	}

	if region == "" {
		region = "us-east-1"
	}

	log.Info("loaded managed secrets", "count", len(decrypted))

	return &ManagedSecrets{
		Decrypted: decrypted,
		SSMPrefix: cfg.SSMPrefix,
		KMSKeyArn: cfg.KMSKeyArn,
		Region:    region,
		AccountID: accountID,
	}, nil
}

func (m *ManagedSecrets) BuildARNMap() map[string]string {
	if m == nil || len(m.Decrypted) == 0 {
		return nil
	}

	arns := make(map[string]string, len(m.Decrypted))
	for key := range m.Decrypted {
		arns[key] = fmt.Sprintf("arn:aws:ssm:%s:%s:parameter%s/%s", m.Region, m.AccountID, m.SSMPrefix, key)
	}
	return arns
}
