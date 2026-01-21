package secrets

import (
	"crypto/rand"
	"testing"
)

func TestEncryptedFile_NewAndAccess(t *testing.T) {
	kmsArn := "arn:aws:kms:us-east-1:123456789:key/test-key"
	encryptedDataKey := make([]byte, 32)
	rand.Read(encryptedDataKey)

	ef := NewEncryptedFile(kmsArn, encryptedDataKey)

	if ef.Sops.KMS.Arn != kmsArn {
		t.Errorf("expected KMS ARN %s, got %s", kmsArn, ef.Sops.KMS.Arn)
	}

	if ef.Sops.Version != "1" {
		t.Errorf("expected version 1, got %s", ef.Sops.Version)
	}

	if ef.Data == nil {
		t.Error("expected Data map to be initialized")
	}

	retrievedKey, err := ef.GetEncryptedDataKey()
	if err != nil {
		t.Fatalf("GetEncryptedDataKey failed: %v", err)
	}

	if len(retrievedKey) != len(encryptedDataKey) {
		t.Errorf("expected key length %d, got %d", len(encryptedDataKey), len(retrievedKey))
	}
}

func TestEncryptedFile_ComputeMAC(t *testing.T) {
	ef := &EncryptedFile{
		Data: map[string]string{
			"key1": "ENC[AES256_GCM,data:abc,iv:def,tag:ghi]",
			"key2": "ENC[AES256_GCM,data:xyz,iv:123,tag:456]",
		},
	}

	dataKey := make([]byte, 32)
	rand.Read(dataKey)

	mac1 := ef.ComputeMAC(dataKey)
	mac2 := ef.ComputeMAC(dataKey)

	if mac1 != mac2 {
		t.Error("same data and key should produce same MAC")
	}

	if mac1 == "" {
		t.Error("MAC should not be empty")
	}
}

func TestEncryptedFile_VerifyMAC_Success(t *testing.T) {
	ef := &EncryptedFile{
		Data: map[string]string{
			"secret": "ENC[AES256_GCM,data:test,iv:123,tag:456]",
		},
	}

	dataKey := make([]byte, 32)
	rand.Read(dataKey)

	ef.Sops.MAC = ef.ComputeMAC(dataKey)

	err := ef.VerifyMAC(dataKey)
	if err != nil {
		t.Errorf("expected MAC verification to succeed: %v", err)
	}
}

func TestEncryptedFile_VerifyMAC_Failure(t *testing.T) {
	ef := &EncryptedFile{
		Data: map[string]string{
			"secret": "ENC[AES256_GCM,data:test,iv:123,tag:456]",
		},
		Sops: SOPS{
			MAC: "invalid_mac",
		},
	}

	dataKey := make([]byte, 32)
	rand.Read(dataKey)

	err := ef.VerifyMAC(dataKey)
	if err == nil {
		t.Error("expected MAC verification to fail with invalid MAC")
	}
}

func TestEncryptedFile_VerifyMAC_TamperedData(t *testing.T) {
	ef := &EncryptedFile{
		Data: map[string]string{
			"secret": "ENC[AES256_GCM,data:original,iv:123,tag:456]",
		},
	}

	dataKey := make([]byte, 32)
	rand.Read(dataKey)

	ef.Sops.MAC = ef.ComputeMAC(dataKey)

	ef.Data["secret"] = "ENC[AES256_GCM,data:tampered,iv:123,tag:456]"

	err := ef.VerifyMAC(dataKey)
	if err == nil {
		t.Error("expected MAC verification to fail after data tampering")
	}
}

func TestEncryptedFile_MACDeterministic(t *testing.T) {
	dataKey := make([]byte, 32)
	rand.Read(dataKey)

	ef1 := &EncryptedFile{
		Data: map[string]string{
			"b_key": "value_b",
			"a_key": "value_a",
		},
	}

	ef2 := &EncryptedFile{
		Data: map[string]string{
			"a_key": "value_a",
			"b_key": "value_b",
		},
	}

	mac1 := ef1.ComputeMAC(dataKey)
	mac2 := ef2.ComputeMAC(dataKey)

	if mac1 != mac2 {
		t.Error("MAC should be deterministic regardless of map insertion order")
	}
}

func TestEncryptedFile_GetEncryptedDataKey_Invalid(t *testing.T) {
	ef := &EncryptedFile{
		Sops: SOPS{
			KMS: KMSInfo{
				Enc: "not-valid-base64!!!",
			},
		},
	}

	_, err := ef.GetEncryptedDataKey()
	if err == nil {
		t.Error("expected error for invalid base64 encoded key")
	}
}

func TestEnvelope_DeleteValue_NotFound(t *testing.T) {
	ef := &EncryptedFile{
		Data: map[string]string{
			"existing": "value",
		},
	}

	env := &Envelope{}

	err := env.DeleteValue(ef, "nonexistent")
	if err == nil {
		t.Error("expected error when deleting non-existent key")
	}
}

func TestEnvelope_DeleteValue_Success(t *testing.T) {
	ef := &EncryptedFile{
		Data: map[string]string{
			"key_to_delete": "value",
			"other_key":     "other_value",
		},
	}

	env := &Envelope{}

	err := env.DeleteValue(ef, "key_to_delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, exists := ef.Data["key_to_delete"]; exists {
		t.Error("key should have been deleted")
	}

	if _, exists := ef.Data["other_key"]; !exists {
		t.Error("other key should still exist")
	}
}
