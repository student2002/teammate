// rsa.go 提供 RSA-OAEP 非对称加密功能，用于加密 AES 对称密钥以实现安全的密钥分发。
// 非对称加密流程：数据 → SHA-256 哈希 → RSA-OAEP 加密 → 密文
// 公钥解析支持 PKIX 和 PKCS1 两种 PEM 格式，优先尝试 PKIX。
//
// 使用场景：
//   - 使用 RSA 公钥加密 AES 密钥，只有持有私钥的服务器可以解密
//   - 适用于多实例部署中安全共享加密密钥
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

// EncryptWithPublicKey 使用 RSA-OAEP 算法和给定的公钥加密数据。
//
// 加密流程：
//  1. 创建 SHA-256 哈希函数（用于 OAEP 填充）
//  2. 使用 RSA-OAEP 算法加密数据
//  3. 返回密文
//
// 算法说明：
//   - RSA-OAEP 比 RSA-PKCS1v15 更安全，具有语义安全性
//   - SHA-256 用于 OAEP 填充的哈希计算
//   - 明文长度不能超过 RSA 密钥长度减去 OAEP 填充开销（通常为密钥长度 - 42 字节）
//
// 参数：
//   - pubKey: RSA 公钥，用于加密
//   - data: 待加密的明文数据
//
// 返回：
//   - []byte: 加密后的密文
//   - error: 加密失败时返回错误
func EncryptWithPublicKey(pubKey *rsa.PublicKey, data []byte) ([]byte, error) {
	hash := sha256.New()
	ciphertext, err := rsa.EncryptOAEP(hash, rand.Reader, pubKey, data, nil)
	if err != nil {
		return nil, fmt.Errorf("rsa encrypt: %w", err)
	}
	return ciphertext, nil
}

// ParsePublicKey 解析 PEM 编码的 RSA 公钥。
// 支持 PKIX 和 PKCS1 两种格式，优先尝试 PKIX（公钥最常用格式）。
//
// 支持格式：
//   - PKIX（SubjectPublicKeyInfo）：标准公钥格式，以 "-----BEGIN PUBLIC KEY-----" 开头
//   - PKCS1：旧式 RSA 公钥格式，以 "-----BEGIN RSA PUBLIC KEY-----" 开头
//
// 参数：
//   - pemData: PEM 编码的公钥数据
//
// 返回：
//   - *rsa.PublicKey: 解析后的 RSA 公钥
//   - error: 解析失败时返回错误
func ParsePublicKey(pemData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	// 优先尝试 PKIX 格式（公钥最常用）
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err == nil {
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("public key is not RSA")
		}
		return rsaPub, nil
	}

	// 降级尝试 PKCS1 格式
	rsaPub, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: not PKIX or PKCS1 format")
	}
	return rsaPub, nil
}
