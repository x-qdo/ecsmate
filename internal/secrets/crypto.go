package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
)

var encPattern = regexp.MustCompile(`^ENC\[AES256_GCM,data:([^,]*),iv:([^,]+),tag:([^]]+)\]$`)

func Encrypt(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	dataLen := len(ciphertext) - gcm.Overhead()
	data := ciphertext[:dataLen]
	tag := ciphertext[dataLen:]

	return fmt.Sprintf("ENC[AES256_GCM,data:%s,iv:%s,tag:%s]",
		base64.StdEncoding.EncodeToString(data),
		base64.StdEncoding.EncodeToString(nonce),
		base64.StdEncoding.EncodeToString(tag),
	), nil
}

func Decrypt(encrypted string, key []byte) ([]byte, error) {
	matches := encPattern.FindStringSubmatch(encrypted)
	if matches == nil {
		return nil, fmt.Errorf("invalid encrypted format")
	}

	data, err := base64.StdEncoding.DecodeString(matches[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode data: %w", err)
	}

	iv, err := base64.StdEncoding.DecodeString(matches[2])
	if err != nil {
		return nil, fmt.Errorf("failed to decode iv: %w", err)
	}

	tag, err := base64.StdEncoding.DecodeString(matches[3])
	if err != nil {
		return nil, fmt.Errorf("failed to decode tag: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	ciphertext := append(data, tag...)
	plaintext, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

func IsEncrypted(s string) bool {
	return encPattern.MatchString(s)
}
