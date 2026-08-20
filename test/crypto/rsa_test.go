// Package crypto_test 包含 crypto 包的测试，涵盖 RSA 加密/解密往返、PEM 密钥解析（PKIX 和 PKCS1 格式）、无效密钥的错误处理以及边界情况（空数据、超大数据）。
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

// newHash 创建用于 RSA-OAEP 加密的 SHA-256 哈希实例。
func newHash() hash.Hash {
	return sha256.New()
}

// TestRSAEncryptionDecryptionRoundtrip 验证完整的 RSA 加密/解密周期：生成密钥对、用公钥加密、用私钥解密。
func TestRSAEncryptionDecryptionRoundtrip(t *testing.T) {
	// 生成 RSA 密钥对
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	// 测试数据
	plaintext := []byte("Hello, RSA encryption roundtrip test!")

	// 使用公钥加密
	ciphertext, err := crypto.EncryptWithPublicKey(publicKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// 验证密文与明文不同
	if string(ciphertext) == string(plaintext) {
		t.Fatal("ciphertext should not equal plaintext")
	}

	// 使用私钥解密
	hash := newHash()
	decrypted, err := rsa.DecryptOAEP(hash, rand.Reader, privateKey, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	// 验证 roundtrip
	if string(decrypted) != string(plaintext) {
		t.Fatalf("decrypted text does not match original: got %q, want %q", decrypted, plaintext)
	}
	t.Log("RSA encryption/decryption roundtrip successful")
}

// TestRSAEncryptionWithParsedPublicKey 验证使用解析后的 PEM 公钥进行加密。
func TestRSAEncryptionWithParsedPublicKey(t *testing.T) {
	// 生成密钥对
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// 将公钥编码为 PEM（PKIX 格式）
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	// 解析 PEM 公钥
	parsedPubKey, err := crypto.ParsePublicKey(pubKeyPEM)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}

	// 使用解析出的公钥加密
	plaintext := []byte("Test with parsed PEM public key")
	ciphertext, err := crypto.EncryptWithPublicKey(parsedPubKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt with parsed key: %v", err)
	}

	// 使用原始私钥解密
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

// TestRSAPKCS1PublicKey 验证解析 PKCS1 格式公钥。
func TestRSAPKCS1PublicKey(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// 以 PKCS1 格式编码公钥
	pubKeyBytes := x509.MarshalPKCS1PublicKey(&privateKey.PublicKey)
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	// 解析 PEM 公钥
	parsedPubKey, err := crypto.ParsePublicKey(pubKeyPEM)
	if err != nil {
		t.Fatalf("parse PKCS1 public key: %v", err)
	}

	// 验证它可用于加密
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

// TestInvalidPublicKey 验证无效的 PEM 数据返回错误。
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

// TestEncryptWithPublicKeyEmptyData 验证空数据的加密。
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

// TestEncryptWithPublicKeyTooLarge 验证超出密钥大小的数据返回错误。
func TestEncryptWithPublicKeyTooLarge(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// 使用 SHA-256 和 2048 位密钥的 RSA-OAEP 最多可加密 190 字节
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
