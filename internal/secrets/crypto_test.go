package secrets

import (
	"crypto/rand"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := "my secret password"

	encrypted, err := Encrypt([]byte(plaintext), key)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if !IsEncrypted(encrypted) {
		t.Error("IsEncrypted returned false for encrypted value")
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != plaintext {
		t.Errorf("decrypted mismatch: got %q, want %q", string(decrypted), plaintext)
	}
}

func TestEncryptDecrypt_EmptyString(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := ""

	encrypted, err := Encrypt([]byte(plaintext), key)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != plaintext {
		t.Errorf("decrypted mismatch: got %q, want %q", string(decrypted), plaintext)
	}
}

func TestEncryptDecrypt_LongString(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := make([]byte, 10000)
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatal(err)
	}

	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("decrypted mismatch for long string")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	if _, err := rand.Read(key1); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(key2); err != nil {
		t.Fatal(err)
	}

	plaintext := "my secret"

	encrypted, err := Encrypt([]byte(plaintext), key1)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	_, err = Decrypt(encrypted, key2)
	if err == nil {
		t.Error("decrypt with wrong key should fail")
	}
}

func TestDecrypt_InvalidFormat(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	testCases := []string{
		"not encrypted",
		"ENC[AES256_GCM]",
		"ENC[AES256_GCM,data:abc]",
		"ENC[AES256_GCM,data:abc,iv:def]",
	}

	for _, tc := range testCases {
		_, err := Decrypt(tc, key)
		if err == nil {
			t.Errorf("expected error for invalid format: %s", tc)
		}
	}
}

func TestIsEncrypted(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
	}{
		{"ENC[AES256_GCM,data:abc,iv:def,tag:ghi]", true},
		{"plaintext", false},
		{"ENC[", false},
		{"", false},
	}

	for _, tc := range testCases {
		result := IsEncrypted(tc.input)
		if result != tc.expected {
			t.Errorf("IsEncrypted(%q) = %v, want %v", tc.input, result, tc.expected)
		}
	}
}
