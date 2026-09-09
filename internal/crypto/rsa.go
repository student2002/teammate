// rsa.go provides RSA-OAEP asymmetric encryption, used to encrypt the AES symmetric key for secure key distribution.
// Asymmetric encryption flow: data → SHA-256 hash → RSA-OAEP encryption → ciphertext
// Public-key parsing supports both PKIX and PKCS1 PEM formats, trying PKIX first.
//
// Use cases:
//   - Encrypt the AES key with an RSA public key; only the server holding the private key can decrypt it
//   - Suitable for securely sharing encryption keys across multiple instances in a deployment
package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// EncryptWithPublicKey encrypts data using the RSA-OAEP algorithm with the given public key.
//
// Encryption flow:
//  1. Create a SHA-256 hash function (used for OAEP padding)
//  2. Encrypt the data with the RSA-OAEP algorithm
//  3. Return the ciphertext
//
// Algorithm notes:
//   - RSA-OAEP is more secure than RSA-PKCS1v15 and is semantically secure
//   - SHA-256 is used for the OAEP-padding hash computation
//   - The plaintext length cannot exceed the RSA key length minus the OAEP padding overhead (typically key length - 42 bytes)
//
// Parameters:
//   - pubKey: the RSA public key used for encryption
//   - data: the plaintext data to encrypt
//
// Returns:
//   - []byte: the encrypted ciphertext
//   - error: returned when encryption fails
func EncryptWithPublicKey(pubKey *rsa.PublicKey, data []byte) ([]byte, error) {
	hash := sha256.New()
	ciphertext, err := rsa.EncryptOAEP(hash, rand.Reader, pubKey, data, nil)
	if err != nil {
		return nil, fmt.Errorf("rsa encrypt: %w", err)
	}
	return ciphertext, nil
}

// ParsePublicKey parses a PEM-encoded RSA public key.
// It supports both PKIX and PKCS1 formats, trying PKIX first (the most common public-key format).
//
// Supported formats:
//   - PKIX (SubjectPublicKeyInfo): the standard public-key format, starting with "-----BEGIN PUBLIC KEY-----"
//   - PKCS1: the legacy RSA public-key format, starting with "-----BEGIN RSA PUBLIC KEY-----"
//
// Parameters:
//   - pemData: the PEM-encoded public-key data
//
// Returns:
//   - *rsa.PublicKey: the parsed RSA public key
//   - error: returned when parsing fails
func ParsePublicKey(pemData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	// Try PKIX format first (the most common public-key format)
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err == nil {
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("public key is not RSA")
		}
		return rsaPub, nil
	}

	// Fall back to PKCS1 format
	rsaPub, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: not PKIX or PKCS1 format")
	}
	return rsaPub, nil
}
