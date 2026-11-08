// resolution.go implements the run-time edge resolution and node activation
// planner (plan §5.2, §6.2). It is a pure function over the current task graph
// state: given every node/edge of one task run it decides which pending edges
// resolve and which waiting nodes become ready or skipped. The package keeps
// its own lifecycle constants so it stays free of internal dependencies; the
// service layer converts between these values and the stored node status.
package workflow

import (
	"encoding/json"
	"fmt"
)

// NodeLifecycleStatus is the run status of a task node (plan §6.2). The
// strings match the task_node_status database enum one-to-one.
type NodeLifecycleStatus string

const (
	NodeWaiting            NodeLifecycleStatus = "waiting"
	NodeReady              NodeLifecycleStatus = "ready"
	NodeRunning            NodeLifecycleStatus = "running"
	NodeSucceeded          NodeLifecycleStatus = "succeeded"
	NodeFailed             NodeLifecycleStatus = "failed"
	NodeSkipped            NodeLifecycleStatus = "skipped"
	NodeCancelled          NodeLifecycleStatus = "cancelled"
	NodeManualIntervention NodeLifecycleStatus = "manual_intervention"
)

// IsTerminal reports whether the status ends the node's lifecycle. Only
// terminal nodes emit ports and resolve their outbound edges.
func (s NodeLifecycleStatus) IsTerminal() bool {
	switch s {
	case NodeSucceeded, NodeFailed, NodeSkipped, NodeCancelled:
		return true
	}
	return false
}

// Edge resolution values, matching the edge_resolution database enum.
const (
	EdgePending   = "pending"
	EdgeActivated = "activated"
	EdgeSkipped   = "skipped"
)

// Node type keys with dedicated port emission rules (plan §5.2).
const (
	TypeKeyFlowCondition = "flow.condition"
	TypeKeyFlowSwitch    = "flow.switch"
	TypeKeyFlowParallel  = "flow.parallel"
	TypeKeyFlowWait      = "flow.wait"
	TypeKeyHumanInput    = "human.input"
)

// NodeRunState is the run state of one task node as seen by the planner.
// Definition is the node type's registry contract; the caller resolves it
// from the registry when building the state. WaitKey is the node's external
// wake registration (timer/event/human); a waiting node with a WaitKey is
// event-driven and never planned by edge activation. RetryPending marks a
// failed node scheduled for a timed retry; only the scheduler wakes it.
type NodeRunState struct {
	ID             string
	TypeKey        string
	Status         NodeLifecycleStatus
	ActivationMode string // "all" or "any"
	WaitKey        string // non-empty: waiting for an external wake-up
	RetryPending   bool   // waiting for a scheduled retry (next_retry_at set)
	Config         json.RawMessage
	Output         json.RawMessage
	Definition     NodeDefinition
}

// EdgeRunState is the run state of one task edge.
type EdgeRunState struct {
	ID           string
	SourceNodeID string
	SourcePort   string
	TargetNodeID string
	TargetPort   string
	Resolution   string // EdgePending / EdgeActivated / EdgeSkipped
}

// EdgeResolutionUpdate is one edge whose resolution must be persisted.
type EdgeResolutionUpdate struct {
	EdgeID     string
	Resolution string
}

// NodeStateChange is one waiting node that leaves waiting in this pass.
// Status is either NodeReady or NodeSkipped.
type NodeStateChange struct {
	NodeID string
	Status NodeLifecycleStatus
}

// AdvancePlan is the outcome of one planner pass.
type AdvancePlan struct {
	EdgeUpdates []EdgeResolutionUpdate
	NodeUpdates []NodeStateChange
}

// EmittedPorts returns the set of output ports a terminal node emits.
//
// Semantics by status:
//   - failed: ["failed"] when the definition declares the port.
//   - skipped/cancelled: no ports.
//   - succeeded, by type:
//     flow.condition / flow.switch: [output.branch]. output.branch must be
//     a JSON string; a missing or non-string branch is an error so the
//     caller can fail the advance instead of silently stalling the graph.
//     flow.parallel: every branch configured in config.branches.
//     flow.wait: [output.outcome] when present, otherwise ["resumed"].
//     human.input: [output.choice] when present, otherwise ["submitted"].
//     everything else: ["success"] when the definition declares the port.
//
// A non-terminal status is a caller bug and returns an error.
func EmittedPorts(typeKey string, status NodeLifecycleStatus, output json.RawMessage, config json.RawMessage, def NodeDefinition) ([]string, error) {
	switch status {
	case NodeFailed:
		if hasPort(def.OutputPorts, "failed") {
			return []string{"failed"}, nil
		}
		return nil, nil
	case NodeSkipped, NodeCancelled:
		return nil, nil
	case NodeSucceeded:
	default:
		return nil, fmt.Errorf("emitted ports of %s: status %s is not terminal", typeKey, status)
	}

	switch typeKey {
	case TypeKeyFlowCondition, TypeKeyFlowSwitch:
		branch, err := outputStringField(output, "branch")
		if err != nil {
			return nil, fmt.Errorf("emitted ports of %s: %w", typeKey, err)
		}
		return []string{branch}, nil
	case TypeKeyFlowParallel:
		branches, err := branchesFromConfig(config)
		if err != nil {
			return nil, fmt.Errorf("emitted ports of %s: %w", typeKey, err)
		}
		return branches, nil
	case TypeKeyFlowWait:
		branch, err := outputStringField(output, "outcome")
		if err != nil {
			return []string{"resumed"}, nil
		}
		return []string{branch}, nil
	case TypeKeyHumanInput:
		branch, err := outputStringField(output, "choice")
		if err != nil {
			return []string{"submitted"}, nil
		}
		return []string{branch}, nil
	default:
		if hasPort(def.OutputPorts, "success") {
			return []string{"success"}, nil
		}
		return nil, nil
	}
}

// outputStringField reads a string field from a JSON object output. A missing
// field is reported through ok=false so callers can fall back; a present
// non-string value is an error.
func outputStringField(output json.RawMessage, field string) (string, error) {
	if len(output) == 0 {
		return "", fmt.Errorf("output.%s is required", field)
	}
	var m map[string]any
	if err := json.Unmarshal(output, &m); err != nil {
		return "", fmt.Errorf("decode output: %w", err)
	}
	raw, ok := m[field]
	if !ok || raw == nil {
		return "", fmt.Errorf("output.%s is required", field)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("output.%s must be a string", field)
	}
	return s, nil
}

func hasPort(ports []PortDefinition, key string) bool {
	for _, p := range ports {
		if p.Key == key {
			return true
		}
	}
	return false
}

// PlanAdvance computes the next planner pass for one task run (plan §5.2):
//
//   - every pending edge whose source node is terminal resolves to activated
//     (the source emitted the edge's source port) or skipped;
//   - every waiting node whose inbound edges are all resolved becomes ready
//     (mode "all": every edge activated; mode "any": at least one activated)
//     or skipped (all resolved but the activation rule is not met).
//
// Skipped nodes cascade: a node planned as skipped emits no ports, so the
// planner keeps iterating until no further edge or node changes appear. Edges
// referencing unknown nodes, duplicate node ids, or nodes of unregistered
// types are errors.
func PlanAdvance(nodes []NodeRunState, edges []EdgeRunState) (AdvancePlan, error) {
	var plan AdvancePlan

	byID := make(map[string]NodeRunState, len(nodes))
	for _, n := range nodes {
		if _, dup := byID[n.ID]; dup {
			return plan, fmt.Errorf("plan advance: duplicate node id %s", n.ID)
		}
		if n.Definition.TypeKey == "" {
			return plan, fmt.Errorf("plan advance: node %s has unknown type %s", n.ID, n.TypeKey)
		}
		byID[n.ID] = n
	}

	inbound := make(map[string][]int, len(nodes))
	byEdgeID := make(map[string]int, len(edges))
	for i, e := range edges {
		if _, ok := byID[e.SourceNodeID]; !ok {
			return plan, fmt.Errorf("plan advance: edge %s references unknown source node %s", e.ID, e.SourceNodeID)
		}
		if _, ok := byID[e.TargetNodeID]; !ok {
			return plan, fmt.Errorf("plan advance: edge %s references unknown target node %s", e.ID, e.TargetNodeID)
		}
		if _, dup := byEdgeID[e.ID]; dup {
			return plan, fmt.Errorf("plan advance: duplicate edge id %s", e.ID)
		}
		byEdgeID[e.ID] = i
		inbound[e.TargetNodeID] = append(inbound[e.TargetNodeID], i)
	}

	status := make(map[string]NodeLifecycleStatus, len(nodes))
	for _, n := range nodes {
		status[n.ID] = n.Status
	}
	resolution := make(map[string]string, len(edges))
	for _, e := range edges {
		resolution[e.ID] = e.Resolution
	}
	emitted := make(map[string][]string, len(nodes))

	emittedPorts := func(nodeID string) ([]string, error) {
		if ports, ok := emitted[nodeID]; ok {
			return ports, nil
		}
		n := byID[nodeID]
		// The working-copy status is authoritative: a node planned as skipped
		// within this pass emits no ports (the cascade).
		ports, err := EmittedPorts(n.TypeKey, status[nodeID], n.Output, n.Config, n.Definition)
		if err != nil {
			return nil, err
		}
		emitted[nodeID] = ports
		return ports, nil
	}

	for {
		changed := false

		for _, e := range edges {
			if resolution[e.ID] != EdgePending {
				continue
			}
			if !status[e.SourceNodeID].IsTerminal() {
				continue
			}
			ports, err := emittedPorts(e.SourceNodeID)
			if err != nil {
				return plan, fmt.Errorf("plan advance: node %s: %w", e.SourceNodeID, err)
			}
			next := EdgeSkipped
			for _, p := range ports {
				if p == e.SourcePort {
					next = EdgeActivated
					break
				}
			}
			resolution[e.ID] = next
			plan.EdgeUpdates = append(plan.EdgeUpdates, EdgeResolutionUpdate{EdgeID: e.ID, Resolution: next})
			changed = true
		}

		for _, n := range nodes {
			if status[n.ID] != NodeWaiting {
				continue
			}
			// A waiting node with a wake key (duration/event/human) is driven
			// by an external trigger, not by edge activation; planning it
			// ready again would loop forever. Same for a node scheduled for a
			// timed retry — the scheduler owns its wake-up.
			if n.WaitKey != "" || n.RetryPending {
				continue
			}
			in := inbound[n.ID]
			if len(in) == 0 {
				continue
			}
			allResolved := true
			activated := 0
			for _, ei := range in {
				switch resolution[edges[ei].ID] {
				case EdgeActivated:
					activated++
				case EdgePending:
					allResolved = false
				}
			}
			if !allResolved {
				continue
			}
			satisfied := activated > 0
			if n.ActivationMode != "any" {
				satisfied = activated == len(in)
			}
			next := NodeSkipped
			if satisfied {
				next = NodeReady
			}
			status[n.ID] = next
			plan.NodeUpdates = append(plan.NodeUpdates, NodeStateChange{NodeID: n.ID, Status: next})
			changed = true
		}

		if !changed {
			return plan, nil
		}
	}
}
