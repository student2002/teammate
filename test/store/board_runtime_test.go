// board_runtime_test.go tests for board data access and daemon-reported agent discovery.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

func TestGetBoardData_ColumnMapping(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	_, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Board task",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	columns, err := s.GetBoardData(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("GetBoardData: %v", err)
	}
	if len(columns) == 0 {
		t.Fatal("expected board columns")
	}
}

// TestDaemonAgentPendingDiscovery verifies the daemon-first discovery path:
// a daemon registers reporting the agent, and the agent's pending nodes are
// discoverable through the board (the watcher's poll source).
func TestDaemonAgentPendingDiscovery(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	// Create a task with pending nodes
	_, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Pending task",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Daemon registers reporting the agent
	daemon, _, err := s.CreateDaemon(ctx, uuid.MustParse(ws.ID), "discovery-daemon")
	if err != nil {
		t.Fatalf("CreateDaemon: %v", err)
	}
	reported, err := s.UpsertDaemonAgent(ctx, uuid.MustParse(ws.ID), daemon.ID, agent.Name, agent.Provider)
	if err != nil {
		t.Fatalf("UpsertDaemonAgent: %v", err)
	}
	if reported.DaemonID != daemon.ID.String() {
		t.Fatalf("expected agent bound to daemon %s, got %q", daemon.ID, reported.DaemonID)
	}
	if reported.Status != types.AgentStatusOnline {
		t.Fatalf("expected reported agent online, got %q", reported.Status)
	}

	// Board still exposes the pending node for the watcher to discover
	columns, err := s.GetBoardData(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("GetBoardData: %v", err)
	}
	pending := 0
	for _, col := range columns {
		if col.Key == "pending" {
			pending = len(col.Tasks)
		}
	}
	if pending == 0 {
		t.Fatal("expected at least 1 pending task on the board")
	}
}

// TestDaemonAgentMentionComments verifies mention comments remain retrievable
// for a daemon-reported agent (execution context injection source).
func TestDaemonAgentMentionComments(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Comment task",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Create a comment mentioning the agent
	_, err = s.CreateComment(ctx, types.CreateCommentParams{
		TaskID:      task.ID,
		AuthorType:  "member",
		AuthorID:    uuid.New().String(),
		Content:     "Hey @" + agent.ID + " check this",
		CommentType: "text",
		Mentions:    []string{agent.ID},
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	// Daemon registers reporting the agent
	daemon, _, err := s.CreateDaemon(ctx, uuid.MustParse(ws.ID), "mention-daemon")
	if err != nil {
		t.Fatalf("CreateDaemon: %v", err)
	}
	if _, err := s.UpsertDaemonAgent(ctx, uuid.MustParse(ws.ID), daemon.ID, agent.Name, agent.Provider); err != nil {
		t.Fatalf("UpsertDaemonAgent: %v", err)
	}

	comments, err := s.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	found := false
	for _, c := range comments {
		for _, m := range c.Mentions {
			if m == agent.ID {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected at least 1 mention comment for the daemon-reported agent")
	}
}

var _ = store.Store{}
