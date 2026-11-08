// service_test.go covers tests for Service layer core logic.
package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

// strPtr returns a string pointer, used for *string fields in types domain structs.
func strPtr(s string) *string {
	return &s
}

// TestMain sets up the test database.
func TestMain(m *testing.M) {
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test database: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

// svcGetTestDSN returns the database connection string for service tests.
func svcGetTestDSN() string {
	return testdb.GetTestDSN()
}

// svcConnectTestDB connects to the test database and returns the connection.
func svcConnectTestDB(t *testing.T) *sql.DB {
	t.Helper()
	testDB, err := sql.Open("pgx", svcGetTestDSN())
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	if err := testDB.Ping(); err != nil {
		testDB.Close()
		t.Skipf("database not available, skipping: %v", err)
	}
	return testDB
}

// setupServiceTest creates a complete test environment: workspace, project,
// V2 workflow template, agents, and a task main record.
// Returns the service instance, database connection, and all test-referenced IDs.
func setupServiceTest(t *testing.T) (*service.Service, *sql.DB, *testEnv) {
	t.Helper()
	pgDB := svcConnectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	// Create workspace
	ws, err := svc.Store.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "svc-test-" + uuid.New().String()[:8],
		Description: strPtr("service test"),
		IssuePrefix: "ST",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_ = testdb.DeleteWorkspace(pgDB, ws.ID)
	})

	// Create a member for granting permissions
	member, err := svc.Store.CreateMember(ctx, types.CreateMemberParams{
		Name:  "test-granter",
		Email: "granter-" + uuid.New().String()[:8] + "@test.local",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		_ = testdb.DeleteMember(pgDB, member.ID)
	})

	// Add member to workspace as admin
	_, err = svc.Store.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "admin",
	})
	if err != nil {
		t.Fatalf("create workspace member: %v", err)
	}

	// Create project
	proj, err := svc.Store.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "proj-" + uuid.New().String()[:8],
		Description: strPtr("test project"),
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Create a V2 workflow template: trigger.manual → agent.task → flow.end
	// chained by port-based edges
	node1ID := uuid.NewString()
	node2ID := uuid.NewString()
	node3ID := uuid.NewString()
	tpl, _, _, err := svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: ws.ID,
		Name:        "flow-" + uuid.New().String()[:8],
		Description: strPtr("3-node test flow"),
	}, []types.CreateTemplateNodeParams{
		{ID: &node1ID, Name: "start", SortOrder: 1, TypeKey: "trigger.manual", Config: []byte(`{}`), UIConfig: []byte(`{}`)},
		{ID: &node2ID, Name: "work", SortOrder: 2, TypeKey: "agent.task", Config: []byte(`{}`), UIConfig: []byte(`{}`)},
		{ID: &node3ID, Name: "done", SortOrder: 3, TypeKey: "flow.end", Config: []byte(`{}`), UIConfig: []byte(`{}`)},
	}, []types.CreateTemplateEdgeParams{
		{SourceNodeID: node1ID, SourcePort: "success", TargetNodeID: node2ID, TargetPort: "input"},
		{SourceNodeID: node2ID, SourcePort: "success", TargetNodeID: node3ID, TargetPort: "completed"},
	})
	if err != nil {
		t.Fatalf("create workflow template: %v", err)
	}

	// Create agents
	agent1Daemon, _, err := svc.Store.CreateDaemon(ctx, uuid.MustParse(ws.ID), "box-agent1")
	if err != nil {
		t.Fatalf("create agent daemon: %v", err)
	}
	agent1, err := svc.Store.CreateDaemonAgent(ctx, uuid.MustParse(ws.ID), agent1Daemon.ID, "agent1-" + uuid.NewString()[:8], "claude", "", "persona-agent1", "Tester", "tester@teammate.local")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	agent2Daemon, _, err := svc.Store.CreateDaemon(ctx, uuid.MustParse(ws.ID), "box-agent2")
	if err != nil {
		t.Fatalf("create agent daemon: %v", err)
	}
	agent2, err := svc.Store.CreateDaemonAgent(ctx, uuid.MustParse(ws.ID), agent2Daemon.ID, "agent2-" + uuid.NewString()[:8], "claude", "", "persona-agent2", "Tester", "tester@teammate.local")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Add agents to project and grant default permissions
	for _, aid := range []string{agent1.ID, agent2.ID} {
		_, err = svc.Store.CreateProjectMember(ctx, types.CreateProjectMemberParams{
			ProjectID:  proj.ID,
			MemberType: "agent",
			AgentID:    &aid,
			Role:       "member",
		})
		if err != nil {
			t.Fatalf("add agent to project: %v", err)
		}
		if err := svc.Store.GrantDefaultPermissions(ctx, uuid.MustParse(aid), uuid.MustParse(member.ID)); err != nil {
			t.Fatalf("grant default permissions for agent %s: %v", aid, err)
		}
	}

	// Create the task main record. V2 task creation does not instantiate
	// template nodes; the workflow engine owns that (S3).
	tplIDStr := tpl.ID
	task, err := svc.Store.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:          proj.ID,
		WorkflowName:       tpl.Name,
		WorkflowTemplateID: &tplIDStr,
		Title:              "Service test task",
		Description:        strPtr("test"),
		Type:               "task",
		Priority:           "medium",
		Status:             "active",
		AuthorType:         "agent",
		AuthorID:           agent1.ID,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	env := &testEnv{
		svc:         svc,
		workspaceID: ws.ID,
		projectID:   proj.ID,
		templateID:  tpl.ID,
		taskID:      task.ID,
		agent1ID:    agent1.ID,
		agent2ID:    agent2.ID,
	}

	return svc, pgDB, env
}

type testEnv struct {
	svc         *service.Service
	workspaceID string
	projectID   string
	templateID  string
	taskID      int32
	agent1ID    string
	agent2ID    string
}

// TestTemplateStatsUsageCountAndCompletion verifies that TemplateStatsService.
// GetStats aggregates by template id and returns usage_count /
// avg_completion_seconds for a template with one task main record.
func TestTemplateStatsUsageCountAndCompletion(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// setupServiceTest already created 1 task with this template (env.taskID), so usage_count >= 1.
	statsSvc := service.NewTemplateStatsService(svc)
	stats, err := statsSvc.GetStats(ctx, uuid.MustParse(env.templateID))
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if stats.UsageCount < 1 {
		t.Fatalf("expected usage_count >= 1, got %d", stats.UsageCount)
	}
	// The task was just created and not completed, so avg_completion_seconds should at least be non-negative
	if stats.AvgCompletionSeconds < 0 {
		t.Fatalf("expected avg_completion_seconds >= 0, got %v", stats.AvgCompletionSeconds)
	}
	t.Logf("template stats: usage_count=%d, avg_completion_seconds=%.2f",
		stats.UsageCount, stats.AvgCompletionSeconds)
}
