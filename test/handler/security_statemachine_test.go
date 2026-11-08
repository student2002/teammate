// security_statemachine_test.go covers security validation of task-scoped log
// endpoints. The V1 node state machine security tests (claim/approve/reject/
// manual/resolve ordering) were removed with the V1 workflow model; they are
// rebuilt together with the V2 run engine (S3/S4).
package handler_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTaskLogsRejectsInvalidNodeIDFilter(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID, _ := createV2WorkflowTemplate(t, client, srv.URL, wsID, token)
	taskID := createTask(t, client, srv.URL, projID, tplID, token)

	logsURL := fmt.Sprintf("%s/api/tasks/%d/logs?node_id=not-a-uuid", srv.URL, taskID)
	_, status, _ := doRequestWithToken(t, client, http.MethodGet, logsURL, token, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("get logs with invalid node_id: expected 400, got %d", status)
	}
}
