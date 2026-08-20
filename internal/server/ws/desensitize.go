// desensitize.go 提供日志内容脱敏功能，在发布前对敏感数据进行自动替换。
// 支持脱敏的数据类型包括：API Key、Bearer Token、JWT、密码、电子邮件地址。
// 脱敏规则在保护安全的同时保留足够的调试信息：
//   - API Key 保留前缀（sk-/tm_/key_），其余替换为 ****
//   - Bearer Token 全部替换为 "Bearer ****"
//   - JWT 保留段首字符，其余替换为 ****
//   - 密码保留键名，密码值替换为 ****
//   - 邮箱保留首字符和域名
package ws

import (
	"regexp"
	"strings"
)

var (
	// reAPIKey 匹配 API Key 格式：sk-...、tm_...、key_...
	// 这些是 Teammate 系统中使用的令牌前缀。
	reAPIKey = regexp.MustCompile(`(?i)(sk-|tm_|key_)[\w\-]{8,}`)

	// reBearer 匹配 Bearer Token，格式为 "Bearer <token>"
	reBearer = regexp.MustCompile(`(?i)Bearer\s+\S+`)

	// reJWT 匹配 JWT 格式的令牌（三个 base64url 段用点分隔，以 eyJ 开头）
	reJWT = regexp.MustCompile(`eyJ[\w\-]+\.eyJ[\w\-]+\.[\w\-]+`)

	// rePassword 匹配 password=、pass=、pwd= 后面的密码值
	rePassword = regexp.MustCompile(`(?i)(password|pass|pwd)\s*[=:]\s*\S+`)

	// reEmail 匹配电子邮件地址格式
	reEmail = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
)

// Desensitize 在发布前对日志内容中的敏感数据进行脱敏处理。
// 脱敏规则在保护安全的同时保留足够的调试信息，便于定位问题。
//
// 脱敏规则：
//   - API Key：保留前缀（sk-/tm_/key_），其余替换为 ****
//   - Bearer Token：全部替换为 "Bearer ****"
//   - JWT：保留三段的首字符，其余替换为 ****
//   - 密码：保留键名（如 password=），密码值替换为 ****
//   - 邮箱：保留首字符和域名（如 j***@example.com）
//
// 参数：
//   - content: 原始日志内容
//
// 返回：
//   - string: 脱敏后的日志内容
func Desensitize(content string) string {
	s := content

	// 脱敏 API Key — 保留前缀，其余替换为 ****
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

	// 脱敏 Bearer Token
	s = reBearer.ReplaceAllString(s, "Bearer ****")

	// 脱敏 JWT Token
	s = reJWT.ReplaceAllString(s, "eyJ****.eyJ****.****")

	// 脱敏密码 — 保留键名，密码值替换为 ****
	s = rePassword.ReplaceAllStringFunc(s, func(match string) string {
		// 找到分隔符位置
		for i, sep := range match {
			if sep == '=' || sep == ':' {
				key := strings.TrimSpace(match[:i])
				return key + string(sep) + "****"
			}
		}
		return "****"
	})

	// 脱敏邮箱 — 保留首字符和域名
	s = reEmail.ReplaceAllStringFunc(s, func(match string) string {
		parts := strings.SplitN(match, "@", 2)
		if len(parts) == 2 && len(parts[0]) > 1 {
			return string(parts[0][0]) + "***@" + parts[1]
		}
		return "***@***"
	})

	return s
}
