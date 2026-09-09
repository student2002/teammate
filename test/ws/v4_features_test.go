// v4_features_test.go covers tests for WS layer v4 features.
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

// TestIsControlEvent verifies that the five control event types are correctly identified.
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

// TestControlEventPriorityDelivery verifies that control events use blocking delivery with timeout,
// while non-control events use best-effort delivery.
// This tests the Publish method behavior by subscribing a client and verifying both event types are received.
func TestControlEventPriorityDelivery(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-ctrl-" + uuid.New().String()
	ctx := context.Background()

	// Clean up Redis keys
	t.Cleanup(func() {
		rdb.Del(ctx, ws.BufferKey(runtimeID))
		rdb.Del(ctx, ws.RedisChannel(runtimeID))
	})

	// Subscribe a client
	ch, unsub := hub.Subscribe(runtimeID)
	defer unsub()

	// Publish a control event (task:interrupt)
	controlEvent := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventTaskInterrupt,
		Data:  json.RawMessage(`{"task_id":1}`),
	}
	if err := hub.Publish(ctx, runtimeID, controlEvent); err != nil {
		t.Fatalf("publish control event: %v", err)
	}

	// Verify the control event was received
	select {
	case evt := <-ch:
		if evt.Event != ws.EventTaskInterrupt {
			t.Errorf("received event type = %q, want %q", evt.Event, ws.EventTaskInterrupt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("control event not received within timeout")
	}

	// Publish a non-control event (node:pending)
	nonControlEvent := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventNodePending,
		Data:  json.RawMessage(`{"node_id":"n1"}`),
	}
	if err := hub.Publish(ctx, runtimeID, nonControlEvent); err != nil {
		t.Fatalf("publish non-control event: %v", err)
	}

	// Verify the non-control event was received
	select {
	case evt := <-ch:
		if evt.Event != ws.EventNodePending {
			t.Errorf("received event type = %q, want %q", evt.Event, ws.EventNodePending)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("non-control event not received within timeout")
	}
}

// TestBufferEventForOfflineRuntime verifies that events can be buffered for a runtime with no active subscribers and retrieved later.
func TestBufferEventForOfflineRuntime(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-offline-" + uuid.New().String()
	ctx := context.Background()

	t.Cleanup(func() { rdb.Del(ctx, ws.BufferKey(runtimeID)) })

	// Verify no subscribers (offline)
	if hub.ClientCount(runtimeID) != 0 {
		t.Errorf("expected 0 subscribers for offline runtime, got %d", hub.ClientCount(runtimeID))
	}

	// Buffer events for offline runtime
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

	// Verify events are stored in Redis
	count, err := rdb.ZCard(ctx, ws.BufferKey(runtimeID)).Result()
	if err != nil {
		t.Fatalf("zcard: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 buffered events, got %d", count)
	}

	// Later, when the runtime comes online, replay events after event1.ID
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

	// Replay all events (no Last-Event-ID = no replay)
	replayed, err = hub.GetBufferedEvents(ctx, runtimeID, "")
	if err != nil {
		t.Fatalf("get buffered events with empty ID: %v", err)
	}
	if len(replayed) != 0 {
		t.Errorf("expected 0 events with empty Last-Event-ID, got %d", len(replayed))
	}
}

// TestBufferEventTTL verifies that buffered events have a TTL set in Redis.
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
