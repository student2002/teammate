// errors.go 定义共享的 API 错误码、哨兵错误和错误响应结构体。
//
// 本文件包含：
//   - ErrCode* 常量：统一的 API 错误码
//   - Err* 哨兵错误：domain 层错误，供 Store/Service 抛出、Handler 识别
//   - IsNotFound helper：识别"资源不存在"错误（兼容 sql.ErrNoRows 包装链）
//   - ErrorResponse：标准的 API 错误响应格式
package types

import (
	"errors"
)

// API 响应的错误码常量。
const (
	ErrCodeBadRequest         = "bad_request"          // 请求参数错误
	ErrCodeUnauthorized       = "unauthorized"         // 未认证
	ErrCodeForbidden          = "forbidden"            // 无权限
	ErrCodeNotFound           = "not_found"            // 资源不存在
	ErrCodeInternal           = "internal"             // 内部服务器错误
	ErrCodeConflict           = "conflict"             // 资源冲突（如并发认领）
	ErrCodeServiceUnavailable = "service_unavailable"  // 服务不可用
)

// ErrNotFound 是 domain 层的"资源不存在"哨兵错误。
//
// Store 层在底层查询返回 sql.ErrNoRows 时，应将其包装为 ErrNotFound：
//
//	if errors.Is(err, sql.ErrNoRows) {
//	    return types.Task{}, fmt.Errorf("get task: %w", types.ErrNotFound)
//	}
//
// 这样 Handler 层只判 types.IsNotFound(err)，不再直接依赖 database/sql。
var ErrNotFound = errors.New("resource not found")

// IsNotFound 判断 err 是否表示"资源不存在"。
//
// 通过 errors.Is 沿错误链查找 ErrNotFound。任何被包装为 ErrNotFound 的错误
// 都会被识别（包括 Store 层从 sql.ErrNoRows 转换而来的情况）。
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// ErrNodeStateConflict 表示节点当前状态不允许执行该操作（如 pending 却要求审批/驳回/完成）。
var ErrNodeStateConflict = errors.New("node is not in the expected state")

// IsNodeStateConflict 判断错误链里是否含节点状态冲突。
func IsNodeStateConflict(err error) bool {
	return errors.Is(err, ErrNodeStateConflict)
}

// ErrorResponse 是标准的 API 错误响应格式。
type ErrorResponse struct {
	Code    string `json:"code"`    // 错误码
	Message string `json:"message"` // 错误消息
	Details any    `json:"details"` // 详细信息（可选）
}
