// workflow_human.go implements the human.input completion endpoint's service
// side (plan §12.5): a human member submits or cancels a waiting human.input
// node; the node succeeds with {"choice": ...} which emits the matching
// submitted/cancelled port, and the task advances. human waits carry no
// expiry in this stage — the registry's timeout port has no driver yet.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/internal/workflow"
)

// SubmitHumanInput completes one waiting human.input node of a task with the
// operator's choice (plan §12.5): "submitted" emits the submitted port with
// the supplied data on the node output, "cancelled" emits the cancelled port.
// A node that is not a waiting human.input of the addressed task fails with
// ErrNodeNotRunning; an unknown node fails with ErrNodeNotFound.
func (s *WorkflowRunService) SubmitHumanInput(ctx context.Context, taskID int32, nodeID uuid.UUID, action string, data json.RawMessage) error {
	var (
		st    advanceState
		pubID int32
	)
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		node, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("submit node %s: %w", nodeID, ErrNodeNotFound)
			}
			return err
		}
		if node.TaskID != taskID {
			return fmt.Errorf("submit node %s: %w", nodeID, ErrNodeNotFound)
		}
		if node.TypeKey != workflow.TypeKeyHumanInput ||
			(node.Status != types.TaskNodeStatusWaiting &&
				node.Status != types.TaskNodeStatusManualIntervention) ||
			!strings.HasPrefix(node.WaitKey, "human:") {
			return fmt.Errorf("submit node %s: %w", nodeID, ErrNodeNotRunning)
		}

		now := s.svc.Store.Clock.Now()
		node.Status = types.TaskNodeStatusSucceeded
		node.CompletedAt = &now
		node.WaitKey = ""
		node.WaitExpiresAt = nil
		node.AttemptCount++
		output := map[string]any{"choice": action}
		if action == "submitted" {
			output["data"] = decodeJSONValue(data)
		}
		outputJSON, err := json.Marshal(output)
		if err != nil {
			return fmt.Errorf("submit node %s: marshal output: %w", nodeID, err)
		}
		node.Output = outputJSON
		if err := s.writeNodeRun(ctx, tx, &node); err != nil {
			return err
		}
		if err := s.writeAttempt(ctx, tx, &node, node.AttemptCount, "succeeded", node.Input, outputJSON, json.RawMessage("{}"), now); err != nil {
			return err
		}
		pubID = node.TaskID
		st, err = s.advanceTx(ctx, tx, node.TaskID)
		return err
	})
	if err != nil {
		return err
	}
	if pubID != 0 {
		s.publishReadyNodes(ctx, st.projectID, pubID, st.readyAgentd)
	}
	return nil
}
