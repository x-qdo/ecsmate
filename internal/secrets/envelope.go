package secrets

import (
	"context"
	"fmt"

	"github.com/x-qdo/ecsmate/internal/aws"
)

type Envelope struct {
	kms *aws.KMSClient
}

func NewEnvelope(kms *aws.KMSClient) *Envelope {
	return &Envelope{kms: kms}
}

func (e *Envelope) EncryptFile(ctx context.Context, kmsArn string, plainData map[string]string) (*EncryptedFile, error) {
	plainKey, encryptedKey, err := e.kms.GenerateDataKey(ctx, kmsArn)
	if err != nil {
		return nil, fmt.Errorf("failed to generate data key: %w", err)
	}

	ef := NewEncryptedFile(kmsArn, encryptedKey)

	for k, v := range plainData {
		encrypted, err := Encrypt([]byte(v), plainKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt value for key %s: %w", k, err)
		}
		ef.Data[k] = encrypted
	}

	ef.Sops.MAC = ef.ComputeMAC(plainKey)

	return ef, nil
}

func (e *Envelope) DecryptFile(ctx context.Context, ef *EncryptedFile) (map[string]string, error) {
	encryptedKey, err := ef.GetEncryptedDataKey()
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted data key: %w", err)
	}

	plainKey, err := e.kms.Decrypt(ctx, encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data key: %w", err)
	}

	if err := ef.VerifyMAC(plainKey); err != nil {
		return nil, err
	}

	result := make(map[string]string, len(ef.Data))
	for k, v := range ef.Data {
		plaintext, err := Decrypt(v, plainKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt value for key %s: %w", k, err)
		}
		result[k] = string(plaintext)
	}

	return result, nil
}

func (e *Envelope) EncryptValue(ctx context.Context, ef *EncryptedFile, key, value string) error {
	encryptedKey, err := ef.GetEncryptedDataKey()
	if err != nil {
		return fmt.Errorf("failed to decode encrypted data key: %w", err)
	}

	plainKey, err := e.kms.Decrypt(ctx, encryptedKey)
	if err != nil {
		return fmt.Errorf("failed to decrypt data key: %w", err)
	}

	encrypted, err := Encrypt([]byte(value), plainKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt value: %w", err)
	}

	ef.Data[key] = encrypted
	ef.Sops.MAC = ef.ComputeMAC(plainKey)

	return nil
}

func (e *Envelope) DeleteValue(ef *EncryptedFile, key string) error {
	if _, exists := ef.Data[key]; !exists {
		return fmt.Errorf("key %s not found", key)
	}

	delete(ef.Data, key)

	return nil
}

func (e *Envelope) UpdateMAC(ctx context.Context, ef *EncryptedFile) error {
	encryptedKey, err := ef.GetEncryptedDataKey()
	if err != nil {
		return fmt.Errorf("failed to decode encrypted data key: %w", err)
	}

	plainKey, err := e.kms.Decrypt(ctx, encryptedKey)
	if err != nil {
		return fmt.Errorf("failed to decrypt data key: %w", err)
	}

	ef.Sops.MAC = ef.ComputeMAC(plainKey)
	return nil
}
