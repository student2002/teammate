// resolution_test.go covers the run-time planner: EmittedPorts per node type
// and terminal status, and PlanAdvance over the §5.2 activation rules
// (condition true/false, switch hit/default, parallel, join all/any, skipped
// cascade, unresolved sources and error cases).
package workflow_test

import (
	"encoding/json"
	"testing"

	"github.com/teammate/server/internal/workflow"
)

// planRegistry is the builtin registry used to fill NodeRunState definitions.
var planRegistry = mustBuiltinRegistry()

func mustBuiltinRegistry() *workflow.Registry {
	r, err := workflow.BuiltinRegistry()
	if err != nil {
		panic(err)
	}
	return r
}

// planNode builds a NodeRunState with the registry definition of typeKey.
func planNode(id, typeKey string, status workflow.NodeLifecycleStatus, mode string, config, output json.RawMessage) workflow.NodeRunState {
	def, ok := planRegistry.Get(typeKey)
	if !ok {
		panic("unknown type in test: " + typeKey)
	}
	if config == nil {
		config = json.RawMessage(`{}`)
	}
	if output == nil {
		output = json.RawMessage(`{}`)
	}
	return workflow.NodeRunState{ID: id, TypeKey: typeKey, Status: status, ActivationMode: mode, Config: config, Output: output, Definition: def}
}

func planEdge(id, source, sourcePort, target, targetPort string, resolution string) workflow.EdgeRunState {
	return workflow.EdgeRunState{ID: id, SourceNodeID: source, SourcePort: sourcePort, TargetNodeID: target, TargetPort: targetPort, Resolution: resolution}
}

func TestEmittedPortsByStatus(t *testing.T) {
	agentDef, _ := planRegistry.Get("agent.task")
	endDef, _ := planRegistry.Get("flow.end")

	cases := []struct {
		name    string
		typeKey string
		status  workflow.NodeLifecycleStatus
		output  json.RawMessage
		want    []string
		wantErr bool
	}{
		{"failed emits failed", "agent.task", workflow.NodeFailed, nil, []string{"failed"}, false},
		{"failed without port emits nothing", "flow.end", workflow.NodeFailed, nil, nil, false},
		{"skipped emits nothing", "agent.task", workflow.NodeSkipped, nil, nil, false},
		{"cancelled emits nothing", "agent.task", workflow.NodeCancelled, nil, nil, false},
		{"running is not terminal", "agent.task", workflow.NodeRunning, nil, nil, true},
		{"waiting is not terminal", "agent.task", workflow.NodeWaiting, nil, nil, true},
		{"fixed success port", "agent.task", workflow.NodeSucceeded, nil, []string{"success"}, false},
		{"no success port emits nothing", "flow.end", workflow.NodeSucceeded, nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := agentDef
			if tc.typeKey == "flow.end" {
				def = endDef
			}
			ports, err := workflow.EmittedPorts(tc.typeKey, tc.status, tc.output, json.RawMessage(`{}`), def)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got ports %v", ports)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(ports) != len(tc.want) {
				t.Fatalf("ports = %v, want %v", ports, tc.want)
			}
			for i, p := range ports {
				if p != tc.want[i] {
					t.Fatalf("ports = %v, want %v", ports, tc.want)
				}
			}
		})
	}
}

func TestEmittedPortsBranchFromOutput(t *testing.T) {
	condDef, _ := planRegistry.Get("flow.condition")
	switchDef, _ := planRegistry.Get("flow.switch")

	ports, err := workflow.EmittedPorts("flow.condition", workflow.NodeSucceeded, json.RawMessage(`{"branch":"true"}`), nil, condDef)
	if err != nil || len(ports) != 1 || ports[0] != "true" {
		t.Fatalf("condition ports = %v, err = %v", ports, err)
	}

	// A succeeded condition without a branch field must error so the caller
	// fails the advance instead of stalling the graph.
	if _, err := workflow.EmittedPorts("flow.condition", workflow.NodeSucceeded, json.RawMessage(`{}`), nil, condDef); err == nil {
		t.Fatalf("condition without branch: expected error")
	}
	if _, err := workflow.EmittedPorts("flow.condition", workflow.NodeSucceeded, json.RawMessage(`{"branch":42}`), nil, condDef); err == nil {
		t.Fatalf("condition with non-string branch: expected error")
	}

	ports, err = workflow.EmittedPorts("flow.switch", workflow.NodeSucceeded, json.RawMessage(`{"branch":"b"}`), nil, switchDef)
	if err != nil || len(ports) != 1 || ports[0] != "b" {
		t.Fatalf("switch ports = %v, err = %v", ports, err)
	}
}

func TestEmittedPortsParallelAndFallbacks(t *testing.T) {
	parallelDef, _ := planRegistry.Get("flow.parallel")
	waitDef, _ := planRegistry.Get("flow.wait")
	humanDef, _ := planRegistry.Get("human.input")

	ports, err := workflow.EmittedPorts("flow.parallel", workflow.NodeSucceeded, nil, json.RawMessage(`{"branches":["p1","p2"]}`), parallelDef)
	if err != nil || len(ports) != 2 || ports[0] != "p1" || ports[1] != "p2" {
		t.Fatalf("parallel ports = %v, err = %v", ports, err)
	}

	ports, err = workflow.EmittedPorts("flow.wait", workflow.NodeSucceeded, json.RawMessage(`{}`), nil, waitDef)
	if err != nil || len(ports) != 1 || ports[0] != "resumed" {
		t.Fatalf("wait default ports = %v, err = %v", ports, err)
	}
	ports, err = workflow.EmittedPorts("flow.wait", workflow.NodeSucceeded, json.RawMessage(`{"outcome":"timeout"}`), nil, waitDef)
	if err != nil || len(ports) != 1 || ports[0] != "timeout" {
		t.Fatalf("wait outcome ports = %v, err = %v", ports, err)
	}

	ports, err = workflow.EmittedPorts("human.input", workflow.NodeSucceeded, json.RawMessage(`{}`), nil, humanDef)
	if err != nil || len(ports) != 1 || ports[0] != "submitted" {
		t.Fatalf("human default ports = %v, err = %v", ports, err)
	}
	ports, err = workflow.EmittedPorts("human.input", workflow.NodeSucceeded, json.RawMessage(`{"choice":"cancelled"}`), nil, humanDef)
	if err != nil || len(ports) != 1 || ports[0] != "cancelled" {
		t.Fatalf("human choice ports = %v, err = %v", ports, err)
	}
}

// planConditionGraph builds condition → (true→agentA, false→agentB).
func planConditionGraph(conditionMet bool) ([]workflow.NodeRunState, []workflow.EdgeRunState) {
	branch := "false"
	if conditionMet {
		branch = "true"
	}
	nodes := []workflow.NodeRunState{
		planNode("cond", "flow.condition", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{"branch":"`+branch+`"}`)),
		planNode("a", "agent.task", workflow.NodeWaiting, "all", nil, nil),
		planNode("b", "agent.task", workflow.NodeWaiting, "all", nil, nil),
	}
	edges := []workflow.EdgeRunState{
		planEdge("e1", "cond", "true", "a", "input", workflow.EdgePending),
		planEdge("e2", "cond", "false", "b", "input", workflow.EdgePending),
	}
	return nodes, edges
}

func TestPlanAdvanceConditionTrue(t *testing.T) {
	nodes, edges := planConditionGraph(true)
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertEdgePlan(t, plan, map[string]string{"e1": workflow.EdgeActivated, "e2": workflow.EdgeSkipped})
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"a": workflow.NodeReady, "b": workflow.NodeSkipped})
}

func TestPlanAdvanceConditionFalse(t *testing.T) {
	nodes, edges := planConditionGraph(false)
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertEdgePlan(t, plan, map[string]string{"e1": workflow.EdgeSkipped, "e2": workflow.EdgeActivated})
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"a": workflow.NodeSkipped, "b": workflow.NodeReady})
}

func TestPlanAdvanceSwitchHitAndDefault(t *testing.T) {
	nodes := []workflow.NodeRunState{
		planNode("sw", "flow.switch", workflow.NodeSucceeded, "all", json.RawMessage(`{"branches":["a","b"]}`), json.RawMessage(`{"branch":"b"}`)),
		planNode("na", "agent.task", workflow.NodeWaiting, "all", nil, nil),
		planNode("nb", "agent.task", workflow.NodeWaiting, "all", nil, nil),
		planNode("nd", "agent.task", workflow.NodeWaiting, "all", nil, nil),
	}
	edges := []workflow.EdgeRunState{
		planEdge("ea", "sw", "a", "na", "input", workflow.EdgePending),
		planEdge("eb", "sw", "b", "nb", "input", workflow.EdgePending),
		planEdge("ed", "sw", "default", "nd", "input", workflow.EdgePending),
	}
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertEdgePlan(t, plan, map[string]string{"ea": workflow.EdgeSkipped, "eb": workflow.EdgeActivated, "ed": workflow.EdgeSkipped})
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"na": workflow.NodeSkipped, "nb": workflow.NodeReady, "nd": workflow.NodeSkipped})
}

func TestPlanAdvanceParallelAllBranches(t *testing.T) {
	nodes := []workflow.NodeRunState{
		planNode("par", "flow.parallel", workflow.NodeSucceeded, "all", json.RawMessage(`{"branches":["p1","p2"]}`), nil),
		planNode("n1", "agent.task", workflow.NodeWaiting, "all", nil, nil),
		planNode("n2", "agent.task", workflow.NodeWaiting, "all", nil, nil),
	}
	edges := []workflow.EdgeRunState{
		planEdge("e1", "par", "p1", "n1", "input", workflow.EdgePending),
		planEdge("e2", "par", "p2", "n2", "input", workflow.EdgePending),
	}
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertEdgePlan(t, plan, map[string]string{"e1": workflow.EdgeActivated, "e2": workflow.EdgeActivated})
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"n1": workflow.NodeReady, "n2": workflow.NodeReady})
}

func TestPlanAdvanceJoinAll(t *testing.T) {
	// Two sources both emitted success → mode=all join is ready.
	nodes := []workflow.NodeRunState{
		planNode("join", "flow.join", workflow.NodeWaiting, "all", json.RawMessage(`{"mode":"all"}`), nil),
		planNode("s1", "agent.task", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{}`)),
		planNode("s2", "agent.task", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{}`)),
	}
	edges := []workflow.EdgeRunState{
		planEdge("e1", "s1", "success", "join", "input", workflow.EdgePending),
		planEdge("e2", "s2", "success", "join", "input", workflow.EdgePending),
	}
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"join": workflow.NodeReady})

	// One side emitted a different port (edge listens on a skipped port) →
	// join all is skipped.
	nodes[1] = planNode("s1", "flow.condition", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{"branch":"true"}`))
	edges[0] = planEdge("e1", "s1", "false", "join", "input", workflow.EdgePending)
	plan, err = workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"join": workflow.NodeSkipped})
}

func TestPlanAdvanceJoinAny(t *testing.T) {
	// Two sources: one emitted success, the other failed (its success edge is
	// skipped but its failed port has no edge to this join, so model it with a
	// condition source emitting false while the edge listens on true).
	nodes := []workflow.NodeRunState{
		planNode("join", "flow.join", workflow.NodeWaiting, "any", json.RawMessage(`{"mode":"any"}`), nil),
		planNode("s1", "agent.task", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{}`)),
		planNode("s2", "flow.condition", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{"branch":"false"}`)),
	}
	edges := []workflow.EdgeRunState{
		planEdge("e1", "s1", "success", "join", "input", workflow.EdgePending),
		planEdge("e2", "s2", "true", "join", "input", workflow.EdgePending),
	}
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertEdgePlan(t, plan, map[string]string{"e1": workflow.EdgeActivated, "e2": workflow.EdgeSkipped})
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"join": workflow.NodeReady})

	// Neither side activated → skipped.
	nodes[1] = planNode("s1", "flow.condition", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{"branch":"false"}`))
	edges[0] = planEdge("e1", "s1", "true", "join", "input", workflow.EdgePending)
	plan, err = workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"join": workflow.NodeSkipped})
}

func TestPlanAdvanceSkippedCascade(t *testing.T) {
	// condition(false) → a(skipped) → b: the skipped node's outbound edge must
	// resolve skipped in the same plan, cascading the skip downstream.
	nodes := []workflow.NodeRunState{
		planNode("cond", "flow.condition", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{"branch":"false"}`)),
		planNode("a", "agent.task", workflow.NodeWaiting, "all", nil, nil),
		planNode("b", "agent.task", workflow.NodeWaiting, "all", nil, nil),
	}
	edges := []workflow.EdgeRunState{
		planEdge("e0", "cond", "true", "a", "input", workflow.EdgePending),
		planEdge("e1", "a", "success", "b", "input", workflow.EdgePending),
	}
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertEdgePlan(t, plan, map[string]string{"e0": workflow.EdgeSkipped, "e1": workflow.EdgeSkipped})
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"a": workflow.NodeSkipped, "b": workflow.NodeSkipped})
}

func TestPlanAdvanceWaitKeyNodeIsEventDriven(t *testing.T) {
	// A waiting node with a wake key (duration/event/human wait) is driven by
	// an external trigger: even with all inbound edges resolved it is not
	// planned ready, so the planner terminates.
	nodes := []workflow.NodeRunState{
		planNode("wait", "flow.wait", workflow.NodeWaiting, "all", json.RawMessage(`{"mode":"duration","minutes":5}`), nil),
		planNode("agent", "agent.task", workflow.NodeWaiting, "all", nil, nil),
	}
	// The wait node's inbound edge is already activated (it fired once); the
	// wait node now holds a duration wake key.
	nodes[0].WaitKey = "duration:1:abc"

	// A trigger source completes the id space of the activated edge.
	nodes = append(nodes, planNode("trg", "trigger.manual", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{}`)))

	edges := []workflow.EdgeRunState{
		planEdge("e1", "trg", "success", "wait", "input", workflow.EdgeActivated),
		planEdge("e2", "wait", "resumed", "agent", "input", workflow.EdgePending),
	}
	planned, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(planned.NodeUpdates) != 0 || len(planned.EdgeUpdates) != 0 {
		t.Fatalf("expected empty plan for event-driven wait node, got %+v", planned)
	}
}

func TestPlanAdvancePendingSourceHoldsNode(t *testing.T) {
	// A running source keeps its edge pending, so the target stays waiting and
	// the plan is empty.
	nodes := []workflow.NodeRunState{
		planNode("agent", "agent.task", workflow.NodeRunning, "all", nil, nil),
		planNode("next", "agent.task", workflow.NodeWaiting, "all", nil, nil),
	}
	edges := []workflow.EdgeRunState{
		planEdge("e1", "agent", "success", "next", "input", workflow.EdgePending),
	}
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.EdgeUpdates) != 0 || len(plan.NodeUpdates) != 0 {
		t.Fatalf("expected empty plan, got %+v", plan)
	}
}

func TestPlanAdvanceAlreadyResolvedEdgesAreStable(t *testing.T) {
	// Edges already activated/skipped are not re-emitted; a node with one
	// pending edge stays waiting.
	nodes := []workflow.NodeRunState{
		planNode("cond", "flow.condition", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{"branch":"true"}`)),
		planNode("a", "agent.task", workflow.NodeReady, "all", nil, nil),
		planNode("b", "agent.task", workflow.NodeWaiting, "all", nil, nil),
	}
	edges := []workflow.EdgeRunState{
		planEdge("e1", "cond", "true", "a", "input", workflow.EdgeActivated),
		planEdge("e2", "cond", "false", "b", "input", workflow.EdgeSkipped),
		planEdge("e3", "a", "success", "b", "input", workflow.EdgePending),
	}
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.EdgeUpdates) != 0 || len(plan.NodeUpdates) != 0 {
		t.Fatalf("expected empty plan, got %+v", plan)
	}
}

func TestPlanAdvanceEndFiresOnAnyActivatedPort(t *testing.T) {
	// flow.end activation mode "any": one activated edge among all-resolved
	// inbound edges makes it ready.
	nodes := []workflow.NodeRunState{
		planNode("end", "flow.end", workflow.NodeWaiting, "any", nil, nil),
		planNode("s1", "agent.task", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{}`)),
		planNode("s2", "agent.task", workflow.NodeSkipped, "all", nil, nil),
	}
	edges := []workflow.EdgeRunState{
		planEdge("e1", "s1", "success", "end", "completed", workflow.EdgePending),
		planEdge("e2", "s2", "success", "end", "failed", workflow.EdgePending),
	}
	plan, err := workflow.PlanAdvance(nodes, edges)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertEdgePlan(t, plan, map[string]string{"e1": workflow.EdgeActivated, "e2": workflow.EdgeSkipped})
	assertNodePlan(t, plan, map[string]workflow.NodeLifecycleStatus{"end": workflow.NodeReady})
}

func TestPlanAdvanceErrors(t *testing.T) {
	// Edge referencing an unknown source node.
	nodes := []workflow.NodeRunState{planNode("a", "agent.task", workflow.NodeWaiting, "all", nil, nil)}
	edges := []workflow.EdgeRunState{planEdge("e1", "ghost", "success", "a", "input", workflow.EdgePending)}
	if _, err := workflow.PlanAdvance(nodes, edges); err == nil {
		t.Fatalf("unknown source node: expected error")
	}

	// Edge referencing an unknown target node.
	edges[0] = planEdge("e1", "a", "success", "ghost", "input", workflow.EdgePending)
	if _, err := workflow.PlanAdvance(nodes, edges); err == nil {
		t.Fatalf("unknown target node: expected error")
	}

	// Node of an unregistered type (empty definition).
	bad := []workflow.NodeRunState{{ID: "x", TypeKey: "nope", Status: workflow.NodeSucceeded, Definition: workflow.NodeDefinition{}}}
	if _, err := workflow.PlanAdvance(bad, nil); err == nil {
		t.Fatalf("unregistered type: expected error")
	}

	// Duplicate node id.
	dup := []workflow.NodeRunState{
		planNode("a", "agent.task", workflow.NodeWaiting, "all", nil, nil),
		planNode("a", "agent.task", workflow.NodeWaiting, "all", nil, nil),
	}
	if _, err := workflow.PlanAdvance(dup, nil); err == nil {
		t.Fatalf("duplicate node id: expected error")
	}

	// Succeeded condition without branch fails the plan.
	nodes2 := []workflow.NodeRunState{
		planNode("cond", "flow.condition", workflow.NodeSucceeded, "all", nil, json.RawMessage(`{}`)),
		planNode("a", "agent.task", workflow.NodeWaiting, "all", nil, nil),
	}
	edges2 := []workflow.EdgeRunState{planEdge("e1", "cond", "true", "a", "input", workflow.EdgePending)}
	if _, err := workflow.PlanAdvance(nodes2, edges2); err == nil {
		t.Fatalf("condition without branch: expected error")
	}
}

func assertEdgePlan(t *testing.T, plan workflow.AdvancePlan, want map[string]string) {
	t.Helper()
	got := map[string]string{}
	for _, u := range plan.EdgeUpdates {
		if _, dup := got[u.EdgeID]; dup {
			t.Fatalf("edge %s updated twice in one plan", u.EdgeID)
		}
		got[u.EdgeID] = u.Resolution
	}
	if len(got) != len(want) {
		t.Fatalf("edge updates = %v, want %v", got, want)
	}
	for id, res := range want {
		if got[id] != res {
			t.Fatalf("edge %s = %s, want %s (all: %v)", id, got[id], res, got)
		}
	}
}

func assertNodePlan(t *testing.T, plan workflow.AdvancePlan, want map[string]workflow.NodeLifecycleStatus) {
	t.Helper()
	got := map[string]workflow.NodeLifecycleStatus{}
	for _, u := range plan.NodeUpdates {
		if _, dup := got[u.NodeID]; dup {
			t.Fatalf("node %s updated twice in one plan", u.NodeID)
		}
		got[u.NodeID] = u.Status
	}
	if len(got) != len(want) {
		t.Fatalf("node updates = %v, want %v", got, want)
	}
	for id, status := range want {
		if got[id] != status {
			t.Fatalf("node %s = %s, want %s (all: %v)", id, got[id], status, got)
		}
	}
}
