// helpers_test.go 覆盖 store 层辅助函数的测试。
package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"

	"github.com/teammate/server/internal/clock"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

// TestMain 设置测试数据库和全局连接。
func TestMain(m *testing.M) {
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test database: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

func getTestDSN() string {
	return testdb.GetTestDSN()
}

func connectTestDB(t *testing.T) *sql.DB {
	t.Helper()
	testDB, err := sql.Open("pgx", getTestDSN())
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	if err := testDB.Ping(); err != nil {
		testDB.Close()
		t.Skipf("database not available, skipping: %v", err)
	}
	return testDB
}

// testDB 保存当前测试数据库连接，由 setupTestStore 设置。
// 由辅助函数（createTestWorkspace、createTestMember）用于注册清理操作。
var testDB *sql.DB

func setupTestStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	testDB = connectTestDB(t)
	t.Cleanup(func() { testDB.Close() })
	return store.New(testDB), testDB
}

func setupTestStoreWithClock(t *testing.T, c clock.Clock) (*store.Store, *sql.DB) {
	t.Helper()
	testDB = connectTestDB(t)
	t.Cleanup(func() { testDB.Close() })
	return store.NewWithClock(testDB, c), testDB
}

func createTestWorkspace(t *testing.T, s *store.Store) types.Workspace {
	t.Helper()
	ws, err := s.CreateWorkspace(context.Background(), types.CreateWorkspaceParams{
		Name:        "ws-" + uuid.New().String()[:8],
		Description: strPtr("test workspace"),
		IssuePrefix: "TST",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		if testDB != nil {
			_ = testdb.DeleteWorkspace(testDB, ws.ID)
		}
	})
	return ws
}

func createTestProject(t *testing.T, s *store.Store, workspaceID string) types.Project {
	t.Helper()
	proj, err := s.CreateProject(context.Background(), types.CreateProjectParams{
		WorkspaceID: workspaceID,
		Name:        "proj-" + uuid.New().String()[:8],
		Description: strPtr("test project"),
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return proj
}

func createTestAgent(t *testing.T, s *store.Store, workspaceID string) (types.Agent, string) {
	t.Helper()
	model := "claude-3.5-sonnet"
	agent, apiToken, err := s.CreateAgent(context.Background(), types.CreateAgentParams{
		WorkspaceID:  workspaceID,
		Name:         "agent-" + uuid.New().String()[:8],
		Provider:     "claude",
		Instructions: "test agent",
		Model:        &model,
		Status:       "offline",
		ExtraArgs:    []string{},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return agent, apiToken
}

func createTestWorkflowTemplate(t *testing.T, s *store.Store, workspaceID string, nodeCount int) (types.WorkflowTemplate, []types.WorkflowTemplateNode) {
	t.Helper()
	params := types.CreateWorkflowTemplateParams{
		WorkspaceID: workspaceID,
		Name:        "flow-" + uuid.New().String()[:8],
		Description: strPtr("test workflow"),
	}
	nodes := make([]types.CreateTemplateNodeParams, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodeType := "standard"
		if i == 1 && nodeCount > 2 {
			nodeType = "review"
		}
		desc := fmt.Sprintf("node %d description", i+1)
		nodes[i] = types.CreateTemplateNodeParams{
			Name:            fmt.Sprintf("node-%d", i+1),
			Description:     &desc,
			SortOrder:       int32(i + 1),
			NodeType:        nodeType,
			AssigneeType:    "any_agent",
			TimeoutMinutes:  60,
			MaxRejectCycles: 3,
		}
	}
	tpl, createdNodes, err := s.CreateWorkflowTemplate(context.Background(), params, nodes)
	if err != nil {
		t.Fatalf("create workflow template: %v", err)
	}
	return tpl, createdNodes
}

const testMemberPassword = "Test123456"

func createTestMember(t *testing.T, s *store.Store, workspaceID string) types.Member {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(testMemberPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	member, err := s.CreateMember(context.Background(), types.CreateMemberParams{
		Name:  "member-" + uuid.New().String()[:8],
		Email: "member-" + uuid.New().String()[:8] + "@test.com",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	// 以 member 角色将成员添加到工作区
	_, err = s.CreateWorkspaceMember(context.Background(), types.CreateWorkspaceMemberParams{
		WorkspaceID: workspaceID,
		MemberID:    member.ID,
		Role:        "member",
	})
	if err != nil {
		t.Fatalf("create workspace member: %v", err)
	}
	err = s.UpdateMemberPasswordHash(context.Background(), types.UpdateMemberPasswordHashParams{
		ID:           member.ID,
		PasswordHash: string(hash),
	})
	if err != nil {
		t.Fatalf("update member password: %v", err)
	}
	t.Cleanup(func() {
		if testDB != nil {
			_ = testdb.DeleteMember(testDB, member.ID)
		}
	})
	return member
}

func addAgentToProject(t *testing.T, s *store.Store, projectID, agentID string) {
	t.Helper()
	_, err := s.CreateProjectMember(context.Background(), types.CreateProjectMemberParams{
		ProjectID:  projectID,
		MemberType: "agent",
		AgentID:    &agentID,
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("add agent to project: %v", err)
	}
}

// strPtr 返回字符串指针，用于 types 参数中的 *string 字段。
func strPtr(s string) *string {
	return &s
}
