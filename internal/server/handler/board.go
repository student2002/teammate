// board.go 提供项目看板数据查询的 HTTP API 端点，返回按列组织的任务数据。
//
// 本文件提供以下 HTTP API 端点：
//   - GET /projects/{projectId}/board: 获取项目看板数据，返回按列（pending/in_progress/completed/rejected/manual_intervention）组织的任务列表
//
// 看板数据由 BoardService 查询并按预定义列顺序排列，每列包含列标识、标签及对应任务列表。
// 请求路径参数 projectId 必须为合法的 UUID 格式，否则返回 400 Bad Request。

package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// BoardHandler 处理项目看板数据查询的 HTTP 请求。
type BoardHandler struct {
	Svc *service.Service
}

// NewBoardHandler 创建 BoardHandler 实例。
//
// 参数:
//   - svc: 业务逻辑服务实例，提供数据查询能力
//
// 返回:
//   - *BoardHandler: 看板处理器实例
func NewBoardHandler(svc *service.Service) *BoardHandler {
	return &BoardHandler{Svc: svc}
}

// Routes 返回看板的路由表。
//
// 返回:
//   - chi.Router: 包含看板相关端点的路由
func (h *BoardHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.GetBoardData)

	return r
}

// GetBoardData 处理 GET /projects/{projectId}/board 端点，返回按列组织的看板任务数据。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 projectId 为项目 UUID
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含按列组织的任务数据或错误信息
func (h *BoardHandler) GetBoardData(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	boardSvc := service.NewBoardService(h.Svc)
	columns, err := boardSvc.GetBoardData(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// 按定义的列顺序构建结果
	type boardColumn struct {
		Key   string                   `json:"key"`
		Label string                   `json:"label"`
		Tasks []service.BoardColumnTask `json:"tasks"`
	}

	result := struct {
		Columns []boardColumn `json:"columns"`
	}{
		Columns: make([]boardColumn, 0, len(columns)),
	}
	for _, col := range columns {
		result.Columns = append(result.Columns, boardColumn{
			Key:   col.Key,
			Label: col.Label,
			Tasks: col.Tasks,
		})
	}

	response.JSON(w, r, result)
}
