// response.go 提供统一的 HTTP 响应格式化工具，包括 JSON 响应和标准错误响应。
// 自动清理 sqlc 生成的 NullXxx 可空类型包装器，使 API 响应更简洁。
// 提供一组便捷函数用于生成标准错误响应（400/401/403/404/409/500/503）。
// 安全说明：生产环境下 InternalServerError 隐藏内部错误详情，仅返回错误码。
package response

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/teammate/server/internal/types"
)

// nullWrapperKeys 是 sqlc NullXxx 包装器的已知字段名列表。
// 当 map 中恰好包含以下一个键加一个 "Valid" 键时，认为是 sqlc 可空包装器。
var nullWrapperKeys = []string{
	"String",
	"UUID",
	"Time",
	"Int64",
	"Float64",
	"Bool",
	"RawMessage",
}

// CleanValue 递归清理单个值中的 sqlc NullXxx 包装器。
// sqlc 生成的可空类型会序列化为 {"String":"x","Valid":true} 形式的 JSON 对象，
// 该函数将其解包为原始值（如 "x"），使 API 响应更简洁。
//
// 清理规则：
//   - {"String":"x","Valid":true}  → "x"
//   - {"String":"","Valid":false}  → nil
//   - {"UUID":"...","Valid":true}  → "..."
//   - {"UUID":"00000000-...","Valid":false} → nil
//   - {"Time":"...","Valid":true}  → "..."
//   - {"Time":"...","Valid":false} → nil
//   - {"RawMessage":...,"Valid":true}  → 原始 JSON 值
//   - {"RawMessage":null,"Valid":false} → nil
//   - {"Int64":0,"Valid":true}     → 0
//   - {"Int64":0,"Valid":false}    → nil
//   - {"Float64":0,"Valid":true}   → 0
//   - {"Float64":0,"Valid":false}  → nil
//   - {"Bool":false,"Valid":true}  → false
//   - {"Bool":false,"Valid":false} → nil
//
// 参数：
//   - v: 待清理的值（可以是任意类型）
//
// 返回：
//   - interface{}: 清理后的值
func CleanValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return cleanMapValue(val)
	case []interface{}:
		cleaned := make([]interface{}, len(val))
		for i, elem := range val {
			cleaned[i] = CleanValue(elem)
		}
		return cleaned
	default:
		return v
	}
}

// cleanMapValue 处理 map：如果是 NullXxx 包装器则解包，否则递归清理所有值。
//
// 参数：
//   - m: 待清理的 map
//
// 返回：
//   - interface{}: 解包后的原始值或清理后的 map
func cleanMapValue(m map[string]interface{}) interface{} {
	// 先递归清理 map 中的所有值
	cleaned := make(map[string]interface{}, len(m))
	for k, v := range m {
		cleaned[k] = CleanValue(v)
	}

	// 检查该 map 本身是否是 NullXxx 包装器（包含 "Valid" 键且恰好有两个键）
	if validVal, hasValid := cleaned["Valid"]; hasValid && len(cleaned) == 2 {
		for _, wrapperKey := range nullWrapperKeys {
			if innerVal, hasKey := cleaned[wrapperKey]; hasKey {
				valid, _ := validVal.(bool)
				if valid {
					if wrapperKey == "RawMessage" {
						return unwrapRawMessage(innerVal)
					}
					return innerVal
				}
				return nil
			}
		}
	}

	return cleaned
}

// unwrapRawMessage 处理 RawMessage 的特殊情况，其中内部值可能是需要解析回 JSON 的字符串。
//
// 参数：
//   - v: RawMessage 包装器中的内部值
//
// 返回：
//   - interface{}: 解析后的 JSON 值
func unwrapRawMessage(v interface{}) interface{} {
	// 如果原始消息被序列化为字符串，尝试反序列化
	if s, ok := v.(string); ok {
		var parsed interface{}
		if err := json.Unmarshal([]byte(s), &parsed); err == nil {
			return CleanValue(parsed)
		}
		return s
	}
	// 如果已经是解析后的 JSON 值（对象/数组等），递归清理
	return CleanValue(v)
}

// JSON 将值序列化为 JSON，清理 sqlc NullXxx 包装器后写入响应。
// 用于替代 render.JSON 处理所有 handler 响应。
//
// 优化：如果序列化后的输出不含 NullXxx 包装器（无 "Valid": 键），
// 则跳过反序列化/清理/重新序列化的循环，直接输出原始 JSON。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求对象
//   - v: 待序列化为 JSON 的值
func JSON(w http.ResponseWriter, r *http.Request, v interface{}) {
	raw, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 快速检查：如果不存在 NullXxx 包装器，跳过昂贵的反序列化→清理→重新序列化流程
	needsCleaning := bytes.Contains(raw, []byte(`"Valid":`))

	var out []byte
	if needsCleaning {
		var generic interface{}
		if err := json.Unmarshal(raw, &generic); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		cleaned := CleanValue(generic)
		out, err = json.Marshal(cleaned)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("response.JSON: cleaned %d bytes -> %d bytes", len(raw), len(out))
	} else {
		out = raw
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// isDevMode 检查是否在开发模式下运行（TEAMMATE_DEV=true）。
// 开发模式下会返回详细的错误信息，生产环境仅返回错误码。
func isDevMode() bool {
	return os.Getenv("TEAMMATE_DEV") == "true"
}

// ErrorBody 是标准的 JSON 错误响应体结构。
type ErrorBody struct {
	// Error 是错误码常量，如 "bad_request"、"unauthorized"、"internal"。
	Error string `json:"error"`
	// Message 是人类可读的错误描述，开发模式下包含详细错误信息。
	Message string `json:"message"`
}

// Error 写入一个 JSON 格式的错误响应。生产环境下隐藏内部错误详情。
//
// 参数：
//   - w: HTTP 响应写入器
//   - httpStatus: HTTP 状态码
//   - code: 错误码常量（来自 types 包）
//   - err: 原始错误对象（仅开发模式下返回给客户端）
func Error(w http.ResponseWriter, httpStatus int, code string, err error) {
	msg := code
	if isDevMode() && err != nil {
		msg = err.Error()
	}

	body := ErrorBody{Error: code, Message: msg}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(body)
}

// BadRequest 写入 400 错误响应，表示请求参数无效或请求格式错误。
//
// 参数：
//   - w: HTTP 响应写入器
//   - message: 错误描述信息
func BadRequest(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeBadRequest, Message: message})
}

// Unauthorized 写入 401 错误响应，表示未认证或认证令牌无效/过期。
//
// 参数：
//   - w: HTTP 响应写入器
//   - message: 错误描述信息
func Unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeUnauthorized, Message: message})
}

// Forbidden 写入 403 错误响应，表示已认证但权限不足，无法访问目标资源。
//
// 参数：
//   - w: HTTP 响应写入器
//   - message: 错误描述信息
func Forbidden(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeForbidden, Message: message})
}

// NotFound 写入 404 错误响应，表示请求的资源不存在。
//
// 参数：
//   - w: HTTP 响应写入器
//   - message: 错误描述信息
func NotFound(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeNotFound, Message: message})
}

// Conflict 写入 409 错误响应，表示资源冲突（如并发认领失败、乐观锁冲突）。
//
// 参数：
//   - w: HTTP 响应写入器
//   - message: 错误描述信息
func Conflict(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeConflict, Message: message})
}

// InternalServerError 写入 500 错误响应，生产环境下隐藏内部错误详情。
// 错误详情记录到服务器日志，便于排查问题。
//
// 参数：
//   - w: HTTP 响应写入器
//   - err: 原始错误对象
func InternalServerError(w http.ResponseWriter, err error) {
	log.Printf("[error] internal server error: %v", err)
	Error(w, http.StatusInternalServerError, types.ErrCodeInternal, err)
}

// ServiceUnavailable 写入 503 错误响应，表示服务暂时不可用（如维护中或过载）。
//
// 参数：
//   - w: HTTP 响应写入器
//   - message: 错误描述信息
func ServiceUnavailable(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeServiceUnavailable, Message: message})
}
