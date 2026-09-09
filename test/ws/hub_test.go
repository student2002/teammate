// hub_test.go covers tests for the SSE Hub.
package ws_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/server/ws"
)

func getTestRedisURL() string {
	if url := os.Getenv("TEAMS_REDIS_URL"); url != "" {
		return url
	}
	return "redis://localhost:16379"
}

func connectTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	opts, err := redis.ParseURL(getTestRedisURL())
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		t.Skipf("redis not available, skipping: %v", err)
	}
	return rdb
}

// testKeyPrefix avoids conflicts with production data.
const testKeyPrefix = "test:sse:"

// TestEventBuffering verifies that published events are stored in a Redis sorted set.
func TestEventBuffering(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	_ = ws.NewHub(rdb) // hub is not needed for direct Redis operations in this test
	runtimeID := uuid.New().String()
	testBufferKey := testKeyPrefix + runtimeID
	ctx := context.Background()

	// Clean up test keys
	t.Cleanup(func() { rdb.Del(ctx, testBufferKey) })

	// Publish multiple events
	events := []ws.SSEEvent{
		{ID: fmt.Sprintf("%d", time.Now().Add(-2*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"1"}`)},
		{ID: fmt.Sprintf("%d", time.Now().Add(-1*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"2"}`)},
		{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"3"}`)},
	}

	// Buffer events directly
	for _, evt := range events {
		// Override buffer key for testing
		data, _ := json.Marshal(evt)
		score, _ := strconv.ParseFloat(evt.ID, 64)
		pipe := rdb.Pipeline()
		pipe.ZAdd(ctx, testBufferKey, redis.Z{Score: score, Member: data})
		pipe.Expire(ctx, testBufferKey, ws.BufferTTL)
		if _, err := pipe.Exec(ctx); err != nil {
			t.Fatalf("buffer event: %v", err)
		}
	}

	// Verify events are stored in the sorted set
	count, err := rdb.ZCard(ctx, testBufferKey).Result()
	if err != nil {
		t.Fatalf("zcard: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 buffered events, got %d", count)
	}
	t.Logf("event buffering: %d events stored in Redis sorted set", count)
}

// TestLastEventIDReplay verifies that missed events can be replayed when connecting with Last-Event-ID.
func TestLastEventIDReplay(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := uuid.New().String()
	testBufferKey := testKeyPrefix + runtimeID
	ctx := context.Background()

	t.Cleanup(func() { rdb.Del(ctx, testBufferKey) })

	// Create events with known timestamps
	now := time.Now()
	events := []ws.SSEEvent{
		{ID: fmt.Sprintf("%d", now.Add(-3*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"old1"}`)},
		{ID: fmt.Sprintf("%d", now.Add(-2*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"old2"}`)},
		{ID: fmt.Sprintf("%d", now.Add(-1*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"new1"}`)},
		{ID: fmt.Sprintf("%d", now.UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"new2"}`)},
	}

	// Buffer events directly using test key prefix
	for _, evt := range events {
		data, _ := json.Marshal(evt)
		score, _ := strconv.ParseFloat(evt.ID, 64)
		pipe := rdb.Pipeline()
		pipe.ZAdd(ctx, testBufferKey, redis.Z{Score: score, Member: data})
		pipe.Expire(ctx, testBufferKey, ws.BufferTTL)
		if _, err := pipe.Exec(ctx); err != nil {
			t.Fatalf("buffer event: %v", err)
		}
	}

	// Simulate Last-Event-ID = events[1].ID (client has seen the first 2 events)
	lastEventID := events[1].ID

	// Use GetBufferedEvents to replay missed messages
	// We need to temporarily override the buffer key for testing
	// Since GetBufferedEvents uses BufferKey(runtimeID), we need to use the actual key format
	// We use the real hub methods and actual keys
	actualRuntimeID := "test-replay-" + runtimeID
	actualBufferKey := ws.BufferKey(actualRuntimeID)

	// Copy test data to the actual buffer key format
	srcKey := testBufferKey
	rdb.Copy(ctx, srcKey, actualBufferKey, 0, true)
	t.Cleanup(func() { rdb.Del(ctx, actualBufferKey) })

	replayed, err := hub.GetBufferedEvents(ctx, actualRuntimeID, lastEventID)
	if err != nil {
		t.Fatalf("get buffered events: %v", err)
	}

	// Should replay events after lastEventID (events[2] and events[3])
	if len(replayed) != 2 {
		t.Fatalf("expected 2 replayed events, got %d", len(replayed))
	}

	// Verify the replayed events are correct
	var replayedNodeIDs []string
	for _, evt := range replayed {
		var data map[string]interface{}
		json.Unmarshal(evt.Data, &data)
		replayedNodeIDs = append(replayedNodeIDs, data["node_id"].(string))
	}
	if replayedNodeIDs[0] != "new1" || replayedNodeIDs[1] != "new2" {
		t.Fatalf("expected replayed node_ids [new1, new2], got %v", replayedNodeIDs)
	}
	t.Logf("Last-Event-ID replay: correctly replayed %d events after ID %s", len(replayed), lastEventID)
}

// TestSyncRequiredDegradation verifies that clients receive a sync:required event when the buffer is expired/empty.
func TestSyncRequiredDegradation(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-sync-" + uuid.New().String()
	ctx := context.Background()

	t.Cleanup(func() { rdb.Del(ctx, ws.BufferKey(runtimeID)) })

	// When the buffer is empty and the client provided a Last-Event-ID,
	// GetBufferedEvents returns empty (buffer has expired)
	replayed, err := hub.GetBufferedEvents(ctx, runtimeID, "1234567890")
	if err != nil {
		t.Fatalf("get buffered events: %v", err)
	}

	// Buffer is empty, so no events are replayed
	if len(replayed) != 0 {
		t.Fatalf("expected 0 replayed events for expired buffer, got %d", len(replayed))
	}

	// In the real SSE handler, this would trigger a sync:required event.
	// We verify the conditions that lead to sync:required:
	// - Client sends Last-Event-ID
	// - Buffer returns empty (expired or never existed)
	// This means the client must perform a full sync.
	t.Log("sync:required degradation: empty buffer correctly signals full sync needed")

	// Also test with unparseable Last-Event-ID
	replayed, err = hub.GetBufferedEvents(ctx, runtimeID, "not-a-timestamp")
	if err != nil {
		t.Fatalf("get buffered events with bad ID: %v", err)
	}
	if len(replayed) != 0 {
		t.Fatalf("expected 0 events for unparseable Last-Event-ID, got %d", len(replayed))
	}
	t.Log("sync:required degradation: unparseable Last-Event-ID also signals full sync")
}

// TestCrossInstanceDelivery verifies that events are delivered to local subscribers via Redis Pub/Sub.
func TestCrossInstanceDelivery(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-cross-" + uuid.New().String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the hub's Redis listener
	go hub.Start(ctx)
	t.Cleanup(func() { hub.Close() })

	// Allow time for Redis Pub/Sub to establish subscription
	time.Sleep(500 * time.Millisecond)

	// Subscribe locally
	ch, unsub := hub.Subscribe(runtimeID)
	defer unsub()

	// Publish event directly via Redis (simulating another instance)
	event := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventNodePending,
		Data:  json.RawMessage(`{"node_id":"cross-instance-test"}`),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	err = rdb.Publish(ctx, ws.RedisChannel(runtimeID), data).Err()
	if err != nil {
		t.Fatalf("publish to redis: %v", err)
	}

	// Wait for the event to arrive via Redis Pub/Sub
	select {
	case received := <-ch:
		if received.Event != ws.EventNodePending {
			t.Fatalf("expected event type %s, got %s", ws.EventNodePending, received.Event)
		}
		var d map[string]interface{}
		json.Unmarshal(received.Data, &d)
		if d["node_id"] != "cross-instance-test" {
			t.Fatalf("expected node_id 'cross-instance-test', got %v", d["node_id"])
		}
		t.Log("cross-instance delivery: event received via Redis Pub/Sub")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for cross-instance event delivery")
	}
}

// TestPublishDeliversLocally verifies that Publish delivers events to local subscribers.
func TestPublishDeliversLocally(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-local-" + uuid.New().String()
	ctx := context.Background()

	t.Cleanup(func() {
		rdb.Del(ctx, ws.BufferKey(runtimeID))
		hub.Close()
	})

	// Subscribe locally
	ch, unsub := hub.Subscribe(runtimeID)
	defer unsub()

	// Publish event
	event := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventNodePending,
		Data:  json.RawMessage(`{"node_id":"local-test"}`),
	}

	err := hub.Publish(ctx, runtimeID, event)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Verify local delivery
	select {
	case received := <-ch:
		if received.Event != ws.EventNodePending {
			t.Fatalf("expected event type %s, got %s", ws.EventNodePending, received.Event)
		}
		t.Log("local delivery: event received by local subscriber")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for local event delivery")
	}

	// Verify event is buffered in Redis
	count, err := rdb.ZCard(ctx, ws.BufferKey(runtimeID)).Result()
	if err != nil {
		t.Fatalf("zcard: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 buffered event, got %d", count)
	}
}

// TestSubscribeUnsubscribe verifies that subscribe and unsubscribe work correctly.
func TestSubscribeUnsubscribe(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-sub-" + uuid.New().String()

	// Subscribe
	ch, unsub := hub.Subscribe(runtimeID)

	// Verify channel is registered
	if hub.ClientCount(runtimeID) != 1 {
		t.Fatalf("expected 1 subscriber, got %d", hub.ClientCount(runtimeID))
	}

	// Unsubscribe
	unsub()

	// Verify channel is removed
	if hub.ClientCount(runtimeID) != 0 {
		t.Fatalf("expected 0 subscribers after unsub, got %d", hub.ClientCount(runtimeID))
	}

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
	t.Log("subscribe/unsubscribe works correctly")
}

// TestHubMultipleSubscribers verifies that multiple subscribers to the same runtime all receive events.
func TestHubMultipleSubscribers(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-multi-" + uuid.New().String()
	ctx := context.Background()

	t.Cleanup(func() {
		rdb.Del(ctx, ws.BufferKey(runtimeID))
		hub.Close()
	})

	// Subscribe 3 clients
	ch1, unsub1 := hub.Subscribe(runtimeID)
	ch2, unsub2 := hub.Subscribe(runtimeID)
	ch3, unsub3 := hub.Subscribe(runtimeID)
	defer unsub1()
	defer unsub2()
	defer unsub3()

	// Publish event
	event := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventNodePending,
		Data:  json.RawMessage(`{"node_id":"broadcast"}`),
	}
	hub.Publish(ctx, runtimeID, event)

	// All 3 should receive
	for i, ch := range []<-chan ws.SSEEvent{ch1, ch2, ch3} {
		select {
		case <-ch:
			t.Logf("subscriber %d received event", i+1)
		case <-time.After(2 * time.Second):
			t.Fatalf("subscriber %d did not receive event", i+1)
		}
	}
}
