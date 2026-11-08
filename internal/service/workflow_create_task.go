// workflow_create_task.go executes ready action.create_task nodes (S6): the
// title/description expressions are resolved against the run context, an
// independent plain task is created in the same project (type 'task', author
// 'system', no run graph), and the node completes with {task_id, task_title}.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// spawnedTaskAuthorType marks the author of a task created by the workflow
// engine itself (no human/agent principal exists at creation time).
const spawnedTaskAuthorType = "system"

// executeCreateTaskNode runs one claimed action.create_task node.
func (s *WorkflowAsyncNodeService) executeCreateTaskNode(ctx context.Context, node types.TaskNode, attemptNo int) {
	nodeID, err := uuid.Parse(node.ID)
	if err != nil {
		return
	}
	runSvc := NewWorkflowRunService(s.svc)
	fail := func(code, format string, args ...any) {
		message := fmt.Sprintf(format, args...)
		slog.Error("create_task node failed", "node_id", node.ID, "code", code, "message", message)
		if err := runSvc.FailNode(ctx, nodeID, int32(attemptNo), NodeFailure{Code: code, Message: message}); err != nil {
			slog.Error("fail create_task node", "node_id", node.ID, "err", err)
		}
	}

	task, err := s.svc.Store.GetTask(ctx, node.TaskID)
	if err != nil {
		fail("node_execution_failed", "load task: %v", err)
		return
	}
	resolve := resolveTaskContextFor(task, node.TaskID)

	var raw struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(node.Config, &raw); err != nil {
		fail("invalid_config", "decode action.create_task config: %v", err)
		return
	}
	title := strings.TrimSpace(resolveNodeString(raw.Title, resolve))
	if title == "" {
		fail("invalid_config", "action.create_task title resolved to an empty value")
		return
	}
	description := resolveNodeString(raw.Description, resolve)

	projectUUID, err := uuid.Parse(task.ProjectID)
	if err != nil {
		fail("node_execution_failed", "parse project id: %v", err)
		return
	}

	var created types.Task
	err = s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		sequence, err := tx.GetProjectNextTaskSequence(ctx, projectUUID)
		if err != nil {
			return err
		}
		created, err = tx.CreateSpawnedTask(ctx, types.CreateSpawnedTaskParams{
			ProjectID:   task.ProjectID,
			Title:       title,
			Description: description,
			AuthorType:  spawnedTaskAuthorType,
			AuthorID:    uuid.Nil.String(),
			Sequence:    sequence,
		})
		return err
	})
	if err != nil {
		fail("node_execution_failed", "create task: %v", err)
		return
	}

	output, err := json.Marshal(map[string]any{
		"task_id":    created.ID,
		"task_title": created.Title,
	})
	if err != nil {
		fail("node_execution_failed", "marshal output: %v", err)
		return
	}
	if err := runSvc.CompleteNode(ctx, nodeID, int32(attemptNo), NodeResult{Output: output}); err != nil {
		slog.Error("complete create_task node", "node_id", node.ID, "err", err)
	}
}
