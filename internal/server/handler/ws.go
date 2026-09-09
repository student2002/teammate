// ws.go provides HTTP API endpoints for WebSocket real-time log streaming, log message publishing, and log querying.
//
// This file provides the following HTTP API endpoints:
//   - GET /ws/tasks/{taskId}: WebSocket upgrade endpoint, subscribes to the real-time log stream of the specified task
//   - POST /api/tasks/{taskId}/messages: the Agent daemon pushes log messages to the Gateway
//   - GET /api/tasks/{taskId}/logs: query the buffered log message list of the task
//
// The WebSocket connection authenticates via the token query parameter (supporting JWT, session token, and API key),
// and after connecting it automatically subscribes to the task log channel and pushes in real time, supporting filtering by node_id.
// Log messages are published/subscribed via ws.Gateway, which uses Redis Pub/Sub + buffering under the hood.

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
	// allow all origins - CORS is handled at the routing layer.
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSHandler handles HTTP requests for WebSocket connections, providing real-time log stream push.
type WSHandler struct {
	Gateway   *ws.Gateway
	JWTSecret string
	Svc       *service.Service
	Checker   svcmw.WorkspaceAccessCheckerFunc // workspace access checker (injected, not global)
}

// NewWSHandler creates a WSHandler instance.
//
// Parameters:
//   - gateway: WebSocket gateway instance, responsible for message publishing/subscribing
//   - jwtSecret: JWT signing secret, used to verify the token
//   - svc: business logic service instance, provides task and node query capabilities
//   - checker: workspace access checker (injected, not global)
//
// Returns:
//   - *WSHandler: WebSocket handler instance
func NewWSHandler(gateway *ws.Gateway, jwtSecret string, svc *service.Service, checker svcmw.WorkspaceAccessCheckerFunc) *WSHandler {
	return &WSHandler{
		Gateway:   gateway,
		JWTSecret: jwtSecret,
		Svc:       svc,
		Checker:   checker,
	}
}

// HandleWS handles the WebSocket upgrade request, authenticates via the token query parameter, then subscribes to the specified task's log messages and pushes them to the client in real time.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter taskId is the task ID, query parameter token is the auth token, node_id is optional for filtering
//
// Returns:
//   - no return value, after the connection is established it continuously pushes log messages until the client disconnects
func (h *WSHandler) HandleWS(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		taskID = r.URL.Query().Get("task_id")
	}
	if taskID == "" {
		response.BadRequest(w, "missing task_id")
		return
	}

	// authenticate via the token query parameter (WebSocket does not support request headers).
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

	// verify the user has access to this task
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
			// the Agent must be a node assignee of the task (not just a project member)
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

	// optional node_id filter: only forward logs of this node.
	nodeID := r.URL.Query().Get("node_id")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "task_id", taskID, "err", err)
		return
	}
	defer conn.Close()

	ch, unsub := h.Gateway.Subscribe(taskID)
	defer unsub()

	// set the read deadline to handle pong.
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// start a Ping goroutine to keep the connection alive.
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

	// read loop, handles client messages (mainly used to detect disconnections).
	go func() {
		defer cancel()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// write loop: forwards log messages to the WebSocket client.
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if nodeID != "" && msg.NodeID != nodeID {
				continue // skip non-matching nodes
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

// validateToken verifies the token string for WebSocket authentication.
// Supports three auth methods: JWT Bearer token, session token (st_ prefix), and API key (tm_ prefix).
//
// Parameters:
//   - ctx: request context
//   - tokenStr: the token string to verify
//
// Returns:
//   - svcmw.AuthClaims: auth claim info, containing user ID, type, workspace ID, and role
//   - error: returns an error when verification fails
func (h *WSHandler) validateToken(ctx context.Context, tokenStr string) (svcmw.AuthClaims, error) {
	// prefer trying API key / session token (st_ or tm_ prefix)
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

	// fall back to JWT
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

// PostLogMessageHandler handles the POST /api/tasks/{taskId}/messages endpoint, receiving log messages pushed by the Agent daemon and distributing them via the Gateway.
type PostLogMessageHandler struct {
	Gateway *ws.Gateway
	Svc     *service.Service
	Checker svcmw.WorkspaceAccessCheckerFunc // workspace access checker (injected, not global)
}

// NewPostLogMessageHandler creates a PostLogMessageHandler instance.
//
// Parameters:
//   - gateway: WebSocket gateway instance, responsible for message publishing
//   - svc: business logic service instance, provides node and task verification capabilities
//   - checker: workspace access checker (injected, not global)
//
// Returns:
//   - *PostLogMessageHandler: log message push handler instance
func NewPostLogMessageHandler(gateway *ws.Gateway, svc *service.Service, checker svcmw.WorkspaceAccessCheckerFunc) *PostLogMessageHandler {
	return &PostLogMessageHandler{Gateway: gateway, Svc: svc, Checker: checker}
}

type postLogMessageRequest struct {
	NodeID  string `json:"node_id"`
	Type    string `json:"type"` // "stdout", "stderr", "system"
	Content string `json:"content"`
}

// ServeHTTP handles the log message push request, verifies the Agent identity, then publishes the message to the corresponding task channel via the Gateway.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter taskId is the task ID, request body contains the node ID, message type, and content
//
// Returns:
//   - no return value, returns 202 Accepted on success, or an error message on failure
func (h *PostLogMessageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		response.BadRequest(w, "missing task_id")
		return
	}

	// verify the authenticated Agent is the assignee of this node
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// parse the request body
	var req postLogMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// node_id is required
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

		// verify the node belongs to this task
		if node.TaskID != taskIDInt {
			response.Forbidden(w, "node does not belong to this task")
			return
		}

		// verify the node is in the in_progress status
		if node.Status != TaskNodeStatusInProgress {
			response.Conflict(w, "node is not in progress")
			return
		}

		if claims.UserType == "agent" {
			// the Agent must be the assignee of this node
			if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
				response.Forbidden(w, "agent is not the assignee of this node")
				return
			}
		} else {
			// Member: verify workspace access based on persisted resource ownership.
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
		// valid
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

// GetTaskLogsHandler handles the GET /api/tasks/{taskId}/logs endpoint, returning the buffered log message list of the task.
type GetTaskLogsHandler struct {
	Gateway *ws.Gateway
	Svc     *service.Service
}

// NewGetTaskLogsHandler creates a GetTaskLogsHandler instance.
//
// Parameters:
//   - gateway: WebSocket gateway instance, provides log buffer reading capabilities
//
// Returns:
//   - *GetTaskLogsHandler: log query handler instance
func NewGetTaskLogsHandler(gateway *ws.Gateway, svc *service.Service) *GetTaskLogsHandler {
	return &GetTaskLogsHandler{Gateway: gateway, Svc: svc}
}

// ServeHTTP handles the log query request, fetching the task's log message list from the Redis buffer, supporting filtering by node_id.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter taskId is the task ID, query parameter node_id is optional for filtering
//
// Returns:
//   - no return value, writes a JSON response via w, containing the log message list or an error message
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

	// optional node_id filter: only return logs of the specified node.
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
