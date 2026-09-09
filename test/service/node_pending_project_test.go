// node_pending_project_test.go covers node:pending project-scoped broadcast: only delivered to project member Agents,
// non-project-member Agents do not receive node notifications (information isolation).
package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// TestCreateTask_PublishesNodePendingOnlyToProjectMembers verifies that when a task is created,
// node:pending is only sent to online runtimes of project member Agents; non-project-member Agents do not receive the broadcast.
func TestCreateTask_PublishesNodePendingOnlyToProjectMembers(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	hub := &recordingHub{}
	svc.Hub = hub

	// agent1 and agent2 are project members (setupServiceTest already added them to the project)
	agent1Runtime := createOnlineRuntime(t, svc, env.agent1ID)
	agent2Runtime := createOnlineRuntime(t, svc, env.agent2ID)

	// Create a non-project-member agent3 (same workspace, but not belonging to this project)
	agent3, _, err := svc.Store.CreateAgent(context.Background(), types.CreateAgentParams{
		WorkspaceID:  env.workspaceID,
		Name:         "agent3-" + uuid.NewString()[:8],
		Provider:     "claude",
		Instructions: "non-member agent",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       "offline",
		GitName:      strPtr("agent3"),
		GitEmail:     strPtr("agent3@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent3: %v", err)
	}
	agent3Runtime := createOnlineRuntime(t, svc, agent3.ID)

	// Create a task through the service layer (triggers publishNodePendingEvents project-scoped broadcast)
	taskSvc := service.NewTaskService(svc)
	desc := "node pending project test"
	_, err = taskSvc.Create(context.Background(), uuid.MustParse(env.projectID), types.CreateTaskParams{
		ProjectID:    env.projectID,
		WorkflowName: "flow",
		Title:        "Project scoped broadcast test",
		Description:  &desc,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     env.agent1ID,
		Sequence:     0,
	}, uuid.MustParse(env.templateID))
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Collect all node:pending event delivery targets
	subs, events := hub.recorded()
	var pendingSubs []string
	for i, e := range events {
		if e.Event == types.EventNodePending {
			pendingSubs = append(pendingSubs, subs[i])
		}
	}

	// Only runtimes of project members (agent1/agent2) should receive the broadcast
	expected := map[string]bool{agent1Runtime: true, agent2Runtime: true}
	if len(pendingSubs) != len(expected) {
		t.Fatalf("expected %d node:pending deliveries, got %d: %v", len(expected), len(pendingSubs), pendingSubs)
	}
	for _, sub := range pendingSubs {
		if !expected[sub] {
			t.Fatalf("unexpected delivery to runtime %s (non-member should not receive)", sub)
		}
	}
	if containsSub(pendingSubs, agent3Runtime) {
		t.Fatalf("non-member agent3 runtime %s received node:pending — broadcast must be project-scoped", agent3Runtime)
	}
}

func containsSub(subs []string, target string) bool {
	for _, s := range subs {
		if s == target {
			return true
		}
	}
	return false
}
