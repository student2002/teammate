// review.go 提供审查队列查询和自我审查检测等 HTTP API 端点。
//
// 本文件提供以下 HTTP API 端点：
//   - GET /projects/{projectId}/review-queue: 获取项目的审查队列，返回待审查的节点列表
//   - GET /projects/{projectId}/review/nodes/{nodeId}/self-review-check: 检测审查节点是否存在自我审查冲突
//
// 审查队列由 ReviewService 查询，列出指定项目中所有待审查的节点。
// 自我审查检测用于判断审查者是否为前序节点的执行 Agent，避免自我审查回避机制违规。

package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// ReviewHandler 处理审查相关的 HTTP 请求，包括审查队列查询和自我审查检测。
type ReviewHandler struct {
	Svc *service.Service
}

// NewReviewHandler 创建 ReviewHandler 实例。
//
// 参数:
//   - svc: 业务逻辑服务实例，提供审查队列和自我审查检测能力
//
// 返回:
//   - *ReviewHandler: 审查处理器实例
func NewReviewHandler(svc *service.Service) *ReviewHandler {
	return &ReviewHandler{Svc: svc}
}

// Routes 返回审查相关的路由表。
//
// 返回:
//   - chi.Router: 包含审查队列查询和自我审查检测端点的路由
func (h *ReviewHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/review-queue", h.GetReviewQueue)
	r.Get("/nodes/{nodeId}/self-review-check", h.CheckSelfReview)

	return r
}

// GetReviewQueue 处理 GET /projects/{projectId}/review-queue 端点，获取项目的审查队列，返回待审查的节点列表。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 projectId 为项目 UUID
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含待审查节点列表或错误信息
func (h *ReviewHandler) GetReviewQueue(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	reviewSvc := service.NewReviewService(h.Svc)
	items, err := reviewSvc.GetReviewQueue(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, items)
}

// CheckSelfReview 处理 GET /projects/{projectId}/review/nodes/{nodeId}/self-review-check 端点，检测审查节点是否存在自我审查冲突。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 taskId 为任务 ID，nodeId 为节点 UUID
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含自我审查检测结果或错误信息
func (h *ReviewHandler) CheckSelfReview(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskId")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}
	nodeID, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	reviewSvc := service.NewReviewService(h.Svc)
	result, err := reviewSvc.CheckSelfReview(r.Context(), taskID, nodeID)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			response.NotFound(w, errMsg)
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, result)
}
