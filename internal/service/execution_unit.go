// execution_unit.go builds the opaque execution units of the agentd run
// protocol (plan §12.6): it converts a run node's config into the flat ticket
// the daemon executes, without exposing type keys, ports, edges or retry
// policy. JSON shapes follow the daemon's TaskNode decoder exactly.
package service

import (
	"encoding/json"

	"github.com/teammate/server/internal/types"
)

// BuildExecutionUnit renders one run node as an agentd execution unit. The
// execution id is the opaque claim token (the attempt id); unclaimed nodes
// pass the empty string. Non-agentd node kinds produce an empty
// execution_kind — the daemon only ever sees them while filtering ready
// nodes, and a non-agentd node never becomes claimable.
func BuildExecutionUnit(node types.TaskNode, executionID string) types.ExecutionUnit {
	unit := types.ExecutionUnit{
		ID:              node.ID,
		TaskID:          node.TaskID,
		Name:            node.Name,
		Status:          node.Status,
		ExecutionID:     executionID,
		Input:           ensureObjectRaw(node.Input),
		TimeoutMinutes:  configTimeoutMinutes(node.Config),
		ReadonlyDirs:    ensureArrayRaw(node.ReadonlyDirs),
		FullControlDirs: ensureArrayRaw(node.FullControlDirs),
	}
	switch node.TypeKey {
	case "agent.task":
		unit.ExecutionKind = types.ExecutionKindAgent
		unit.Instruction = configInstruction(node.Config)
	case "action.command":
		unit.ExecutionKind = types.ExecutionKindCommand
		unit.Command = configCommand(node.Config)
	}
	return unit
}

// configInstruction reads config.instruction of an agent.task node.
func configInstruction(config json.RawMessage) string {
	var cfg struct {
		Instruction string `json:"instruction"`
	}
	_ = json.Unmarshal(config, &cfg)
	return cfg.Instruction
}

// configTimeoutMinutes reads config.timeout_minutes; zero means the daemon
// default applies.
func configTimeoutMinutes(config json.RawMessage) int32 {
	var cfg struct {
		TimeoutMinutes float64 `json:"timeout_minutes"`
	}
	_ = json.Unmarshal(config, &cfg)
	return int32(cfg.TimeoutMinutes)
}

// configCommand reads config.command of an action.command node; a malformed
// spec yields an empty command so the daemon fails the execution instead of
// the server crashing the claim.
func configCommand(config json.RawMessage) *types.ExecutionCommand {
	var cfg struct {
		Command *types.ExecutionCommand `json:"command"`
	}
	_ = json.Unmarshal(config, &cfg)
	if cfg.Command == nil {
		return &types.ExecutionCommand{}
	}
	if cfg.Command.Args == nil {
		cfg.Command.Args = []string{}
	}
	if cfg.Command.Env == nil {
		cfg.Command.Env = map[string]string{}
	}
	return cfg.Command
}

// ensureObjectRaw normalizes a JSON value to an object; absent input
// degrades to {}.
func ensureObjectRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

// ensureArrayRaw normalizes a JSON value to an array; the directory columns
// are nullable and the daemon expects string arrays.
func ensureArrayRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("[]")
	}
	return raw
}
