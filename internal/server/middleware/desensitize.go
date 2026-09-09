// desensitize.go provides unified sensitive-data masking utilities for log output and error responses.
//
// Coverage:
//   - API tokens (tokens with the tm_/st_/sk_ prefix)
//   - JWT tokens
//   - PAT (Personal Access Token)
//   - Session keys and encryption keys
//   - Sensitive information inside environment variable values
//
// Usage:
//
//	slog.Info("processing", "credential", Desensitize(rawCredential))
//	log.Printf("token: %s", Desensitize(sensitiveToken))
package middleware

import (
	"regexp"
	"strings"
)

// Sensitive data patterns
var (
	// apiTokenPattern matches API tokens (tm_ / st_ / sk_ prefix followed by 16+ characters)
	apiTokenPattern = regexp.MustCompile(`(tm_|st_|sk_)[A-Za-z0-9]{16,}`)

	// jwtPattern matches JWT tokens (header.payload.signature format)
	jwtPattern = regexp.MustCompile(`[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]{20,}`)

	// patPattern matches Personal Access Tokens (ghp_ / github_pat_ / glpat- and other prefixes)
	patPattern = regexp.MustCompile(`(ghp_|github_pat_|glpat-|gho_|ghu_)[A-Za-z0-9_-]{10,}`)

	// encryptionKeyPattern matches Base64-encoded encryption keys (40+ character Base64)
	encryptionKeyPattern = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
)

// Desensitize replaces all sensitive data in a string with "******".
//
// Parameters:
//   - s: the original string that may contain sensitive data
//
// Returns:
//   - string: the desensitized safe string
func Desensitize(s string) string {
	if s == "" {
		return ""
	}
	result := apiTokenPattern.ReplaceAllString(s, "******")
	result = jwtPattern.ReplaceAllString(result, "******")
	result = patPattern.ReplaceAllString(result, "******")
	// Mask encryption keys (only when the string looks like a key)
	if looksLikeKey(s) {
		result = encryptionKeyPattern.ReplaceAllString(result, "******")
	}
	return result
}

// DesensitizeMap masks all string values in a map.
//
// Parameters:
//   - m: the map that may contain sensitive data
//
// Returns:
//   - map[string]interface{}: the desensitized safe map
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

// looksLikeKey heuristically determines whether a string is likely an encryption key.
func looksLikeKey(s string) bool {
	if len(s) < 40 {
		return false
	}
	// If it contains common non-key characters, it is probably not a key
	if strings.Contains(s, " ") || strings.Contains(s, "\n") || strings.Contains(s, "\t") {
		return false
	}
	// Base64 character set check
	base64Chars := 0
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			base64Chars++
		}
	}
	return float64(base64Chars)/float64(len(s)) > 0.9
}
