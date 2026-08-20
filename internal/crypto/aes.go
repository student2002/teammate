// aes.go 提供 AES-256-GCM 对称加密功能，用于 Git 凭据（PAT）的安全存储。
// 加密流程：明文 → AES-256-GCM 加密（随机 nonce）→ Base64 编码存储
// 解密流程：Base64 解码 → 分离 nonce 和密文 → AES-256-GCM 解密 → 明文
//
// 密钥管理：
//   - 生产环境：从 TEAMMATE_ENCRYPTION_KEY_BASE64 环境变量读取 32 字节 Base64 编码密钥
//   - 开发环境：未设置密钥时使用临时开发密钥（仅限 TEAMMATE_DEV=true）
//   - 安全要求：密钥必须恰好 32 字节（AES-256 要求）
//
// 安全特性：
//   - AES-256-GCM 提供认证加密（AEAD），同时保证机密性和完整性
//   - 每次加密使用随机 nonce，即使相同明文也会产生不同密文
//   - 密文包含 16 字节认证标签，防止篡改
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

// encryptionKey 是用于加密 PAT 的 AES-256 密钥，必须在使用前通过 InitEncryptionKey() 初始化。
var encryptionKey []byte

// InitEncryptionKey 从环境变量初始化 AES-256 加密密钥。
//
// 初始化优先级：
//  1. TEAMMATE_ENCRYPTION_KEY_BASE64 环境变量（生产环境必须设置）
//  2. 开发模式下使用临时密钥（仅限 TEAMMATE_DEV=true）
//
// 安全要求：密钥必须恰好 32 字节（Base64 解码后）
//
// 返回：
//   - error: 密钥格式错误或未配置时返回错误
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

	// 开发模式：允许使用临时密钥
	if os.Getenv("TEAMMATE_DEV") == "true" {
		slog.Warn("using temporary development encryption key — do not use in production")
		encryptionKey = []byte("teammate-dev-aes-256-key-temp!!!") // 32 字节
		return nil
	}

	return fmt.Errorf("TEAMMATE_ENCRYPTION_KEY_BASE64 is required in production; set TEAMMATE_DEV=true for development")
}

// SetEncryptionKey 设置用于 PAT 加密的 AES-256 密钥，密钥必须恰好 32 字节。
// 主要用于测试场景，生产环境应通过 InitEncryptionKey() 从环境变量加载。
//
// 参数：
//   - key: 32 字节 AES-256 密钥
//
// 返回：
//   - error: 密钥长度不为 32 字节时返回错误
func SetEncryptionKey(key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	encryptionKey = make([]byte, 32)
	copy(encryptionKey, key)
	return nil
}

// EncryptPAT 使用 AES-256-GCM 加密 PAT（Personal Access Token）并返回 Base64 编码字符串。
//
// 加密流程：
//  1. 创建 AES-256 分组密码器
//  2. 初始化 GCM 认证加密模式
//  3. 生成随机 nonce（12 字节）
//  4. 使用 AES-GCM 加密明文，生成密文 + 16 字节认证标签
//  5. 返回 Base64 编码的 "nonce + ciphertext + tag" 组合
//
// 参数：
//   - plaintext: 明文 PAT 字符串
//
// 返回：
//   - string: Base64 编码的密文
//   - error: 加密失败时返回错误
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

// DecryptPAT 解密 Base64 编码的 AES-256-GCM 加密的 PAT。
//
// 解密流程：
//  1. Base64 解码获取原始字节
//  2. 分离 nonce（前 12 字节）和 ciphertext+tag（剩余字节）
//  3. 使用 AES-GCM 解密并验证认证标签
//  4. 返回明文 PAT 字符串
//
// 安全说明：如果密文被篡改或密钥不匹配，AES-GCM 的认证标签验证会失败。
//
// 参数：
//   - encoded: Base64 编码的密文字符串
//
// 返回：
//   - string: 解密后的明文 PAT
//   - error: 解密失败时返回错误
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
