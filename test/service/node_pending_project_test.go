// node_pending_project_test.go 覆盖 node:pending 项目级广播：只投递给项目成员 Agent，
// 非项目成员 Agent 收不到节点通知（信息隔离）。
package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// TestCreateTask_PublishesNodePendingOnlyToProjectMembers 验证创建任务时，
// node:pending 只发给项目成员 Agent 的在线 runtime；非项目成员 Agent 不收到广播。
func TestCreateTask_PublishesNodePendingOnlyToProjectMembers(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	hub := &recordingHub{}
	svc.Hub = hub

	// agent1、agent2 是项目成员（setupServiceTest 已加入项目）
	agent1Runtime := createOnlineRuntime(t, svc, env.agent1ID)
	agent2Runtime := createOnlineRuntime(t, svc, env.agent2ID)

	// 创建非项目成员 agent3（同一工作区，但不属于该项目）
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

	// 通过 service 层创建任务（触发 publishNodePendingEvents 项目级广播）
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

	// 收集所有 node:pending 事件的投递目标
	subs, events := hub.recorded()
	var pendingSubs []string
	for i, e := range events {
		if e.Event == types.EventNodePending {
			pendingSubs = append(pendingSubs, subs[i])
		}
	}

	// 只有项目成员（agent1/agent2）的 runtime 收到广播
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
