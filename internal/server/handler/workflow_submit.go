// workflow_submit.go provides the human.input completion endpoint (plan
// §12.5): POST /tasks/{taskId}/nodes/{nodeId}/submit closes a waiting
// human.input node with the operator's submitted/cancelled choice and
// advances the graph. Task-write permission applies: human members and
// above; agents are rejected.
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// submitHumanInputRequest is the body of the human.input submit endpoint.
type submitHumanInputRequest struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data"`
}

// SubmitHumanInput handles POST /tasks/{taskId}/nodes/{nodeId}/submit.
//
// Response:
//   - 200: the node closed with the choice and the graph advanced
//   - 400: invalid action (only submitted/cancelled are accepted)
//   - 401/403: authentication/permission (agents and viewers are rejected)
//   - 404: node not found on the addressed task
//   - 409: the node is not a waiting human.input node
func (h *NodeExecutionHandler) SubmitHumanInput(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}
	task, ok := taskFromContext(r)
	if !ok {
		response.NotFound(w, "task not found")
		return
	}

	nodeID, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	var req submitHumanInputRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if req.Action != "submitted" && req.Action != "cancelled" {
		response.BadRequest(w, fmt.Sprintf("unsupported action %q: only submitted/cancelled", req.Action))
		return
	}

	if err := service.NewWorkflowRunService(h.Svc).SubmitHumanInput(r.Context(), task.ID, nodeID, req.Action, req.Data); err != nil {
		writeNodeExecError(w, err)
		return
	}
	response.JSON(w, r, map[string]string{
		"node_id": nodeID.String(),
		"choice":  req.Action,
		"status":  "succeeded",
	})
}
