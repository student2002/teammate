// workflow_node_types.go exposes the server-side node catalog to clients
// (plan §12.1). The registry is the single source of truth for type keys,
// categories, executor kinds and port shapes.
package handler

import (
	"net/http"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// WorkflowNodeTypesHandler returns every registered node type definition.
// Any authenticated workspace member may read the catalog; it carries no
// workspace-scoped data.
func WorkflowNodeTypesHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, r, svc.WorkflowRegistry.List())
	}
}
