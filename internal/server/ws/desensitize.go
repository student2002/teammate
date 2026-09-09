// desensitize.go provides log-content masking, automatically replacing sensitive data before publishing.
// Supported sensitive data types include: API Key, Bearer Token, JWT, password, and email address.
// The masking rules preserve enough debugging information while protecting security:
//   - API Key keeps the prefix (sk-/tm_/key_), the rest is replaced with ****
//   - Bearer Token is fully replaced with "Bearer ****"
//   - JWT keeps the first character of each segment, the rest is replaced with ****
//   - Password keeps the key name, the password value is replaced with ****
//   - Email keeps the first character and the domain
package ws

import (
	"regexp"
	"strings"
)

var (
	// reAPIKey matches the API Key format: sk-..., tm_..., key_...
	// These are the token prefixes used in the Teammate system.
	reAPIKey = regexp.MustCompile(`(?i)(sk-|tm_|key_)[\w\-]{8,}`)

	// reBearer matches a Bearer Token, in the format "Bearer <token>"
	reBearer = regexp.MustCompile(`(?i)Bearer\s+\S+`)

	// reJWT matches JWT-format tokens (three base64url segments separated by dots, starting with eyJ)
	reJWT = regexp.MustCompile(`eyJ[\w\-]+\.eyJ[\w\-]+\.[\w\-]+`)

	// rePassword matches the password value following password=, pass=, pwd=
	rePassword = regexp.MustCompile(`(?i)(password|pass|pwd)\s*[=:]\s*\S+`)

	// reEmail matches the email address format
	reEmail = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
)

// Desensitize masks sensitive data in the log content before publishing.
// The masking rules preserve enough debugging information while protecting security, to help locate issues.
//
// Masking rules:
//   - API Key: keep the prefix (sk-/tm_/key_), replace the rest with ****
//   - Bearer Token: fully replaced with "Bearer ****"
//   - JWT: keep the first character of each of the three segments, replace the rest with ****
//   - Password: keep the key name (e.g. password=), replace the password value with ****
//   - Email: keep the first character and the domain (e.g. j***@example.com)
//
// Parameters:
//   - content: the original log content
//
// Returns:
//   - string: the desensitized log content
func Desensitize(content string) string {
	s := content

	// Mask API Key — keep the prefix, replace the rest with ****
	s = reAPIKey.ReplaceAllStringFunc(s, func(match string) string {
		var prefix string
		if strings.HasPrefix(match, "sk-") {
			prefix = "sk-"
		} else if strings.HasPrefix(match, "tm_") {
			prefix = "tm_"
		} else if strings.HasPrefix(match, "key_") {
			prefix = "key_"
		}
		return prefix + "****"
	})

	// Mask Bearer Token
	s = reBearer.ReplaceAllString(s, "Bearer ****")

	// Mask JWT Token
	s = reJWT.ReplaceAllString(s, "eyJ****.eyJ****.****")

	// Mask password — keep the key name, replace the password value with ****
	s = rePassword.ReplaceAllStringFunc(s, func(match string) string {
		// Find the separator position
		for i, sep := range match {
			if sep == '=' || sep == ':' {
				key := strings.TrimSpace(match[:i])
				return key + string(sep) + "****"
			}
		}
		return "****"
	})

	// Mask email — keep the first character and the domain
	s = reEmail.ReplaceAllStringFunc(s, func(match string) string {
		parts := strings.SplitN(match, "@", 2)
		if len(parts) == 2 && len(parts[0]) > 1 {
			return string(parts[0][0]) + "***@" + parts[1]
		}
		return "***@***"
	})

	return s
}
