// events.go 定义 SSE（Server-Sent Events）相关的常量和结构体。
//
// 本文件包含：
//   - SSEEvent：Server-Sent Event 的结构定义
//   - Event* 常量：标准 SSE 事件类型
//
// SSE 用于 server → agent 的实时事件推送，支持 Last-Event-ID 重连。
package types

import "encoding/json"

// SSEEvent 定义 Server-Sent Event 的结构。
// 用于 server → agent 的实时事件推送。
type SSEEvent struct {
	ID    string          `json:"id"`    // 事件唯一标识，支持 Last-Event-ID 重连
	Event string          `json:"event"` // 事件类型（如 node:pending、task:interrupt）
	Data  json.RawMessage `json:"data"`  // 事件数据载荷（JSON）
}

// 标准 SSE 事件类型常量。
const (
	EventNodePending            = "node:pending"              // 新节点待认领
	EventNodeContinuationInvite = "node:continuation_invite"  // 节点完成后续约权邀请
	EventMentionTrigger         = "mention:trigger"           // @提及触发
	EventTaskInterrupt          = "task:interrupt"            // 任务中断（控制事件）
	EventNodeTimeout            = "node:timeout"              // 节点超时（控制事件）
	EventSyncRequired           = "sync:required"             // 需要全量同步（控制事件）
	EventNodeRejectRollback     = "node:reject_rollback"      // 审查拒绝回滚（控制事件）
	EventPermissionChanged      = "permission:changed"        // 权限变更（控制事件）
)
