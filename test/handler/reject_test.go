// reject_test.go 覆盖节点驳回接口的测试。
package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	dbgen "github.com/teammate/server/internal/db/generated"
)

// TestRejectCascading 验证驳回节点会级联回退：
// - 驳回节点 2 指向节点 1 → 节点 1 回到 pending，节点 2 标记为 rejected
// - 驳回计数递增
func TestRejectCascading(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// 准备：项目、包含 3 个节点（code -> review -> deploy）的工作流

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// 创建包含 3 个节点的任务
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	// 注意：跳过 deleteTask 清理，因为拒绝操作可能导致无法删除

	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Agent1 认领并批准节点 1（code）
	claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	approveNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)

	// Agent2 认领节点 2（review）——使用不同 Agent 以避免自我审查
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)

	// 拒绝 node 2 并指向 node 1
	status, rejectedNode := rejectNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token, &node1ID)
	if status != 200 {
		t.Fatalf("rejectNode: expected 200, got %d", status)
	}
	if rejectedNode["status"] != "rejected" {
		t.Fatalf("node2: expected status 'rejected' after reject, got %v", rejectedNode["status"])
	}

	// 验证拒绝次数已递增
	rejectCount, ok := rejectedNode["reject_count"].(float64)
	if !ok {
		t.Fatalf("node2: reject_count is not a number, got %v", rejectedNode["reject_count"])
	}
	if rejectCount != 1 {
		t.Fatalf("node2: expected reject_count 1, got %v", rejectCount)
	}
	t.Logf("node2 reject_count: %v", rejectCount)

	// 通过数据库查询验证节点 1 回到 in_progress
	q = dbQueries(t, db)
	node1UUID := parseUUID(t, node1ID)
	node1, err := q.GetTaskNode(t.Context(), node1UUID)
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if node1.Status != "pending" {
		t.Fatalf("node1: expected status 'pending' after reject targeting it, got %v", node1.Status)
	}
	t.Log("node1 is back to pending after reject targeting it (needs re-claim)")
}

// TestRejectToManualNode 验证驳回至人工/手动节点返回错误
func TestRejectToManualNode(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)

	// 创建第一个节点为 manual、第二个节点为 standard 的工作流
	q = dbQueries(t, db)

	wsUUID := parseUUID(t, wsID)
	tpl, err := q.CreateWorkflowTemplate(t.Context(), dbgen.CreateWorkflowTemplateParams{
		WorkspaceID:    wsUUID,
		Name:           "manual-first-flow",
		Description:    sql.NullString{String: "manual first node flow", Valid: true},
		TriggerType:    dbgen.WorkflowTriggerTypeManual,
		TriggerConfig:  json.RawMessage(`{}`),
		TriggerEnabled: true,
	})
	if err != nil {
		t.Fatalf("create workflow template: %v", err)
	}

	// 创建 manual 类型的第一个节点
	_, err = q.CreateTemplateNode(t.Context(), dbgen.CreateTemplateNodeParams{
		TemplateID:      tpl.ID,
		Name:            "manual-review",
		Description:     sql.NullString{String: "manual review gate", Valid: true},
		SortOrder:       1,
		NodeType:        dbgen.NodeTypeManual,
		AssigneeType:    dbgen.AssigneeTypeHuman,
		AssigneeID:      uuid.NullUUID{},
		TimeoutMinutes:  60,
		ReadonlyDirs:    pqtype.NullRawMessage{},
		FullControlDirs: pqtype.NullRawMessage{},
		Artifact:        pqtype.NullRawMessage{},
	})
	if err != nil {
		t.Fatalf("create manual template node: %v", err)
	}

	// 创建 standard 类型的第二个节点
	_, err = q.CreateTemplateNode(t.Context(), dbgen.CreateTemplateNodeParams{
		TemplateID:      tpl.ID,
		Name:            "code",
		Description:     sql.NullString{String: "write code", Valid: true},
		SortOrder:       2,
		NodeType:        dbgen.NodeTypeStandard,
		AssigneeType:    dbgen.AssigneeTypeAnyAgent,
		AssigneeID:      uuid.NullUUID{},
		TimeoutMinutes:  60,
		ReadonlyDirs:    pqtype.NullRawMessage{},
		FullControlDirs: pqtype.NullRawMessage{},
		Artifact:        pqtype.NullRawMessage{},
	})
	if err != nil {
		t.Fatalf("create standard template node: %v", err)
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
		DefaultWorkflowID: uuid.NullUUID{UUID: tpl.ID, Valid: true},
		MaxReviewCycles:   sql.NullInt32{Int32: 3, Valid: true},
	})

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// 创建任务
	taskID, nodes := createTask(t, client, srv.URL, projID, tpl.ID.String(), token)
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	if len(nodes) < 2 {
		t.Fatalf("expected at least 2 nodes, got %d", len(nodes))
	}

	manualNodeID := nodes[0]["id"].(string)
	codeNodeID := nodes[1]["id"].(string)

	// 认领 code 节点
	claimNode(t, client, srv.URL, taskID, codeNodeID, agentID, agentToken)

	// 拒绝并指向 manual 节点——按设计规范应被拒绝
	// （不能拒绝到 manual 或人工分配的节点）
	status, _ := rejectNode(t, client, srv.URL, taskID, codeNodeID, agentID, agentToken, &manualNodeID)
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request when rejecting to manual node, got %d", status)
	}
	t.Logf("reject to manual node correctly rejected with status: %d", status)
}

func parseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse UUID %q: %v", s, err)
	}
	return id
}
