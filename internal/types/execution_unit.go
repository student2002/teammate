// execution_unit.go defines the opaque execution unit of the agentd run
// protocol (plan §12.6): the flat JSON the server sends on claim and the
// daemon executes without seeing workflow internals (type keys, ports, edges,
// retry policy). The JSON tags are the wire contract with the agentd daemon
// and must stay byte-compatible with its TaskNode decoder.
package types

import "encoding/json"

// Execution kinds of the agentd protocol: only the two values the daemon can
// execute locally.
const (
	ExecutionKindAgent   = "agent"
	ExecutionKindCommand = "command"
)

// ExecutionCommand is the final program invocation of a command execution
// unit. The server resolves all templates before delivery; the daemon runs it
// verbatim as program + argv, never through a shell.
type ExecutionCommand struct {
	Program string            `json:"program"`
	Args    []string          `json:"args"`
	WorkDir string            `json:"workdir"`
	Env     map[string]string `json:"env"`
}

// ExecutionUnit is one opaque execution ticket for the agentd daemon. The
// JSON field names mirror the daemon's TaskNode struct exactly.
type ExecutionUnit struct {
	ID              string            `json:"id"`
	TaskID          int32             `json:"task_id"`
	Name            string            `json:"name"`
	Status          string            `json:"status"`
	ExecutionID     string            `json:"execution_id"`
	ExecutionKind   string            `json:"execution_kind"`
	Instruction     string            `json:"instruction"`
	Input           json.RawMessage   `json:"input"`
	Command         *ExecutionCommand `json:"command"`
	TimeoutMinutes  int32             `json:"timeout_minutes"`
	ReadonlyDirs    json.RawMessage   `json:"readonly_dirs"`
	FullControlDirs json.RawMessage   `json:"full_control_dirs"`
}

// InProgressExecution is one claimed-but-unfinished execution unit, served on
// the recovery endpoint so a restarted daemon resumes with the original
// execution id.
type InProgressExecution struct {
	ExecutionUnit
	ProjectID string `json:"project_id"`
}
