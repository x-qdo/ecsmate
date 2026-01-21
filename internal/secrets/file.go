package secrets

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

type EncryptedFile struct {
	Sops SOPS              `yaml:"sops"`
	Data map[string]string `yaml:"data"`
}

type SOPS struct {
	KMS          KMSInfo `yaml:"kms"`
	MAC          string  `yaml:"mac"`
	LastModified string  `yaml:"lastModified"`
	Version      string  `yaml:"version"`
}

type KMSInfo struct {
	Arn string `yaml:"arn"`
	Enc string `yaml:"enc"`
}

func LoadEncryptedFile(path string) (*EncryptedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var ef EncryptedFile
	if err := yaml.Unmarshal(data, &ef); err != nil {
		return nil, fmt.Errorf("failed to parse encrypted file: %w", err)
	}

	if ef.Sops.Version == "" {
		return nil, fmt.Errorf("not a valid encrypted secrets file (missing sops metadata)")
	}

	return &ef, nil
}

func (ef *EncryptedFile) Save(path string) error {
	ef.Sops.LastModified = time.Now().UTC().Format(time.RFC3339)

	data, err := yaml.Marshal(ef)
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (ef *EncryptedFile) ComputeMAC(dataKey []byte) string {
	h := hmac.New(sha256.New, dataKey)

	keys := make([]string, 0, len(ef.Data))
	for k := range ef.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(ef.Data[k]))
	}

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (ef *EncryptedFile) VerifyMAC(dataKey []byte) error {
	expected := ef.ComputeMAC(dataKey)
	if ef.Sops.MAC != expected {
		return fmt.Errorf("MAC verification failed: file may have been tampered with")
	}
	return nil
}

func NewEncryptedFile(kmsArn string, encryptedDataKey []byte) *EncryptedFile {
	return &EncryptedFile{
		Sops: SOPS{
			KMS: KMSInfo{
				Arn: kmsArn,
				Enc: base64.StdEncoding.EncodeToString(encryptedDataKey),
			},
			Version: "1",
		},
		Data: make(map[string]string),
	}
}

func (ef *EncryptedFile) GetEncryptedDataKey() ([]byte, error) {
	return base64.StdEncoding.DecodeString(ef.Sops.KMS.Enc)
}
