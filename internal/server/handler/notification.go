// notification.go 提供通知列表查询的 HTTP API 端点。
//
// 本文件提供以下 HTTP API 端点：
//   - GET /workspaces/{workspaceId}/notifications: 列出工作区下的通知，支持按 member_id 查询参数过滤
//
// 通知列表由 NotificationService 查询，返回指定工作区中所有通知或特定成员的通知。
// 请求路径参数 workspaceId 必须为合法的 UUID 格式；member_id 查询参数可选，若提供则仅返回该成员的通知。

package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// NotificationHandler 处理通知查询的 HTTP 请求。
type NotificationHandler struct {
	Svc *service.Service
}

// NewNotificationHandler 创建 NotificationHandler 实例。
//
// 参数:
//   - svc: 业务逻辑服务实例，提供通知查询能力
//
// 返回:
//   - *NotificationHandler: 通知处理器实例
func NewNotificationHandler(svc *service.Service) *NotificationHandler {
	return &NotificationHandler{Svc: svc}
}

// Routes 返回通知的路由表。
//
// 返回:
//   - chi.Router: 包含通知相关端点的路由
func (h *NotificationHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListNotifications)

	return r
}

// ListNotifications 处理 GET /workspaces/{workspaceId}/notifications 端点，列出工作区下的通知，支持按成员 ID 过滤。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID，查询参数 member_id 可选
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含通知列表或错误信息
func (h *NotificationHandler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	memberIDStr := r.URL.Query().Get("member_id")
	memberID, err := uuid.Parse(memberIDStr)
	if err != nil {
		memberID = uuid.Nil
	}

	notifSvc := service.NewNotificationService(h.Svc)
	notifications, err := notifSvc.ListNotifications(r.Context(), workspaceID, memberID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, notifications)
}
