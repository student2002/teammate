// desensitize.go 提供统一的敏感数据脱敏工具，用于日志输出和错误响应。
//
// 覆盖范围：
//   - API Token（tm_/st_/sk_ 前缀的令牌）
//   - JWT Token
//   - PAT（Personal Access Token）
//   - 会话密钥和加密密钥
//   - 环境变量值中的敏感信息
//
// 使用方式：
//
//	slog.Info("processing", "credential", Desensitize(rawCredential))
//	log.Printf("token: %s", Desensitize(sensitiveToken))
package middleware

import (
	"regexp"
	"strings"
)

// 敏感数据模式
var (
	// apiTokenPattern 匹配 API 令牌（tm_ / st_ / sk_ 前缀，后跟 32+ 字符）
	apiTokenPattern = regexp.MustCompile(`(tm_|st_|sk_)[A-Za-z0-9]{16,}`)

	// jwtPattern 匹配 JWT Token（header.payload.signature 格式）
	jwtPattern = regexp.MustCompile(`[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]{20,}`)

	// patPattern 匹配 Personal Access Token（ghp_ / github_pat_ / glpat- 等前缀）
	patPattern = regexp.MustCompile(`(ghp_|github_pat_|glpat-|gho_|ghu_)[A-Za-z0-9_-]{10,}`)

	// encryptionKeyPattern 匹配 Base64 编码的加密密钥（40+ 字符 Base64）
	encryptionKeyPattern = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
)

// Desensitize 替换字符串中的所有敏感数据为 "******"。
//
// 参数：
//   - s: 可能包含敏感数据的原始字符串
//
// 返回：
//   - string: 脱敏后的安全字符串
func Desensitize(s string) string {
	if s == "" {
		return ""
	}
	result := apiTokenPattern.ReplaceAllString(s, "******")
	result = jwtPattern.ReplaceAllString(result, "******")
	result = patPattern.ReplaceAllString(result, "******")
	// 加密密钥脱敏（仅在看起来像密钥时脱敏）
	if looksLikeKey(s) {
		result = encryptionKeyPattern.ReplaceAllString(result, "******")
	}
	return result
}

// DesensitizeMap 脱敏 map 中的所有字符串值。
//
// 参数：
//   - m: 可能包含敏感数据的 map
//
// 返回：
//   - map[string]interface{}: 脱敏后的安全 map
func DesensitizeMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			result[k] = Desensitize(val)
		default:
			result[k] = v
		}
	}
	return result
}

// looksLikeKey 启发式判断字符串是否可能是加密密钥。
func looksLikeKey(s string) bool {
	if len(s) < 40 {
		return false
	}
	// 如果包含常见的非密钥字符，可能不是密钥
	if strings.Contains(s, " ") || strings.Contains(s, "\n") || strings.Contains(s, "\t") {
		return false
	}
	// Base64 字符集检查
	base64Chars := 0
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			base64Chars++
		}
	}
	return float64(base64Chars)/float64(len(s)) > 0.9
}
