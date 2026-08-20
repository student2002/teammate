// gateway_test.go 覆盖 WebSocket 日志网关的测试。
package ws_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/ws"
)

// TestLogMessageDelivery 验证发布日志消息能够将其传递给订阅者。
func TestLogMessageDelivery(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	gw := ws.NewGateway(rdb)
	taskID := "task-log-delivery-" + uuid.New().String()[:8]
	ctx := context.Background()

	t.Cleanup(func() { gw.Close() })

	// 订阅
	ch, unsub := gw.Subscribe(taskID)
	defer unsub()

	// 发布一条日志消息
	msg := ws.LogMessage{
		TaskID:    taskID,
		NodeID:    uuid.New().String(),
		Type:      "stdout",
		Content:   "Hello, world!",
		Timestamp: time.Now().UnixMilli(),
	}

	err := gw.PublishLog(ctx, taskID, msg)
	if err != nil {
		t.Fatalf("publish log: %v", err)
	}

	// 验证本地投递
	select {
	case received := <-ch:
		if received.Content != "Hello, world!" {
			t.Fatalf("expected content 'Hello, world!', got %q", received.Content)
		}
		if received.TaskID != taskID {
			t.Fatalf("expected task_id %s, got %s", taskID, received.TaskID)
		}
		if received.Type != "stdout" {
			t.Fatalf("expected type 'stdout', got %q", received.Type)
		}
		t.Log("log message delivery: subscriber received message correctly")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for log message delivery")
	}
}

// TestLogDesensitization 验证 API 密钥、令牌和密码在传递前会被遮蔽处理。
func TestLogDesensitization(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	gw := ws.NewGateway(rdb)
	taskID := "task-desensitize-" + uuid.New().String()[:8]
	ctx := context.Background()

	t.Cleanup(func() { gw.Close() })

	// 订阅
	ch, unsub := gw.Subscribe(taskID)
	defer unsub()

	testCases := []struct {
		name     string
		input    string
		forbid   string // 输出中必须不出现的子串
	}{
		{
			name:   "api_key_sk",
			input:  "Using API key sk-abc123def456 for authentication",
			forbid: "sk-abc123def456",
		},
		{
			name:   "api_key_tm",
			input:  "Token tm_abcdefgh12345678 is valid",
			forbid: "tm_abcdefgh12345678",
		},
		{
			name:   "api_key_key",
			input:  "Using key_supersecret12345 for access",
			forbid: "key_supersecret12345",
		},
		{
			name:   "bearer_token",
			input:  "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			forbid: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0",
		},
		{
			name:   "password_equals",
			input:  "password=supersecret123",
			forbid: "supersecret123",
		},
		{
			name:   "password_colon",
			input:  "pass: mysecretpass",
			forbid: "mysecretpass",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := ws.LogMessage{
				TaskID:    taskID,
				NodeID:    uuid.New().String(),
				Type:      "stdout",
				Content:   tc.input,
				Timestamp: time.Now().UnixMilli(),
			}

			err := gw.PublishLog(ctx, taskID, msg)
			if err != nil {
				t.Fatalf("publish log: %v", err)
			}

			select {
			case received := <-ch:
				if contains(received.Content, tc.forbid) {
					t.Fatalf("desensitization failed: forbidden string %q found in output %q", tc.forbid, received.Content)
				}
				t.Logf("desensitization OK: input=%q → output=%q", tc.input, received.Content)
			case <-time.After(2 * time.Second):
				t.Fatal("timeout waiting for log message")
			}
		})
	}
}

// TestLogDesensitizationDirect 直接测试 Desensitize 函数，无需 Redis。
func TestLogDesensitizationDirect(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		forbid   string
		contains string // 应该出现的子串
	}{
		{
			name:     "sk_key_masked",
			input:    "key is sk-abc123def456ghi",
			forbid:   "abc123def456ghi",
			contains: "sk-****",
		},
		{
			name:     "tm_key_masked",
			input:    "token tm_abcdefgh12345678",
			forbid:   "abcdefgh12345678",
			contains: "tm_****",
		},
		{
			name:     "bearer_masked",
			input:    "Bearer abc123xyz789",
			forbid:   "abc123xyz789",
			contains: "Bearer ****",
		},
		{
			name:     "password_masked",
			input:    "password=MySecretPass123",
			forbid:   "MySecretPass123",
			contains: "****",
		},
		{
			name:     "pwd_masked",
			input:    "pwd: AnotherSecret",
			forbid:   "AnotherSecret",
			contains: "****",
		},
		{
			name:     "email_masked",
			input:    "user@example.com logged in",
			forbid:   "user@example.com",
			contains: "u***@example.com",
		},
		{
			name:     "jwt_masked",
			input:    "token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.abc123def456",
			forbid:   "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.abc123def456",
			contains: "eyJ****.eyJ****.****",
		},
		{
			name:     "plain_text_unchanged",
			input:    "Hello, this is a normal log message",
			forbid:   "",
			contains: "Hello, this is a normal log message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ws.Desensitize(tt.input)
			if tt.forbid != "" && contains(result, tt.forbid) {
				t.Fatalf("forbidden string %q found in result %q", tt.forbid, result)
			}
			if tt.contains != "" && !contains(result, tt.contains) {
				t.Fatalf("expected substring %q not found in result %q", tt.contains, result)
			}
		})
	}
}

// TestGatewayMultipleSubscribers 验证订阅同一任务的多个客户端都能收到日志消息。
func TestGatewayMultipleSubscribers(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	gw := ws.NewGateway(rdb)
	taskID := "task-multi-sub-" + uuid.New().String()[:8]
	ctx := context.Background()

	t.Cleanup(func() { gw.Close() })

	// 订阅 3 clients
	ch1, unsub1 := gw.Subscribe(taskID)
	ch2, unsub2 := gw.Subscribe(taskID)
	ch3, unsub3 := gw.Subscribe(taskID)
	defer unsub1()
	defer unsub2()
	defer unsub3()

	// 发布一条日志消息
	msg := ws.LogMessage{
		TaskID:    taskID,
		NodeID:    uuid.New().String(),
		Type:      "stdout",
		Content:   "broadcast message",
		Timestamp: time.Now().UnixMilli(),
	}
	gw.PublishLog(ctx, taskID, msg)

	// 所有 3 个都应收到
	for i, ch := range []<-chan ws.LogMessage{ch1, ch2, ch3} {
		select {
		case received := <-ch:
			if received.Content != "broadcast message" {
				t.Fatalf("subscriber %d: expected 'broadcast message', got %q", i+1, received.Content)
			}
			t.Logf("subscriber %d received message", i+1)
		case <-time.After(2 * time.Second):
			t.Fatalf("subscriber %d did not receive message", i+1)
		}
	}
}

// TestGatewayCrossInstanceDelivery 验证通过 Redis 发布的日志消息能传递给本地订阅者。
func TestGatewayCrossInstanceDelivery(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	gw := ws.NewGateway(rdb)
	taskID := "task-cross-" + uuid.New().String()[:8]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 启动网关的 Redis 监听器
	go gw.Start(ctx)
	t.Cleanup(func() { gw.Close() })

	// 给 Redis Pub/Sub 留出建立订阅的时间
	time.Sleep(1500 * time.Millisecond)

	// 在本地订阅
	ch, unsub := gw.Subscribe(taskID)
	defer unsub()

	// 直接通过 Redis 发布（模拟另一个实例）
	// 必须包装成 redisLogMessage 格式，因为 Gateway.Start 需要它
	type redisLogMessage struct {
		Source string         `json:"source"`
		Msg    ws.LogMessage  `json:"msg"`
	}
	msg := ws.LogMessage{
		TaskID:    taskID,
		NodeID:    uuid.New().String(),
		Type:      "stdout",
		Content:   "cross-instance log",
		Timestamp: time.Now().UnixMilli(),
	}
	wrapped := redisLogMessage{
		Source: "other-instance", // 使用不同的来源以避免去重
		Msg:    msg,
	}
	data, _ := json.Marshal(wrapped)
	err := rdb.Publish(ctx, ws.LogChannel(taskID), data).Err()
	if err != nil {
		t.Fatalf("publish to redis: %v", err)
	}

	// 等待事件通过 Redis Pub/Sub 到达
	select {
	case received := <-ch:
		if received.Content != "cross-instance log" {
			t.Fatalf("expected 'cross-instance log', got %q", received.Content)
		}
		t.Log("cross-instance log delivery: message received via Redis Pub/Sub")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for cross-instance log delivery")
	}
}

// TestGatewaySubscribeUnsubscribe 验证订阅/取消订阅的生命周期。
func TestGatewaySubscribeUnsubscribe(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	gw := ws.NewGateway(rdb)
	taskID := "task-sub-lifecycle-" + uuid.New().String()[:8]

	// 订阅
	ch, unsub := gw.Subscribe(taskID)

	// 验证通道已注册
	if gw.ClientCount(taskID) != 1 {
		t.Fatalf("expected 1 subscriber, got %d", gw.ClientCount(taskID))
	}

	// 取消订阅
	unsub()

	// 验证通道已移除
	if gw.ClientCount(taskID) != 0 {
		t.Fatalf("expected 0 subscribers after unsub, got %d", gw.ClientCount(taskID))
	}

	// 通道应已关闭
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
	t.Log("gateway subscribe/unsubscribe works correctly")
}

// TestLogAutoTimestamp 验证如果未提供时间戳，会自动生成一个。
func TestLogAutoTimestamp(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	gw := ws.NewGateway(rdb)
	taskID := "task-timestamp-" + uuid.New().String()[:8]
	ctx := context.Background()

	t.Cleanup(func() { gw.Close() })

	ch, unsub := gw.Subscribe(taskID)
	defer unsub()

	// 使用零时间戳发布
	msg := ws.LogMessage{
		TaskID:  taskID,
		NodeID:  uuid.New().String(),
		Type:    "stdout",
		Content: "auto timestamp test",
	}
	gw.PublishLog(ctx, taskID, msg)

	select {
	case received := <-ch:
		if received.Timestamp == 0 {
			t.Fatal("expected auto-generated timestamp, got 0")
		}
		t.Logf("auto timestamp: %d", received.Timestamp)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for log message")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}


