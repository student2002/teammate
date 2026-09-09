// aes.go provides AES-256-GCM symmetric encryption for secure storage of Git credentials (PATs).
// Encryption flow: plaintext → AES-256-GCM encryption (random nonce) → Base64-encoded storage
// Decryption flow: Base64 decode → split nonce and ciphertext → AES-256-GCM decryption → plaintext
//
// Key management:
//   - Production: reads a 32-byte Base64-encoded key from the TEAMMATE_ENCRYPTION_KEY_BASE64 environment variable
//   - Development: uses a temporary development key when no key is set (only when TEAMMATE_DEV=true)
//   - Security requirement: the key must be exactly 32 bytes (required by AES-256)
//
// Security features:
//   - AES-256-GCM provides authenticated encryption (AEAD), guaranteeing both confidentiality and integrity
//   - Each encryption uses a random nonce, so identical plaintexts produce different ciphertexts
//   - The ciphertext includes a 16-byte authentication tag to prevent tampering
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
)

// encryptionKey is the AES-256 key used to encrypt PATs; it must be initialized via InitEncryptionKey() before use.
var encryptionKey []byte

// InitEncryptionKey initializes the AES-256 encryption key from the environment variable.
//
// Initialization priority:
//  1. TEAMMATE_ENCRYPTION_KEY_BASE64 environment variable (required in production)
//  2. A temporary key in development mode (only when TEAMMATE_DEV=true)
//
// Security requirement: the key must be exactly 32 bytes (after Base64 decoding)
//
// Returns:
//   - error: returned when the key format is invalid or not configured
func InitEncryptionKey() error {
	keyB64 := os.Getenv("TEAMMATE_ENCRYPTION_KEY_BASE64")
	if keyB64 != "" {
		key, err := base64.StdEncoding.DecodeString(keyB64)
		if err != nil {
			return fmt.Errorf("failed to decode TEAMMATE_ENCRYPTION_KEY_BASE64: %w", err)
		}
		if len(key) != 32 {
			return fmt.Errorf("encryption key must be 32 bytes after base64 decode, got %d", len(key))
		}
		encryptionKey = make([]byte, 32)
		copy(encryptionKey, key)
		return nil
	}

	// Development mode: allow using a temporary key
	if os.Getenv("TEAMMATE_DEV") == "true" {
		slog.Warn("using temporary development encryption key — do not use in production")
		encryptionKey = []byte("teammate-dev-aes-256-key-temp!!!") // 32 bytes
		return nil
	}

	return fmt.Errorf("TEAMMATE_ENCRYPTION_KEY_BASE64 is required in production; set TEAMMATE_DEV=true for development")
}

// SetEncryptionKey sets the AES-256 key used for PAT encryption; the key must be exactly 32 bytes.
// Primarily used in test scenarios; production should load the key from the environment via InitEncryptionKey().
//
// Parameters:
//   - key: a 32-byte AES-256 key
//
// Returns:
//   - error: returned when the key length is not 32 bytes
func SetEncryptionKey(key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	encryptionKey = make([]byte, 32)
	copy(encryptionKey, key)
	return nil
}

// EncryptPAT encrypts a PAT (Personal Access Token) using AES-256-GCM and returns a Base64-encoded string.
//
// Encryption flow:
//  1. Create an AES-256 block cipher
//  2. Initialize GCM authenticated-encryption mode
//  3. Generate a random nonce (12 bytes)
//  4. Encrypt the plaintext with AES-GCM, producing ciphertext + a 16-byte authentication tag
//  5. Return the Base64-encoded combination of "nonce + ciphertext + tag"
//
// Parameters:
//   - plaintext: the plaintext PAT string
//
// Returns:
//   - string: the Base64-encoded ciphertext
//   - error: returned when encryption fails
func EncryptPAT(plaintext string) (string, error) {
	if len(encryptionKey) == 0 {
		return "", fmt.Errorf("encryption key not initialized; call InitEncryptionKey() first")
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptPAT decrypts a Base64-encoded PAT encrypted with AES-256-GCM.
//
// Decryption flow:
//  1. Base64-decode to obtain the raw bytes
//  2. Split the nonce (first 12 bytes) from the ciphertext+tag (remaining bytes)
//  3. Decrypt with AES-GCM and verify the authentication tag
//  4. Return the plaintext PAT string
//
// Security note: if the ciphertext is tampered with or the key does not match, AES-GCM authentication-tag verification fails.
//
// Parameters:
//   - encoded: the Base64-encoded ciphertext string
//
// Returns:
//   - string: the decrypted plaintext PAT
//   - error: returned when decryption fails
func DecryptPAT(encoded string) (string, error) {
	if len(encryptionKey) == 0 {
		return "", fmt.Errorf("encryption key not initialized; call InitEncryptionKey() first")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}
