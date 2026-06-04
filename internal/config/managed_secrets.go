package config

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/log"
	"github.com/x-qdo/ecsmate/internal/secrets"
)

type ManagedSecrets struct {
	Decrypted    map[string]string
	SSMPrefix    string
	KMSKeyArn    string
	KMSKeyRegion string
	SSMKMSKeyID  string
	Region       string
	AccountID    string
}

func LoadManagedSecrets(ctx context.Context, manifestPath string, cfg *ManagedSecretsConfig, region string) (*ManagedSecrets, error) {
	if cfg == nil {
		return nil, nil
	}

	secretsPath := filepath.Join(manifestPath, cfg.File)
	kmsRegion := managedSecretsKMSRegion(cfg, region)
	ssmKMSKeyID := managedSecretsSSMKMSKeyID(cfg, region)
	log.Debug("loading managed secrets", "path", secretsPath, "kmsRegion", kmsRegion)

	ef, err := secrets.LoadEncryptedFile(secretsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load secrets file: %w", err)
	}

	kmsClient, err := aws.NewKMSClient(ctx, kmsRegion)
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
		Decrypted:    decrypted,
		SSMPrefix:    cfg.SSMPrefix,
		KMSKeyArn:    cfg.KMSKeyArn,
		KMSKeyRegion: kmsRegion,
		SSMKMSKeyID:  ssmKMSKeyID,
		Region:       region,
		AccountID:    accountID,
	}, nil
}

func managedSecretsKMSRegion(cfg *ManagedSecretsConfig, deploymentRegion string) string {
	if cfg != nil && cfg.KMSKeyRegion != "" {
		return cfg.KMSKeyRegion
	}
	return deploymentRegion
}

func managedSecretsSSMKMSKeyID(cfg *ManagedSecretsConfig, deploymentRegion string) string {
	if cfg == nil {
		return ""
	}
	if cfg.SSMKMSKeyID != "" {
		return cfg.SSMKMSKeyID
	}
	if cfg.KMSKeyArn == "" {
		return ""
	}

	keyRegion, ok := arnRegion(cfg.KMSKeyArn)
	if !ok || deploymentRegion == "" || keyRegion == deploymentRegion {
		return cfg.KMSKeyArn
	}

	log.Warn(
		"managed secrets KMS key is in a different region than SSM parameters; using the default SSM SecureString key",
		"kmsKeyRegion", keyRegion,
		"ssmRegion", deploymentRegion,
	)
	return ""
}

func arnRegion(arn string) (string, bool) {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 || parts[0] != "arn" {
		return "", false
	}
	return parts[3], true
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
