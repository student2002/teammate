// Package workflow contains the workflow pure logic: the node type
// registry, whole-graph template validation and the expression evaluator
// (plan §2.1, §4, §5.1, §8). The package is self-contained and must not
// depend on storage, HTTP or frontend layers.
package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Output port kinds. "fixed" nodes expose the ports declared on their
// definition; switch/parallel nodes derive their ports from config.branches.
const (
	OutputPortsFixed            = "fixed"
	OutputPortsSwitchBranches   = "switch_branches"
	OutputPortsParallelBranches = "parallel_branches"
)

// Reserved output port name appended to every flow.switch node (plan §2.1).
const defaultBranchPort = "default"

// PortDefinition names one connection point on a node.
type PortDefinition struct {
	Key string `json:"key"`
}

// NodeDefinition is the server-side contract of one node type; it doubles as
// the GET /api/workflow-node-types response item (§12.1).
type NodeDefinition struct {
	TypeKey         string           `json:"type_key"`
	Category        string           `json:"category"`
	ExecutorKind    string           `json:"executor_kind"`
	InputPorts      []PortDefinition `json:"input_ports"`
	OutputPorts     []PortDefinition `json:"output_ports"`
	OutputPortsKind string           `json:"output_ports_kind"`
	Validate        func(json.RawMessage) error `json:"-"`
}

// Registry holds the node types available to templates. A Registry is safe
// for concurrent reads once built; Register must not be called concurrently
// with reads.
type Registry struct {
	nodes map[string]NodeDefinition
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{nodes: make(map[string]NodeDefinition)}
}

// Register adds a node type. Registering the same type_key twice is an error.
func (r *Registry) Register(def NodeDefinition) error {
	if def.TypeKey == "" {
		return fmt.Errorf("register node: type_key is empty")
	}
	if _, exists := r.nodes[def.TypeKey]; exists {
		return fmt.Errorf("register node: type_key %q already registered", def.TypeKey)
	}
	r.nodes[def.TypeKey] = def
	return nil
}

// MustRegister registers a node type and panics on failure; intended for
// process-startup wiring where a duplicate type_key is a programming error.
func (r *Registry) MustRegister(def NodeDefinition) {
	if err := r.Register(def); err != nil {
		panic(err)
	}
}

// Get returns the definition for typeKey.
func (r *Registry) Get(typeKey string) (NodeDefinition, bool) {
	def, ok := r.nodes[typeKey]
	return def, ok
}

// List returns all definitions sorted by type_key for stable output.
func (r *Registry) List() []NodeDefinition {
	out := make([]NodeDefinition, 0, len(r.nodes))
	for _, def := range r.nodes {
		out = append(out, def)
	}
	slices.SortFunc(out, func(a, b NodeDefinition) int {
		return strings.Compare(a.TypeKey, b.TypeKey)
	})
	return out
}

// OutputPorts returns the output ports of typeKey. switch_branches derives
// one port per configured branch plus "default"; parallel_branches derives
// one port per configured branch. Config that fails to describe the branches
// is an error.
func (r *Registry) OutputPorts(typeKey string, config json.RawMessage) ([]PortDefinition, error) {
	def, ok := r.Get(typeKey)
	if !ok {
		return nil, fmt.Errorf("output ports: unknown type_key: %s", typeKey)
	}
	switch def.OutputPortsKind {
	case OutputPortsSwitchBranches:
		rules, err := switchRulesFromConfig(config)
		if err != nil {
			return nil, fmt.Errorf("output ports for %s: %w", typeKey, err)
		}
		branches := make([]string, 0, len(rules)+1)
		for _, rule := range rules {
			branches = append(branches, rule.Branch)
		}
		branches = append(branches, defaultBranchPort)
		return toPorts(branches), nil
	case OutputPortsParallelBranches:
		branches, err := branchesFromConfig(config)
		if err != nil {
			return nil, fmt.Errorf("output ports for %s: %w", typeKey, err)
		}
		return toPorts(branches), nil
	default:
		return slices.Clone(def.OutputPorts), nil
	}
}

func toPorts(keys []string) []PortDefinition {
	ports := make([]PortDefinition, 0, len(keys))
	for _, key := range keys {
		ports = append(ports, PortDefinition{Key: key})
	}
	return ports
}

// branchesFromConfig reads config.branches as a non-empty array of unique,
// non-empty strings. It backs port derivation and the flow.parallel config
// validator.
func branchesFromConfig(config json.RawMessage) ([]string, error) {
	m, err := decodeConfig(config)
	if err != nil {
		return nil, err
	}
	raw, present := m["branches"]
	if !present || raw == nil {
		return nil, fmt.Errorf("branches is required")
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("branches must be an array of strings")
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("branches must contain at least one entry")
	}
	seen := make(map[string]bool, len(arr))
	branches := make([]string, 0, len(arr))
	for _, el := range arr {
		s, ok := el.(string)
		if !ok {
			return nil, fmt.Errorf("branches must contain only strings")
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("branches must not contain empty names")
		}
		if seen[s] {
			return nil, fmt.Errorf("branches must not contain duplicates")
		}
		seen[s] = true
		branches = append(branches, s)
	}
	return branches, nil
}

// SwitchRule is one routing rule of flow.switch: the branch (port) name and
// the condition group that activates it. The first matching rule in order
// emits its branch; when none matches the "default" port is emitted.
type SwitchRule struct {
	Branch    string
	Condition json.RawMessage
}

// switchRulesFromConfig reads config.rules as ordered routing rules. Branch
// names must be non-empty, unique and must not use the reserved "default"
// name. An empty or absent rules array is valid and yields no rules.
func switchRulesFromConfig(config json.RawMessage) ([]SwitchRule, error) {
	m, err := decodeConfig(config)
	if err != nil {
		return nil, err
	}
	raw, present := m["rules"]
	if !present || raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("rules must be an array")
	}
	seen := make(map[string]bool, len(arr))
	rules := make([]SwitchRule, 0, len(arr))
	for i, el := range arr {
		entry, ok := el.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rules[%d] must be an object", i)
		}
		branch, ok := cfgString(entry, "branch")
		if !ok || strings.TrimSpace(branch) == "" {
			return nil, fmt.Errorf("rules[%d].branch is required", i)
		}
		branch = strings.TrimSpace(branch)
		if branch == defaultBranchPort {
			return nil, fmt.Errorf("rules[%d].branch must not use the reserved name default", i)
		}
		if seen[branch] {
			return nil, fmt.Errorf("rules must not contain duplicate branch %q", branch)
		}
		seen[branch] = true
		cond, err := json.Marshal(entry["condition"])
		if err != nil {
			return nil, fmt.Errorf("rules[%d].condition encode: %w", i, err)
		}
		rules = append(rules, SwitchRule{Branch: branch, Condition: cond})
	}
	return rules, nil
}

// BuiltinRegistry registers the 19 built-in node types of plan §2.1. The
// catalog is pinned by test/workflow/testdata/node-registry.json.
func BuiltinRegistry() (*Registry, error) {
	r := NewRegistry()
	builtins := []NodeDefinition{
		{
			TypeKey: "trigger.manual", Category: "trigger", ExecutorKind: "server",
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success"}),
			Validate: validateAnyObject,
		},
		{
			TypeKey: "trigger.schedule", Category: "trigger", ExecutorKind: "server",
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success"}),
			Validate: validateTriggerSchedule,
		},
		{
			TypeKey: "trigger.github", Category: "trigger", ExecutorKind: "server",
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success"}),
			Validate: validateTriggerGitHub,
		},
		{
			TypeKey: "trigger.webhook", Category: "trigger", ExecutorKind: "server",
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success"}),
			Validate: validateAnyObject,
		},
		{
			TypeKey: "agent.task", Category: "agent", ExecutorKind: "agentd",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success", "failed"}),
			Validate: validateAgentTask,
		},
		{
			TypeKey: "action.github", Category: "integration", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success", "failed"}),
			Validate: validateActionGitHub,
		},
		{
			TypeKey: "action.http", Category: "integration", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success", "failed"}),
			Validate: validateActionHTTP,
		},
		{
			TypeKey: "action.command", Category: "action", ExecutorKind: "agentd",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success", "failed"}),
			Validate: validateActionCommand,
		},
		{
			TypeKey: "action.create_task", Category: "task", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success", "failed"}),
			Validate: validateActionCreateTask,
		},
		{
			TypeKey: "workflow.call", Category: "reuse", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success", "failed"}),
			Validate: validateWorkflowCall,
		},
		{
			TypeKey: "data.set", Category: "data", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success", "failed"}),
			Validate: validateDataSet,
		},
		{
			TypeKey: "data.map", Category: "data", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success", "failed"}),
			Validate: validateDataMap,
		},
		{
			TypeKey: "flow.condition", Category: "logic", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"true", "false"}),
			Validate: validateFlowCondition,
		},
		{
			TypeKey: "flow.switch", Category: "logic", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsSwitchBranches,
			Validate:        validateFlowSwitch,
		},
		{
			TypeKey: "flow.parallel", Category: "logic", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsParallelBranches,
			Validate:        validateFlowParallel,
		},
		{
			TypeKey: "flow.join", Category: "logic", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"success"}),
			Validate: validateFlowJoin,
		},
		{
			TypeKey: "flow.wait", Category: "wait", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"resumed", "timeout"}),
			Validate: validateFlowWait,
		},
		{
			TypeKey: "human.input", Category: "human", ExecutorKind: "human",
			InputPorts:      toPorts([]string{"input"}),
			OutputPortsKind: OutputPortsFixed, OutputPorts: toPorts([]string{"submitted", "cancelled", "timeout"}),
			Validate: validateHumanInput,
		},
		{
			TypeKey: "flow.end", Category: "end", ExecutorKind: "server",
			InputPorts:      toPorts([]string{"completed", "failed"}),
			OutputPortsKind: OutputPortsFixed,
			Validate:        validateAnyObject,
		},
	}
	for _, def := range builtins {
		if err := r.Register(def); err != nil {
			return nil, fmt.Errorf("builtin registry: %w", err)
		}
	}
	return r, nil
}
