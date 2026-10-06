// control_event_test.go verifies PublishControlEvent writes exactly one
// buffered copy per event on the daemon stream (live delivery plus offline
// replay come from the same single publish).
package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testconfig"
	"github.com/teammate/server/test/testdb"
)

func TestPublishControlEventBuffersOnce(t *testing.T) {
	pgDB := svcConnectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	opts, err := redis.ParseURL(testconfig.RedisURL())
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { rdb.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available, skipping: %v", err)
	}

	hub := ws.NewHub(rdb)
	svc := service.New(pgDB, hub, rdb)
	bg := context.Background()

	wsRow, err := svc.Store.CreateWorkspace(bg, types.CreateWorkspaceParams{
		Name:        "ctl-evt-" + uuid.NewString()[:8],
		IssuePrefix: "CE",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wsRow.ID) })

	daemon, _, err := svc.Store.CreateDaemon(bg, uuid.MustParse(wsRow.ID), "ctl-evt-daemon")
	if err != nil {
		t.Fatalf("create daemon: %v", err)
	}
	agent, err := svc.Store.CreateDaemonAgent(bg, uuid.MustParse(wsRow.ID), daemon.ID,
		"ctl-evt-agent", "claude", "", "persona-ctl", "Tester", "tester@teammate.local")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	bufferKey := ws.BufferKey("daemon:" + daemon.ID.String())
	t.Cleanup(func() { rdb.Del(context.Background(), bufferKey) })

	svc.PublishControlEvent(bg, uuid.MustParse(agent.ID), "task:interrupt", map[string]string{"reason": "test"})

	count, err := rdb.ZCard(bg, bufferKey).Result()
	if err != nil {
		t.Fatalf("zcard: %v", err)
	}
	if count != 1 {
		t.Fatalf("control event buffered %d times, want exactly 1", count)
	}
}
