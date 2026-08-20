// sse.go 提供 Server-Sent Events（SSE）实时事件推送端点，用于 Agent 守护进程的事件订阅和断线重连补偿。
//
// SSE 事件类型：node:pending、node:continuation_invite、task:interrupt、node:timeout 等。
// 断线重连：Agent 重连时发送 Last-Event-ID，Server 从 Redis 缓冲回放丢失事件。

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/service"
)

// SSEHandler 处理 Server-Sent Events 实时事件推送的 HTTP 请求，支持断线重连补偿。
type SSEHandler struct {
	Svc *service.Service
	Hub *ws.Hub // SSE 事件 Hub
}

// NewSSEHandler 创建 SSEHandler 实例。
func NewSSEHandler(svc *service.Service, hub *ws.Hub) *SSEHandler {
	return &SSEHandler{Svc: svc, Hub: hub}
}

// Routes 返回 SSE 的路由表。
func (h *SSEHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.Stream)

	return r
}

// Stream 处理 GET /workspaces/{workspaceId}/runtimes/{runtimeId}/events 端点，建立 SSE 长连接并实时推送事件给 Agent 守护进程，支持 Last-Event-ID 断线重连补偿。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求头：
//   - Last-Event-ID: string，上次接收的事件 ID（用于断线重连）
//
// 响应：
//   - 200: SSE 事件流（text/event-stream）
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: Agent 只能订阅自己的运行时
//   - 404: 运行时不存在
//
// 处理流程：
//  1. 验证认证和运行时归属
//  2. 检查 Last-Event-ID 头进行断线重连
//  3. 从 Redis 缓冲回放丢失事件
//  4. 订阅事件频道
//  5. 保持长连接并推送事件
func (h *SSEHandler) Stream(w http.ResponseWriter, r *http.Request) {
	// 解析运行时 ID
	runtimeID := chi.URLParam(r, "runtimeId")
	if runtimeID == "" {
		response.BadRequest(w, "missing runtimeId")
		return
	}

	// 解析工作区 ID
	workspaceIDStr := chi.URLParam(r, "workspaceId")
	if workspaceIDStr == "" {
		response.BadRequest(w, "missing workspaceId")
		return
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		response.BadRequest(w, "invalid workspaceId")
		return
	}

	// 验证认证
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 验证运行时归属
	runtimeUUID, err := uuid.Parse(runtimeID)
	if err != nil {
		response.BadRequest(w, "invalid runtimeId")
		return
	}
	runtime, err := service.NewRuntimeService(h.Svc).GetRuntimeByID(r.Context(), runtimeUUID)
	if err != nil {
		response.NotFound(w, "runtime not found")
		return
	}

	// 验证运行时的 Agent 属于 URL 工作区
	runtimeAgentID, _ := uuid.Parse(runtime.AgentID)
	agent, err := service.NewAgentService(h.Svc).Get(r.Context(), runtimeAgentID)
	if err != nil || agent.WorkspaceID != workspaceID.String() {
		response.NotFound(w, "runtime not found")
		return
	}

	// Agent 只能订阅自己的运行时
	if claims.UserType == "agent" {
		sseAgentID, _ := uuid.Parse(runtime.AgentID)
		if sseAgentID != claims.UserID {
			response.Forbidden(w, "agents can only subscribe to their own runtime")
			return
		}
	}

	// chi 的 Timeout 中间件包装了 ResponseWriter，隐藏了 http.Flusher。
	// 我们需要解包以获取底层的 Flusher。
	var flusher http.Flusher
	var flushOK bool
	flusher, flushOK = w.(http.Flusher)
	if !flushOK {
		// 尝试 chi 的 wrapResponseWriter 接口
		type wrapResponseWriter interface {
			http.ResponseWriter
			Unwrap() http.ResponseWriter
		}
		if wrapped, wrapOK := w.(wrapResponseWriter); wrapOK {
			flusher, flushOK = wrapped.Unwrap().(http.Flusher)
		}
	}
	if !flushOK {
		response.InternalServerError(w, fmt.Errorf("streaming not supported"))
		return
	}

	// 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// 禁用写入超时，防止服务器的 WriteTimeout 关闭长连接
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Warn("failed to disable write deadline for SSE connection", "err", err)
	}

	// 检查 Last-Event-ID 头以支持断线重连
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID != "" {
		h.replayBufferedEvents(r.Context(), w, flusher, runtimeID, lastEventID)
	}

	// 订阅事件频道
	ch, unsub := h.Hub.Subscribe(runtimeID)
	defer unsub()

	// 发送初始注释以建立连接
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	// 启动保活定时器，防止空闲连接被关闭
	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	// 主循环：推送事件
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			// 发送 SSE 注释作为保活——客户端忽略注释，
			// 但代理/负载均衡器看到活动不会关闭连接
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEvent(w, flusher, event)
		}
	}
}

// replayBufferedEvents 从 Redis 缓冲区获取丢失的事件并发送给客户端。
// 如果缓冲区为空或已过期，则发送 sync:required 事件。
func (h *SSEHandler) replayBufferedEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, runtimeID string, lastEventID string) {
	events, err := h.Hub.GetBufferedEvents(ctx, runtimeID, lastEventID)
	if err != nil {
		slog.Error("get buffered events for replay", "runtime_id", runtimeID, "last_event_id", lastEventID, "err", err)
	}

	// 缓冲区为空或已过期——发送 sync:required
	if len(events) == 0 {
		syncData, _ := json.Marshal(map[string]string{
			"reason": "buffer_expired",
			"action": "full_sync",
		})
		writeSSEEvent(w, flusher, ws.SSEEvent{
			ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
			Event: ws.EventSyncRequired,
			Data:  syncData,
		})
		return
	}

	// 回放缓冲的事件
	for _, event := range events {
		writeSSEEvent(w, flusher, event)
	}
}

// writeSSEEvent 将单个 SSE 事件写入响应写入器并刷新。
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event ws.SSEEvent) {
	if event.ID != "" {
		fmt.Fprintf(w, "id: %s\n", event.ID)
	}
	if event.Event != "" {
		fmt.Fprintf(w, "event: %s\n", event.Event)
	}
	fmt.Fprintf(w, "data: %s\n\n", event.Data)
	flusher.Flush()
}
