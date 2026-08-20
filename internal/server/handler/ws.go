// ws.go 提供 WebSocket 实时日志流、日志消息推送和日志查询等 HTTP API 端点。
//
// 本文件提供以下 HTTP API 端点：
//   - GET /ws/tasks/{taskId}: WebSocket 升级端点，订阅指定任务的实时日志流
//   - POST /api/tasks/{taskId}/messages: Agent 守护进程推送日志消息到 Gateway
//   - GET /api/tasks/{taskId}/logs: 查询任务的缓冲日志消息列表
//
// WebSocket 连接通过 token 查询参数认证（支持 JWT、session token、API key 三种方式），
// 连接后自动订阅任务日志频道并实时推送，支持按 node_id 过滤。
// 日志消息通过 ws.Gateway 发布/订阅，底层使用 Redis Pub/Sub + 缓冲实现。

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/service"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 允许所有来源——CORS 在路由层处理。
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSHandler 处理 WebSocket 连接的 HTTP 请求，提供实时日志流推送。
type WSHandler struct {
	Gateway   *ws.Gateway
	JWTSecret string
	Svc       *service.Service
	Checker   svcmw.WorkspaceAccessCheckerFunc // 工作区访问检查器（注入，非全局）
}

// NewWSHandler 创建 WSHandler 实例。
//
// 参数:
//   - gateway: WebSocket 网关实例，负责消息发布/订阅
//   - jwtSecret: JWT 签名密钥，用于验证 token
//   - svc: 业务逻辑服务实例，提供任务和节点查询能力
//   - checker: 工作区访问检查器（注入，非全局）
//
// 返回:
//   - *WSHandler: WebSocket 处理器实例
func NewWSHandler(gateway *ws.Gateway, jwtSecret string, svc *service.Service, checker svcmw.WorkspaceAccessCheckerFunc) *WSHandler {
	return &WSHandler{
		Gateway:   gateway,
		JWTSecret: jwtSecret,
		Svc:       svc,
		Checker:   checker,
	}
}

// HandleWS 处理 WebSocket 升级请求，通过 token 查询参数认证后，订阅指定任务的日志消息并实时推送给客户端。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 taskId 为任务 ID，查询参数 token 为认证令牌，node_id 可选用于过滤
//
// 返回:
//   - 无返回值，连接建立后持续推送日志消息直到客户端断开
func (h *WSHandler) HandleWS(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		taskID = r.URL.Query().Get("task_id")
	}
	if taskID == "" {
		response.BadRequest(w, "missing task_id")
		return
	}

	// 通过 token 查询参数进行认证（WebSocket 不支持请求头）。
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		response.Unauthorized(w, "missing authentication token")
		return
	}

	claims, err := h.validateToken(r.Context(), tokenStr)
	if err != nil {
		slog.Error("websocket auth failed", "task_id", taskID, "err", err)
		response.Unauthorized(w, "unauthorized: "+err.Error())
		return
	}

	// 验证用户是否有权访问该任务
	taskIDInt, err := strconv.ParseInt(taskID, 10, 32)
	if err != nil {
		response.BadRequest(w, "invalid task_id")
		return
	}

	if h.Svc != nil {
		taskSvc := service.NewTaskService(h.Svc)
		task, err := taskSvc.Get(r.Context(), int32(taskIDInt))
		if err != nil {
			response.NotFound(w, "task not found")
			return
		}
		taskProjectID, _ := uuid.Parse(task.ProjectID)
		project, err := service.NewProjectService(h.Svc).Get(r.Context(), taskProjectID)
		if err != nil {
			response.NotFound(w, "task not found")
			return
		}

		if claims.UserType == "agent" {
			// Agent 必须是任务的节点 assignee（而不仅是项目成员）
			taskSvc := service.NewTaskService(h.Svc)
			nodes, err := taskSvc.ListTaskNodes(r.Context(), int32(taskIDInt))
			if err != nil {
				response.NotFound(w, "task not found")
				return
			}
			isAssignee := false
			for _, node := range nodes {
				if node.AssigneeID != nil {
					if assigneeID, err := uuid.Parse(*node.AssigneeID); err == nil && assigneeID == claims.UserID {
						isAssignee = true
						break
					}
				}
			}
			if !isAssignee {
				response.Forbidden(w, "agent is not an assignee of this task")
				return
			}
		} else {
			if h.Checker == nil {
				response.InternalServerError(w, fmt.Errorf("workspace access checker not configured"))
				return
			}
			wsUUID, _ := uuid.Parse(project.WorkspaceID)
			if _, err := h.Checker(r.Context(), claims.UserID, claims.UserType, wsUUID); err != nil {
				response.NotFound(w, "task not found")
				return
			}
		}
	}

	// 可选的 node_id 过滤器：仅转发该节点的日志。
	nodeID := r.URL.Query().Get("node_id")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "task_id", taskID, "err", err)
		return
	}
	defer conn.Close()

	ch, unsub := h.Gateway.Subscribe(taskID)
	defer unsub()

	// 设置读超时时间以处理 pong。
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// 启动 Ping 协程以保持连接活跃。
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			}
		}
	}()

	// 读取循环，处理客户端消息（主要用于检测断连）。
	go func() {
		defer cancel()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// 写入循环：将日志消息转发给 WebSocket 客户端。
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if nodeID != "" && msg.NodeID != nodeID {
				continue // 跳过不匹配的节点
			}
			data, err := json.Marshal(msg)
			if err != nil {
				slog.Error("marshal log message for websocket", "task_id", taskID, "err", err)
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}

// validateToken 验证 token 字符串以进行 WebSocket 认证。
// 支持 JWT Bearer token、session token（st_ 前缀）和 API key（tm_ 前缀）三种认证方式。
//
// 参数:
//   - ctx: 请求上下文
//   - tokenStr: 待验证的 token 字符串
//
// 返回:
//   - svcmw.AuthClaims: 认证声明信息，包含用户 ID、类型、工作区 ID 和角色
//   - error: 验证失败时返回错误
func (h *WSHandler) validateToken(ctx context.Context, tokenStr string) (svcmw.AuthClaims, error) {
	// 优先尝试 API key / session token（st_ 或 tm_ 前缀）
	if h.Svc != nil && (strings.HasPrefix(tokenStr, "st_") || strings.HasPrefix(tokenStr, "tm_")) {
		authSvc := service.NewAuthService(h.Svc, "")
		result, err := authSvc.AuthenticateAPIKey(ctx, tokenStr)
		if err != nil {
			return svcmw.AuthClaims{}, err
		}
		return svcmw.AuthClaims{
			UserID:   result.UserID,
			UserType: result.UserType,
		}, nil
	}

	// 回退到 JWT
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(h.JWTSecret), nil
	})
	if err != nil {
		return svcmw.AuthClaims{}, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return svcmw.AuthClaims{}, jwt.ErrSignatureInvalid
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return svcmw.AuthClaims{}, jwt.ErrSignatureInvalid
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return svcmw.AuthClaims{}, jwt.ErrSignatureInvalid
	}

	userType, _ := claims["user_type"].(string)
	if userType != "member" && userType != "agent" {
		return svcmw.AuthClaims{}, jwt.ErrSignatureInvalid
	}

	return svcmw.AuthClaims{
		UserID:   userID,
		UserType: userType,
	}, nil
}

// PostLogMessageHandler 处理 POST /api/tasks/{taskId}/messages 端点，接收 Agent 守护进程推送的日志消息并通过 Gateway 分发。
type PostLogMessageHandler struct {
	Gateway *ws.Gateway
	Svc     *service.Service
	Checker svcmw.WorkspaceAccessCheckerFunc // 工作区访问检查器（注入，非全局）
}

// NewPostLogMessageHandler 创建 PostLogMessageHandler 实例。
//
// 参数:
//   - gateway: WebSocket 网关实例，负责消息发布
//   - svc: 业务逻辑服务实例，提供节点和任务验证能力
//   - checker: 工作区访问检查器（注入，非全局）
//
// 返回:
//   - *PostLogMessageHandler: 日志消息推送处理器实例
func NewPostLogMessageHandler(gateway *ws.Gateway, svc *service.Service, checker svcmw.WorkspaceAccessCheckerFunc) *PostLogMessageHandler {
	return &PostLogMessageHandler{Gateway: gateway, Svc: svc, Checker: checker}
}

type postLogMessageRequest struct {
	NodeID  string `json:"node_id"`
	Type    string `json:"type"` // "stdout", "stderr", "system"
	Content string `json:"content"`
}

// ServeHTTP 处理日志消息推送请求，验证 Agent 身份后将消息通过 Gateway 发布到对应任务的频道。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 taskId 为任务 ID，请求体包含节点 ID、消息类型和内容
//
// 返回:
//   - 无返回值，成功时返回 202 Accepted，失败时返回错误信息
func (h *PostLogMessageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		response.BadRequest(w, "missing task_id")
		return
	}

	// 验证已认证的 Agent 是否为该节点的 assignee
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 解析请求体
	var req postLogMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// node_id 为必填
	if req.NodeID == "" {
		response.BadRequest(w, "node_id is required")
		return
	}
	nodeID, err := uuid.Parse(req.NodeID)
	if err != nil {
		response.BadRequest(w, "node_id must be a valid UUID")
		return
	}

	var taskIDInt int32
	if _, err := fmt.Sscanf(taskID, "%d", &taskIDInt); err != nil {
		response.BadRequest(w, "invalid task_id")
		return
	}

	if h.Svc != nil {
		node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), nodeID)
		if err != nil {
			response.NotFound(w, "node not found")
			return
		}

		// 验证节点是否属于该任务
		if node.TaskID != taskIDInt {
			response.Forbidden(w, "node does not belong to this task")
			return
		}

		// 验证节点处于 in_progress 状态
		if node.Status != TaskNodeStatusInProgress {
			response.Conflict(w, "node is not in progress")
			return
		}

		if claims.UserType == "agent" {
			// Agent 必须是该节点的 assignee
			if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
				response.Forbidden(w, "agent is not the assignee of this node")
				return
			}
		} else {
			// Member：基于持久化资源归属验证工作区访问权限。
			taskSvc := service.NewTaskService(h.Svc)
			task, err := taskSvc.Get(r.Context(), taskIDInt)
			if err != nil {
				response.NotFound(w, "task not found")
				return
			}
			taskProjectID, _ := uuid.Parse(task.ProjectID)
			project, err := service.NewProjectService(h.Svc).Get(r.Context(), taskProjectID)
			if err != nil {
				response.NotFound(w, "task not found")
				return
			}
			if h.Checker == nil {
				response.InternalServerError(w, fmt.Errorf("workspace access checker not configured"))
				return
			}
			wsUUID, _ := uuid.Parse(project.WorkspaceID)
			if _, err := h.Checker(r.Context(), claims.UserID, claims.UserType, wsUUID); err != nil {
				response.NotFound(w, "task not found")
				return
			}
		}
	}

	if req.Type == "" {
		req.Type = "stdout"
	}
	switch req.Type {
	case "stdout", "stderr", "system":
		// 有效
	default:
		response.BadRequest(w, "invalid type, must be stdout/stderr/system")
		return
	}

	timestamp := time.Now()
	content := ws.Desensitize(req.Content)
	msg := ws.LogMessage{
		TaskID:    taskID,
		NodeID:    req.NodeID,
		Type:      req.Type,
		Content:   content,
		Timestamp: timestamp.UnixMilli(),
	}

	if h.Svc != nil {
		if err := service.NewTaskLogService(h.Svc).Create(r.Context(), taskIDInt, nodeID, req.Type, content, timestamp); err != nil {
			slog.Error("persist log message", "task_id", taskID, "node_id", req.NodeID, "err", err)
			response.InternalServerError(w, fmt.Errorf("failed to persist log message"))
			return
		}
	}

	if h.Gateway != nil {
		if err := h.Gateway.PublishLog(r.Context(), taskID, msg); err != nil {
			slog.Error("publish log message", "task_id", taskID, "err", err)
			response.InternalServerError(w, fmt.Errorf("failed to publish log message"))
			return
		}
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GetTaskLogsHandler 处理 GET /api/tasks/{taskId}/logs 端点，返回任务的缓冲日志消息列表。
type GetTaskLogsHandler struct {
	Gateway *ws.Gateway
	Svc     *service.Service
}

// NewGetTaskLogsHandler 创建 GetTaskLogsHandler 实例。
//
// 参数:
//   - gateway: WebSocket 网关实例，提供日志缓冲区读取能力
//
// 返回:
//   - *GetTaskLogsHandler: 日志查询处理器实例
func NewGetTaskLogsHandler(gateway *ws.Gateway, svc *service.Service) *GetTaskLogsHandler {
	return &GetTaskLogsHandler{Gateway: gateway, Svc: svc}
}

// ServeHTTP 处理日志查询请求，从 Redis 缓冲区获取任务的日志消息列表，支持按 node_id 过滤。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 taskId 为任务 ID，查询参数 node_id 可选用于过滤
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含日志消息列表或错误信息
func (h *GetTaskLogsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		response.BadRequest(w, "missing task_id")
		return
	}

	var taskIDInt int32
	if _, err := fmt.Sscanf(taskID, "%d", &taskIDInt); err != nil {
		response.BadRequest(w, "invalid task_id")
		return
	}

	var logs []ws.LogMessage
	if h.Gateway != nil {
		var err error
		logs, err = h.Gateway.GetBufferedLogs(r.Context(), taskID)
		if err != nil {
			slog.Error("get buffered logs", "task_id", taskID, "err", err)
			response.InternalServerError(w, fmt.Errorf("failed to get logs"))
			return
		}
	}

	// 可选的 node_id 过滤器：仅返回指定节点的日志。
	nodeID := r.URL.Query().Get("node_id")
	var parsedNodeID uuid.UUID
	if nodeID != "" {
		var err error
		parsedNodeID, err = uuid.Parse(nodeID)
		if err != nil {
			response.BadRequest(w, "node_id must be a valid UUID")
			return
		}
		filtered := make([]ws.LogMessage, 0, len(logs))
		for _, m := range logs {
			if m.NodeID == nodeID {
				filtered = append(filtered, m)
			}
		}
		logs = filtered
	}

	if len(logs) == 0 && h.Svc != nil {
		dbLogs, err := loadTaskLogsFromDB(r.Context(), h.Svc, taskIDInt, parsedNodeID, nodeID != "")
		if err != nil {
			slog.Error("get persisted logs", "task_id", taskID, "node_id", nodeID, "err", err)
			response.InternalServerError(w, fmt.Errorf("failed to get logs"))
			return
		}
		logs = taskLogsToMessages(dbLogs)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func loadTaskLogsFromDB(ctx context.Context, svc *service.Service, taskID int32, nodeID uuid.UUID, filterByNode bool) ([]service.TaskLogRecord, error) {
	taskLogSvc := service.NewTaskLogService(svc)
	if filterByNode {
		return taskLogSvc.List(ctx, taskID, &nodeID)
	}
	return taskLogSvc.List(ctx, taskID, nil)
}

func taskLogsToMessages(logs []service.TaskLogRecord) []ws.LogMessage {
	messages := make([]ws.LogMessage, 0, len(logs))
	for _, log := range logs {
		messages = append(messages, ws.LogMessage{
		 TaskID:    strconv.FormatInt(int64(log.TaskID), 10),
		 NodeID:    log.NodeID,
		 Type:      log.Type,
			Content:   log.Content,
			Timestamp: log.Timestamp.UnixMilli(),
		})
	}
	return messages
}
