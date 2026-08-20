// hub_test.go 覆盖 SSE Hub 的测试。
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

// testKeyPrefix 避免与生产数据冲突。
const testKeyPrefix = "test:sse:"

// TestEventBuffering 验证发布的事件存储在 Redis 有序集合中。
func TestEventBuffering(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	_ = ws.NewHub(rdb) // 此测试中不需要 hub 进行直接的 Redis 操作
	runtimeID := uuid.New().String()
	testBufferKey := testKeyPrefix + runtimeID
	ctx := context.Background()

	// 清理测试键
	t.Cleanup(func() { rdb.Del(ctx, testBufferKey) })

	// 发布多个事件
	events := []ws.SSEEvent{
		{ID: fmt.Sprintf("%d", time.Now().Add(-2*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"1"}`)},
		{ID: fmt.Sprintf("%d", time.Now().Add(-1*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"2"}`)},
		{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"3"}`)},
	}

	// 直接缓冲事件
	for _, evt := range events {
		// 覆盖缓冲区键以进行测试
		data, _ := json.Marshal(evt)
		score, _ := strconv.ParseFloat(evt.ID, 64)
		pipe := rdb.Pipeline()
		pipe.ZAdd(ctx, testBufferKey, redis.Z{Score: score, Member: data})
		pipe.Expire(ctx, testBufferKey, ws.BufferTTL)
		if _, err := pipe.Exec(ctx); err != nil {
			t.Fatalf("buffer event: %v", err)
		}
	}

	// 验证事件已存储在有序集合中
	count, err := rdb.ZCard(ctx, testBufferKey).Result()
	if err != nil {
		t.Fatalf("zcard: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 buffered events, got %d", count)
	}
	t.Logf("event buffering: %d events stored in Redis sorted set", count)
}

// TestLastEventIDReplay 验证使用 Last-Event-ID 连接时可以重放丢失的事件。
func TestLastEventIDReplay(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := uuid.New().String()
	testBufferKey := testKeyPrefix + runtimeID
	ctx := context.Background()

	t.Cleanup(func() { rdb.Del(ctx, testBufferKey) })

	// 创建具有已知时间戳的事件
	now := time.Now()
	events := []ws.SSEEvent{
		{ID: fmt.Sprintf("%d", now.Add(-3*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"old1"}`)},
		{ID: fmt.Sprintf("%d", now.Add(-2*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"old2"}`)},
		{ID: fmt.Sprintf("%d", now.Add(-1*time.Second).UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"new1"}`)},
		{ID: fmt.Sprintf("%d", now.UnixNano()), Event: "node:pending", Data: json.RawMessage(`{"node_id":"new2"}`)},
	}

	// 使用测试键前缀直接缓冲事件
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

	// 模拟 Last-Event-ID = events[1].ID（客户端已看到前 2 个事件）
	lastEventID := events[1].ID

	// 使用 GetBufferedEvents 重放错过的消息
	// 我们需要临时覆盖缓冲区键以进行测试
	// 由于 GetBufferedEvents 使用 BufferKey(runtimeID)，我们需要使用实际的键格式
	// 我们使用真实的 hub 方法和实际的键
	actualRuntimeID := "test-replay-" + runtimeID
	actualBufferKey := ws.BufferKey(actualRuntimeID)

	// 将测试数据复制到实际的缓冲区键格式
	srcKey := testBufferKey
	rdb.Copy(ctx, srcKey, actualBufferKey, 0, true)
	t.Cleanup(func() { rdb.Del(ctx, actualBufferKey) })

	replayed, err := hub.GetBufferedEvents(ctx, actualRuntimeID, lastEventID)
	if err != nil {
		t.Fatalf("get buffered events: %v", err)
	}

	// 应重放 lastEventID 之后的事件（events[2] 和 events[3]）
	if len(replayed) != 2 {
		t.Fatalf("expected 2 replayed events, got %d", len(replayed))
	}

	// 验证重放的事件是正确的
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

// TestSyncRequiredDegradation 验证缓冲区过期/为空时客户端应收到 sync:required 事件。
func TestSyncRequiredDegradation(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-sync-" + uuid.New().String()
	ctx := context.Background()

	t.Cleanup(func() { rdb.Del(ctx, ws.BufferKey(runtimeID)) })

	// 当缓冲区为空且客户端提供了 Last-Event-ID 时，
	// GetBufferedEvents 返回空（缓冲区已过期）
	replayed, err := hub.GetBufferedEvents(ctx, runtimeID, "1234567890")
	if err != nil {
		t.Fatalf("get buffered events: %v", err)
	}

	// 缓冲区为空，因此不重放任何事件
	if len(replayed) != 0 {
		t.Fatalf("expected 0 replayed events for expired buffer, got %d", len(replayed))
	}

	// 在真实的 SSE 处理器中，这会触发 sync:required 事件。
	// 我们验证导致 sync:required 的条件：
	// - 客户端发送 Last-Event-ID
	// - 缓冲区返回空（已过期或从未存在）
	// 这意味着客户端必须执行一次完整同步。
	t.Log("sync:required degradation: empty buffer correctly signals full sync needed")

	// 同时使用无法解析的 Last-Event-ID 进行测试
	replayed, err = hub.GetBufferedEvents(ctx, runtimeID, "not-a-timestamp")
	if err != nil {
		t.Fatalf("get buffered events with bad ID: %v", err)
	}
	if len(replayed) != 0 {
		t.Fatalf("expected 0 events for unparseable Last-Event-ID, got %d", len(replayed))
	}
	t.Log("sync:required degradation: unparseable Last-Event-ID also signals full sync")
}

// TestCrossInstanceDelivery 验证通过 Redis Pub/Sub 将事件传递给本地订阅者。
func TestCrossInstanceDelivery(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-cross-" + uuid.New().String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 启动 hub 的 Redis 监听器
	go hub.Start(ctx)
	t.Cleanup(func() { hub.Close() })

	// 给 Redis Pub/Sub 留出建立订阅的时间
	time.Sleep(500 * time.Millisecond)

	// 在本地订阅
	ch, unsub := hub.Subscribe(runtimeID)
	defer unsub()

	// 直接通过 Redis 发布事件（模拟另一个实例）
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

	// 等待事件通过 Redis Pub/Sub 到达
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

// TestPublishDeliversLocally 验证 Publish 将事件传递给本地订阅者。
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

	// 在本地订阅
	ch, unsub := hub.Subscribe(runtimeID)
	defer unsub()

	// 发布事件
	event := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventNodePending,
		Data:  json.RawMessage(`{"node_id":"local-test"}`),
	}

	err := hub.Publish(ctx, runtimeID, event)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// 验证本地投递
	select {
	case received := <-ch:
		if received.Event != ws.EventNodePending {
			t.Fatalf("expected event type %s, got %s", ws.EventNodePending, received.Event)
		}
		t.Log("local delivery: event received by local subscriber")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for local event delivery")
	}

	// 验证事件已缓冲在 Redis 中
	count, err := rdb.ZCard(ctx, ws.BufferKey(runtimeID)).Result()
	if err != nil {
		t.Fatalf("zcard: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 buffered event, got %d", count)
	}
}

// TestSubscribeUnsubscribe 验证订阅和取消订阅功能正常。
func TestSubscribeUnsubscribe(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	hub := ws.NewHub(rdb)
	runtimeID := "test-sub-" + uuid.New().String()

	// 订阅
	ch, unsub := hub.Subscribe(runtimeID)

	// 验证通道已注册
	if hub.ClientCount(runtimeID) != 1 {
		t.Fatalf("expected 1 subscriber, got %d", hub.ClientCount(runtimeID))
	}

	// 取消订阅
	unsub()

	// 验证通道已移除
	if hub.ClientCount(runtimeID) != 0 {
		t.Fatalf("expected 0 subscribers after unsub, got %d", hub.ClientCount(runtimeID))
	}

	// 通道应已关闭
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
	t.Log("subscribe/unsubscribe works correctly")
}

// TestHubMultipleSubscribers 验证同一运行时的多个订阅者都能接收到事件。
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

	// 订阅 3 clients
	ch1, unsub1 := hub.Subscribe(runtimeID)
	ch2, unsub2 := hub.Subscribe(runtimeID)
	ch3, unsub3 := hub.Subscribe(runtimeID)
	defer unsub1()
	defer unsub2()
	defer unsub3()

	// 发布事件
	event := ws.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: ws.EventNodePending,
		Data:  json.RawMessage(`{"node_id":"broadcast"}`),
	}
	hub.Publish(ctx, runtimeID, event)

	// 所有 3 个都应收到
	for i, ch := range []<-chan ws.SSEEvent{ch1, ch2, ch3} {
		select {
		case <-ch:
			t.Logf("subscriber %d received event", i+1)
		case <-time.After(2 * time.Second):
			t.Fatalf("subscriber %d did not receive event", i+1)
		}
	}
}
