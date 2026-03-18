package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

type Encryptor struct {
	gcm cipher.AEAD
}

func NewEncryptor(key string) (*Encryptor, error) {
	// MED-04 fix: Increased minimum from 16 to 32 characters. With HKDF (no brute-force
	// protection), a 16-char passphrase could have as little as ~50 bits of entropy.
	// Auto-generated keys are 64 hex chars (256 bits); this prevents weak user-configured keys.
	if len(key) < 32 {
		return nil, fmt.Errorf("encryption key must be at least 32 characters")
	}
	// V4-I1: Derive AES key using HKDF-SHA256 with a fixed salt for
	// cryptographic correctness (prevents theoretical multi-target attacks).
	aesKey := make([]byte, 32)
	// SEC-12: Pass info parameter to HKDF for domain separation (was nil)
	reader := hkdf.New(sha256.New, []byte(key), []byte("forgemill-enc-v1"), []byte("forgemill-aes-256-gcm"))
	if _, err := io.ReadFull(reader, aesKey); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &Encryptor{gcm: gcm}, nil
}

func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (e *Encryptor) Decrypt(encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
