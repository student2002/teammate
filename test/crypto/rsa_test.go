// Package crypto_test contains tests for the crypto package, covering RSA encryption/decryption roundtrips, PEM key parsing (PKIX and PKCS1 formats), error handling for invalid keys, and edge cases (empty data, oversized data).
package crypto_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"hash"
	"testing"

	"github.com/teammate/server/internal/crypto"
)

// newHash creates a SHA-256 hash instance for RSA-OAEP encryption.
func newHash() hash.Hash {
	return sha256.New()
}

// TestRSAEncryptionDecryptionRoundtrip verifies the full RSA encryption/decryption cycle: generate key pair, encrypt with public key, decrypt with private key.
func TestRSAEncryptionDecryptionRoundtrip(t *testing.T) {
	// Generate RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	// Test data
	plaintext := []byte("Hello, RSA encryption roundtrip test!")

	// Encrypt with public key
	ciphertext, err := crypto.EncryptWithPublicKey(publicKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Verify ciphertext differs from plaintext
	if string(ciphertext) == string(plaintext) {
		t.Fatal("ciphertext should not equal plaintext")
	}

	// Decrypt with private key
	hash := newHash()
	decrypted, err := rsa.DecryptOAEP(hash, rand.Reader, privateKey, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	// Verify roundtrip
	if string(decrypted) != string(plaintext) {
		t.Fatalf("decrypted text does not match original: got %q, want %q", decrypted, plaintext)
	}
	t.Log("RSA encryption/decryption roundtrip successful")
}

// TestRSAEncryptionWithParsedPublicKey verifies encryption with a parsed PEM public key.
func TestRSAEncryptionWithParsedPublicKey(t *testing.T) {
	// Generate key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Encode the public key as PEM (PKIX format)
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	// Parse the PEM public key
	parsedPubKey, err := crypto.ParsePublicKey(pubKeyPEM)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}

	// Encrypt with the parsed public key
	plaintext := []byte("Test with parsed PEM public key")
	ciphertext, err := crypto.EncryptWithPublicKey(parsedPubKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt with parsed key: %v", err)
	}

	// Decrypt with the original private key
	hash := newHash()
	decrypted, err := rsa.DecryptOAEP(hash, rand.Reader, privateKey, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("roundtrip failed: got %q, want %q", decrypted, plaintext)
	}
	t.Log("RSA encryption with parsed PEM public key successful")
}

// TestRSAPKCS1PublicKey verifies parsing a PKCS1 format public key.
func TestRSAPKCS1PublicKey(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Encode the public key in PKCS1 format
	pubKeyBytes := x509.MarshalPKCS1PublicKey(&privateKey.PublicKey)
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	// Parse the PEM public key
	parsedPubKey, err := crypto.ParsePublicKey(pubKeyPEM)
	if err != nil {
		t.Fatalf("parse PKCS1 public key: %v", err)
	}

	// Verify it can be used for encryption
	plaintext := []byte("PKCS1 format test")
	ciphertext, err := crypto.EncryptWithPublicKey(parsedPubKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	hash := newHash()
	decrypted, err := rsa.DecryptOAEP(hash, rand.Reader, privateKey, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("roundtrip failed: got %q, want %q", decrypted, plaintext)
	}
	t.Log("RSA PKCS1 public key parsing and encryption successful")
}

// TestInvalidPublicKey verifies that invalid PEM data returns an error.
func TestInvalidPublicKey(t *testing.T) {
	testCases := []struct {
		name    string
		pemData []byte
	}{
		{
			name:    "empty_data",
			pemData: []byte(""),
		},
		{
			name:    "not_pem",
			pemData: []byte("this is not PEM data"),
		},
		{
			name:    "invalid_pem_block",
			pemData: []byte("-----BEGIN PUBLIC KEY-----\nnot-valid-base64\n-----END PUBLIC KEY-----"),
		},
		{
			name:    "wrong_key_type",
			pemData: func() []byte {
				privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
				keyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
				return pem.EncodeToMemory(&pem.Block{
					Type:  "RSA PRIVATE KEY",
					Bytes: keyBytes,
				})
			}(),
		},
		{
			name:    "ec_key_not_rsa",
			pemData: []byte("-----BEGIN PUBLIC KEY-----\nMHYwEAYHKoZIzj0CAQYFK4EEACIDYgAESKOWm8GF0c9mt1R7p0HEGqYsJmMzIH6S\nP0BKo7L8Z1kFZ2qX9q3qZ1J7K5X8P3m2V9Y1N4L6W0R2T8V5X7Z9A1B3C5D7E9F\n-----END PUBLIC KEY-----"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := crypto.ParsePublicKey(tc.pemData)
			if err == nil {
				t.Fatalf("expected error for invalid public key %q, got nil", tc.name)
			}
			t.Logf("invalid key %q correctly rejected: %v", tc.name, err)
		})
	}
}

// TestEncryptWithPublicKeyEmptyData verifies encryption of empty data.
func TestEncryptWithPublicKeyEmptyData(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	ciphertext, err := crypto.EncryptWithPublicKey(&privateKey.PublicKey, []byte{})
	if err != nil {
		t.Fatalf("encrypt empty data: %v", err)
	}

	hash := newHash()
	decrypted, err := rsa.DecryptOAEP(hash, rand.Reader, privateKey, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt empty data: %v", err)
	}

	if len(decrypted) != 0 {
		t.Fatalf("expected empty decrypted data, got %q", decrypted)
	}
	t.Log("RSA encryption/decryption of empty data successful")
}

// TestEncryptWithPublicKeyTooLarge verifies that data exceeding the key size returns an error.
func TestEncryptWithPublicKeyTooLarge(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// RSA-OAEP with SHA-256 and a 2048-bit key can encrypt at most 190 bytes
	largeData := make([]byte, 300)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	_, err = crypto.EncryptWithPublicKey(&privateKey.PublicKey, largeData)
	if err == nil {
		t.Fatal("expected error for data too large, got nil")
	}
	t.Logf("too-large data correctly rejected: %v", err)
}
