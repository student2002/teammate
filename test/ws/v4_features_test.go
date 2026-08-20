// v4_features_test.go 覆盖 WS 层 v4 特性的测试。
package ws_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/ws"
)

// TestIsControlEvent 验证五种控制事件类型能够被正确识别。
func TestIsControlEvent(t *testing.T) {
	controlEvents := []string{
		ws.EventTaskInterrupt,
		ws.EventNodeRejectRollback,
		ws.EventNodeTimeout,
		ws.EventSyncRequired,
		ws.EventPermissionChanged,
	}
	for _, evt := range controlEvents {
		if !ws.IsControlEvent(evt) {
			t.Errorf("IsControlEvent(%q) = false, want true", evt)
		}
	}

	nonControlEvents := []string{
		ws.EventNodePending,
		ws.EventNodeContinuationInvite,
		ws.EventMentionTrigger,
		"node:completed",
		"task:created",
	}
	for _, evt := range nonControlEvents {
		if ws.IsControlEvent(evt) {
			t.Errorf("IsControlEvent(%q) = true, want false", evt)
		}
	}
}

// TestControlEventPriorityDelivery 验证控制事件使用带超时的阻塞投递，而非控制事件使用尽最大努力投递。
// 该测试通过订阅客户端并验证两种事件类型均被接收，来测试 Publish 方法的行为。
func TestControlEventPriorityDelivery(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-ctrl-" + uuid.New().String()
	ctx := context.Background()

	// 清理 Redis 键
	t.Cleanup(func() {
		rdb.Del(ctx, ws.BufferKey(runtimeID))
		rdb.Del(ctx, ws.RedisChannel(runtimeID))
	})

	// 订阅一个客户端
	ch, unsub := hub.Subscribe(runtimeID)
	defer unsub()

	// 发布一个控制事件 (task:interrupt)
	controlEvent := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventTaskInterrupt,
		Data:  json.RawMessage(`{"task_id":1}`),
	}
	if err := hub.Publish(ctx, runtimeID, controlEvent); err != nil {
		t.Fatalf("publish control event: %v", err)
	}

	// 验证控制事件已被接收
	select {
	case evt := <-ch:
		if evt.Event != ws.EventTaskInterrupt {
			t.Errorf("received event type = %q, want %q", evt.Event, ws.EventTaskInterrupt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("control event not received within timeout")
	}

	// 发布一个非控制事件 (node:pending)
	nonControlEvent := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventNodePending,
		Data:  json.RawMessage(`{"node_id":"n1"}`),
	}
	if err := hub.Publish(ctx, runtimeID, nonControlEvent); err != nil {
		t.Fatalf("publish non-control event: %v", err)
	}

	// 验证非控制事件已被接收
	select {
	case evt := <-ch:
		if evt.Event != ws.EventNodePending {
			t.Errorf("received event type = %q, want %q", evt.Event, ws.EventNodePending)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("non-control event not received within timeout")
	}
}

// TestBufferEventForOfflineRuntime 验证事件可以为没有活跃订阅者的运行时进行缓冲，并在之后被检索。
func TestBufferEventForOfflineRuntime(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-offline-" + uuid.New().String()
	ctx := context.Background()

	t.Cleanup(func() { rdb.Del(ctx, ws.BufferKey(runtimeID)) })

	// 验证无订阅者（离线）
	if hub.ClientCount(runtimeID) != 0 {
		t.Errorf("expected 0 subscribers for offline runtime, got %d", hub.ClientCount(runtimeID))
	}

	// 为离线运行时缓冲事件
	event1 := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().Add(-1*time.Second).UnixNano()),
		Event: ws.EventTaskInterrupt,
		Data:  json.RawMessage(`{"task_id":1}`),
	}
	event2 := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventPermissionChanged,
		Data:  json.RawMessage(`{"agent_id":"a1","permission":"task:execute","action":"grant"}`),
	}

	if err := hub.BufferEvent(ctx, runtimeID, event1); err != nil {
		t.Fatalf("buffer event1: %v", err)
	}
	if err := hub.BufferEvent(ctx, runtimeID, event2); err != nil {
		t.Fatalf("buffer event2: %v", err)
	}

	// 验证事件已存储在 Redis 中
	count, err := rdb.ZCard(ctx, ws.BufferKey(runtimeID)).Result()
	if err != nil {
		t.Fatalf("zcard: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 buffered events, got %d", count)
	}

	// 随后，当运行时上线时，重放 event1.ID 之后的事件
	replayed, err := hub.GetBufferedEvents(ctx, runtimeID, event1.ID)
	if err != nil {
		t.Fatalf("get buffered events: %v", err)
	}
	if len(replayed) != 1 {
		t.Fatalf("expected 1 replayed event after event1, got %d", len(replayed))
	}
	if replayed[0].Event != ws.EventPermissionChanged {
		t.Errorf("replayed event type = %q, want %q", replayed[0].Event, ws.EventPermissionChanged)
	}

	// 重放所有事件（无 Last-Event-ID = 不重放）
	replayed, err = hub.GetBufferedEvents(ctx, runtimeID, "")
	if err != nil {
		t.Fatalf("get buffered events with empty ID: %v", err)
	}
	if len(replayed) != 0 {
		t.Errorf("expected 0 events with empty Last-Event-ID, got %d", len(replayed))
	}
}

// TestBufferEventTTL 验证缓冲的事件在 Redis 中设置了 TTL。
func TestBufferEventTTL(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-ttl-" + uuid.New().String()
	ctx := context.Background()

	t.Cleanup(func() { rdb.Del(ctx, ws.BufferKey(runtimeID)) })

	event := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventNodePending,
		Data:  json.RawMessage(`{"node_id":"n1"}`),
	}
	if err := hub.BufferEvent(ctx, runtimeID, event); err != nil {
		t.Fatalf("buffer event: %v", err)
	}

	ttl, err := rdb.TTL(ctx, ws.BufferKey(runtimeID)).Result()
	if err != nil {
		t.Fatalf("ttl: %v", err)
	}
	if ttl <= 0 || ttl > ws.BufferTTL {
		t.Errorf("TTL = %v, expected between 0 and %v", ttl, ws.BufferTTL)
	}
}
