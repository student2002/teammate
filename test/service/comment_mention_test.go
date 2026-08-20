// comment_mention_test.go 覆盖评论 @提及 触发 SSE 事件（mention:trigger）的发布逻辑。
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

// recordingHub 记录所有 Publish 调用，用于验证 SSE 事件发布的目标与载荷。
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

// createOnlineRuntime 为指定代理创建在线 runtime 并返回其 ID。
// publishToAgent 按 runtime 粒度投递（SSE 连接按 runtimeId 订阅），
// 且只发给在线 runtime（离线直接丢弃），测试需模拟在线场景。
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

// TestCommentCreate_PublishesMentionTrigger 验证创建评论时，
// 只向被 @提及 的 Agent 发布 mention:trigger，非 Agent 提及（成员/无效 ID）不发布。
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
		Content:     "请处理 @agent1",
		CommentType: "text",
		Mentions:    []string{env.agent1ID, uuid.NewString()}, // agent1 + 非 Agent（成员/无效）
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

// TestCommentUpdate_PublishesOnlyNewMentions 验证编辑评论时，
// 只对新增的 @提及 发布 mention:trigger，已存在的提及不重复发布。
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
		Content:     "@agent1 处理",
		CommentType: "text",
		Mentions:    []string{env.agent1ID},
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// 首次创建后应只有 1 次发布
	subs, _ := hub.recorded()
	if len(subs) != 1 {
		t.Fatalf("expected 1 event after create, got %d", len(subs))
	}

	// 编辑：新增 @agent2，保留 @agent1 —— 只应发布 agent2
	_, err = commentSvc.Update(context.Background(), uuid.MustParse(comment.ID), "@agent1 @agent2 处理",
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
