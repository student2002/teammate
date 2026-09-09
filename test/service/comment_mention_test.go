// comment_mention_test.go covers the publish logic of comment @mention triggering SSE events (mention:trigger).
package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// recordingHub records all Publish calls, used to verify SSE event publish targets and payloads.
type recordingHub struct {
	mu          sync.Mutex
	subscribers []string
	events      []types.SSEEvent
}

func (h *recordingHub) Publish(_ context.Context, subscriberID string, event types.SSEEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribers = append(h.subscribers, subscriberID)
	h.events = append(h.events, event)
	return nil
}

func (h *recordingHub) BufferEvent(_ context.Context, _ string, _ types.SSEEvent) error {
	return nil
}

func (h *recordingHub) recorded() ([]string, []types.SSEEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := append([]string(nil), h.subscribers...)
	events := append([]types.SSEEvent(nil), h.events...)
	return subs, events
}

// createOnlineRuntime creates an online runtime for the specified agent and returns its ID.
// publishToAgent delivers at runtime granularity (SSE connections subscribe by runtimeId),
// and only sends to online runtimes (offline ones are discarded), so tests need to simulate the online scenario.
func createOnlineRuntime(t *testing.T, svc *service.Service, agentID string) string {
	t.Helper()
	rt, err := svc.Store.CreateRuntime(context.Background(), types.CreateRuntimeParams{
		AgentID:  agentID,
		DaemonID: "test-daemon-" + uuid.NewString()[:8],
		Provider: "claude",
		Status:   "online",
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	return rt.ID
}

// TestCommentCreate_PublishesMentionTrigger verifies that when a comment is created,
// mention:trigger is only published to @mentioned Agents, non-Agent mentions (members/invalid IDs) are not published.
func TestCommentCreate_PublishesMentionTrigger(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	hub := &recordingHub{}
	svc.Hub = hub
	agent1RuntimeID := createOnlineRuntime(t, svc, env.agent1ID)

	commentSvc := service.NewCommentService(svc)
	_, err := commentSvc.Create(context.Background(), types.CreateCommentParams{
		TaskID:      env.taskID,
		AuthorType:  "member",
		AuthorID:    uuid.NewString(),
		Content:     "Please handle @agent1",
		CommentType: "text",
		Mentions:    []string{env.agent1ID, uuid.NewString()}, // agent1 + non-Agent (member/invalid)
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	subs, events := hub.recorded()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 mention:trigger event, got %d", len(events))
	}
	if events[0].Event != types.EventMentionTrigger {
		t.Fatalf("expected event %q, got %q", types.EventMentionTrigger, events[0].Event)
	}
	if len(subs) != 1 || subs[0] != agent1RuntimeID {
		t.Fatalf("expected publish to agent %s runtime %s, got %v", env.agent1ID, agent1RuntimeID, subs)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if payload["task_id"] != fmt.Sprintf("%d", env.taskID) {
		t.Errorf("payload task_id = %v, want %d", payload["task_id"], env.taskID)
	}
	if payload["comment_id"] == "" || payload["comment_id"] == nil {
		t.Errorf("payload comment_id missing: %v", payload)
	}
}

// TestCommentUpdate_PublishesOnlyNewMentions verifies that when a comment is edited,
// mention:trigger is only published for newly added @mentions, existing mentions are not re-published.
func TestCommentUpdate_PublishesOnlyNewMentions(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	hub := &recordingHub{}
	svc.Hub = hub
	agent1RuntimeID := createOnlineRuntime(t, svc, env.agent1ID)
	agent2RuntimeID := createOnlineRuntime(t, svc, env.agent2ID)

	commentSvc := service.NewCommentService(svc)
	comment, err := commentSvc.Create(context.Background(), types.CreateCommentParams{
		TaskID:      env.taskID,
		AuthorType:  "member",
		AuthorID:    uuid.NewString(),
		Content:     "@agent1 handle this",
		CommentType: "text",
		Mentions:    []string{env.agent1ID},
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// After initial creation, there should be only 1 publish
	subs, _ := hub.recorded()
	if len(subs) != 1 {
		t.Fatalf("expected 1 event after create, got %d", len(subs))
	}

	// Edit: add @agent2, keep @agent1 — only agent2 should be published
	_, err = commentSvc.Update(context.Background(), uuid.MustParse(comment.ID), "@agent1 @agent2 handle this",
		[]uuid.UUID{uuid.MustParse(env.agent1ID), uuid.MustParse(env.agent2ID)})
	if err != nil {
		t.Fatalf("update comment: %v", err)
	}

	subs, events := hub.recorded()
	if len(events) != 2 {
		t.Fatalf("expected 2 events total (1 create + 1 update), got %d", len(events))
	}
	if len(subs) != 2 || subs[0] != agent1RuntimeID || subs[1] != agent2RuntimeID {
		t.Fatalf("expected first event to agent1 runtime %s, second to agent2 runtime %s, got %v",
			agent1RuntimeID, agent2RuntimeID, subs)
	}
}
