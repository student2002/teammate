// autostart_test.go 覆盖自动启动接口的测试。
package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	dbgen "github.com/teammate/server/internal/db/generated"
)

// TestAutoStartFirstNode 验证 CreateTask 在 assignee_type 为 'auto' 时自动启动第一个节点，为 'any_agent' 时保持为 'pending'。
func TestAutoStartFirstNode(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// 测试 1：第一个 assignee_type='auto' 的节点应以 'in_progress' 启动
	projID := createProject(t, client, srv.URL, wsID, token)

	// 创建一个第一个节点 assignee_type='auto' 的工作流模板
	// 我们需要通过数据库创建它，因为 API 不易支持 'auto' 类型
	q := dbQueries(t, db)
	wsUUID := parseUUID(t, wsID)

	autoTpl, err := q.CreateWorkflowTemplate(t.Context(), dbgen.CreateWorkflowTemplateParams{
		WorkspaceID:    wsUUID,
		Name:           "auto-start-flow",
		Description:    sql.NullString{String: "auto start first node", Valid: true},
		TriggerType:    dbgen.WorkflowTriggerTypeManual,
		TriggerConfig:  json.RawMessage(`{}`),
		TriggerEnabled: true,
	})
	if err != nil {
		t.Fatalf("create auto workflow template: %v", err)
	}

	// 创建自动类型的第一个节点
	_, err = q.CreateTemplateNode(t.Context(), dbgen.CreateTemplateNodeParams{
		TemplateID:      autoTpl.ID,
		Name:            "auto-code",
		Description:     sql.NullString{String: "auto code node", Valid: true},
		SortOrder:       1,
		NodeType:        dbgen.NodeTypeStandard,
		AssigneeType:    dbgen.AssigneeTypeAuto,
		AssigneeID:      uuid.NullUUID{},
		TimeoutMinutes:  60,
		ReadonlyDirs:    pqtype.NullRawMessage{},
		FullControlDirs: pqtype.NullRawMessage{},
		Artifact:        pqtype.NullRawMessage{},
	})
	if err != nil {
		t.Fatalf("create auto template node: %v", err)
	}

	// 创建 standard 类型的第二个节点
	_, err = q.CreateTemplateNode(t.Context(), dbgen.CreateTemplateNodeParams{
		TemplateID:      autoTpl.ID,
		Name:            "review",
		Description:     sql.NullString{String: "review node", Valid: true},
		SortOrder:       2,
		NodeType:        dbgen.NodeTypeReview,
		AssigneeType:    dbgen.AssigneeTypeAnyAgent,
		AssigneeID:      uuid.NullUUID{},
		TimeoutMinutes:  30,
		ReadonlyDirs:    pqtype.NullRawMessage{},
		FullControlDirs: pqtype.NullRawMessage{},
		Artifact:        pqtype.NullRawMessage{},
	})
	if err != nil {
		t.Fatalf("create review template node: %v", err)
	}

	// 设置
	projUUID := parseUUID(t, projID)
	_, _ = q.UpdateProject(t.Context(), dbgen.UpdateProjectParams{
		ID:                projUUID,
		Name:              "test-project",
		Description:       sql.NullString{String: "test", Valid: true},
		Status:            dbgen.ProjectStatusActive,
		RepoUrl:           sql.NullString{},
		Context:           sql.NullString{},
		DefaultWorkflowID: uuid.NullUUID{UUID: autoTpl.ID, Valid: true},
		MaxReviewCycles:   sql.NullInt32{Int32: 3, Valid: true},
	})

	// 创建任务 → 验证第一个节点是 'in_progress' 而不是 'pending'
	taskID, nodes := createTask(t, client, srv.URL, projID, autoTpl.ID.String(), token)
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	if len(nodes) < 1 {
		t.Fatal("expected at least 1 node")
	}

	firstNode := nodes[0]
	if firstNode["status"] != "in_progress" {
		t.Fatalf("auto first node: expected status 'in_progress', got %v", firstNode["status"])
	}
	t.Logf("Auto first node status: %v (correctly auto-started)", firstNode["status"])

	// 测试 2：第一个 assignee_type='any_agent' 的节点应为 'pending'
	// 注册第二个用户以获得独立的工作区
	token2, wsID2 := registerTestUser(t, client, srv.URL)

	projID2 := createProject(t, client, srv.URL, wsID2, token2)
	tplID2 := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID2, token2)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID2, projID2, tplID2, token2)

	// 创建任务 → 验证第一个节点是 'pending'
	taskID2, nodes2 := createTask(t, client, srv.URL, projID2, tplID2, token2)
	defer deleteTask(t, client, srv.URL, projID2, taskID2, token2)

	if len(nodes2) < 1 {
		t.Fatal("expected at least 1 node")
	}

	firstNode2 := nodes2[0]
	if firstNode2["status"] != "pending" {
		t.Fatalf("any_agent first node: expected status 'pending', got %v", firstNode2["status"])
	}
	t.Logf("Any_agent first node status: %v (correctly stays pending)", firstNode2["status"])
}
