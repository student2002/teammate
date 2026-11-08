package workflow_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/teammate/server/internal/workflow"
)

// buildExampleGraph returns the plan §18 acceptance template as a valid graph:
// trigger.github → condition(labels contains bug) → agent.task →
// action.command → condition(exit_code == 0) → action.github(create PR) →
// flow.wait(review) → condition(approved) → action.create_task → flow.end,
// with the label-condition false branch going through an add_labels action.
// The two inner conditions route their false ports straight to flow.end's
// failed input, and the wait timeout port is intentionally unconnected.
func buildExampleGraph() ([]workflow.TemplateNodeInput, []workflow.TemplateEdgeInput) {
	nodes := []workflow.TemplateNodeInput{
		{ID: "n1", TypeKey: "trigger.github", Config: json.RawMessage(`{
			"integration_id": "11111111-1111-4111-8111-111111111111",
			"event": "issues",
			"actions": ["opened"]
		}`)},
		{ID: "n2", TypeKey: "flow.condition", Config: json.RawMessage(`{
			"condition": {"all": [{"path": "trigger.issue.labels", "op": "contains", "value": "bug"}]}
		}`)},
		{ID: "n3", TypeKey: "agent.task", Config: json.RawMessage(`{
			"instruction": "分析 Issue 并完成修复",
			"assignee": {"mode": "any_agent"},
			"input_map": {"title": "{{ trigger.issue.title }}"},
			"timeout_minutes": 60,
			"retry": {"max_attempts": 2, "backoff_seconds": 30},
			"readonly_dirs": [],
			"full_control_dirs": ["."]
		}`)},
		{ID: "n4", TypeKey: "action.command", Config: json.RawMessage(`{
			"command": {"program": "go", "args": ["test", "./..."], "workdir": "."}
		}`)},
		{ID: "n5", TypeKey: "flow.condition", Config: json.RawMessage(`{
			"condition": {"all": [{"path": "nodes.n4.output.exit_code", "op": "eq", "value": "0"}]}
		}`)},
		{ID: "n6", TypeKey: "action.github", Config: json.RawMessage(`{
			"operation": "create_pull_request",
			"title": "Fix #{{ trigger.issue.number }}",
			"head": "teammate/task-{{ task.id }}",
			"base": "main"
		}`)},
		{ID: "n7", TypeKey: "flow.wait", Config: json.RawMessage(`{
			"mode": "github_event",
			"event": "pull_request_review"
		}`)},
		{ID: "n8", TypeKey: "flow.condition", Config: json.RawMessage(`{
			"condition": {"any": [{"path": "nodes.n7.output.state", "op": "eq", "value": "approved"}]}
		}`)},
		{ID: "n9", TypeKey: "action.create_task", Config: json.RawMessage(`{"title": "处理 Review 意见"}`)},
		{ID: "n10", TypeKey: "flow.end", Config: json.RawMessage(`{}`)},
		{ID: "n11", TypeKey: "action.github", Config: json.RawMessage(`{
			"operation": "add_labels",
			"issue": "{{ trigger.issue.number }}",
			"labels": ["triage"]
		}`)},
	}
	edges := []workflow.TemplateEdgeInput{
		{ID: "e1", SourceNodeID: "n1", SourcePort: "success", TargetNodeID: "n2", TargetPort: "input"},
		{ID: "e2", SourceNodeID: "n2", SourcePort: "true", TargetNodeID: "n3", TargetPort: "input"},
		{ID: "e3", SourceNodeID: "n2", SourcePort: "false", TargetNodeID: "n11", TargetPort: "input"},
		{ID: "e4", SourceNodeID: "n3", SourcePort: "success", TargetNodeID: "n4", TargetPort: "input"},
		{ID: "e5", SourceNodeID: "n4", SourcePort: "success", TargetNodeID: "n5", TargetPort: "input"},
		{ID: "e6", SourceNodeID: "n5", SourcePort: "true", TargetNodeID: "n6", TargetPort: "input"},
		{ID: "e7", SourceNodeID: "n5", SourcePort: "false", TargetNodeID: "n10", TargetPort: "failed"},
		{ID: "e8", SourceNodeID: "n6", SourcePort: "success", TargetNodeID: "n7", TargetPort: "input"},
		{ID: "e9", SourceNodeID: "n7", SourcePort: "resumed", TargetNodeID: "n8", TargetPort: "input"},
		{ID: "e10", SourceNodeID: "n8", SourcePort: "true", TargetNodeID: "n9", TargetPort: "input"},
		{ID: "e11", SourceNodeID: "n8", SourcePort: "false", TargetNodeID: "n10", TargetPort: "failed"},
		{ID: "e12", SourceNodeID: "n9", SourcePort: "success", TargetNodeID: "n10", TargetPort: "completed"},
		{ID: "e13", SourceNodeID: "n11", SourcePort: "success", TargetNodeID: "n10", TargetPort: "completed"},
	}
	return nodes, edges
}

func mustRegistry(t *testing.T) *workflow.Registry {
	t.Helper()
	registry, err := workflow.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry() error: %v", err)
	}
	return registry
}

func validateExample(t *testing.T, nodes []workflow.TemplateNodeInput, edges []workflow.TemplateEdgeInput) []workflow.ValidationError {
	t.Helper()
	return workflow.ValidateTemplateGraph(mustRegistry(t), nodes, edges)
}

func errorMessages(errs []workflow.ValidationError) string {
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "; ")
}

func requireNoErrors(t *testing.T, errs []workflow.ValidationError) {
	t.Helper()
	if len(errs) != 0 {
		t.Fatalf("expected zero validation errors, got: %s", errorMessages(errs))
	}
}

func requireMessage(t *testing.T, errs []workflow.ValidationError, substr string) {
	t.Helper()
	for _, err := range errs {
		if strings.Contains(err.Message, substr) {
			return
		}
	}
	t.Fatalf("expected an error containing %q, got: %s", substr, errorMessages(errs))
}

func TestValidateTemplateGraphAcceptsPlanExample(t *testing.T) {
	nodes, edges := buildExampleGraph()
	requireNoErrors(t, validateExample(t, nodes, edges))
}

func TestValidateTemplateGraphMissingTrigger(t *testing.T) {
	nodes, edges := buildExampleGraph()
	filtered := nodes[:0]
	for _, n := range nodes {
		if n.TypeKey != "trigger.github" {
			filtered = append(filtered, n)
		}
	}
	keptEdges := edges[:0]
	for _, e := range edges {
		if e.SourceNodeID != "n1" && e.TargetNodeID != "n1" {
			keptEdges = append(keptEdges, e)
		}
	}
	errs := validateExample(t, filtered, keptEdges)
	requireMessage(t, errs, "missing trigger node")
}

func TestValidateTemplateGraphMultipleTriggers(t *testing.T) {
	nodes, edges := buildExampleGraph()
	nodes = append(nodes, workflow.TemplateNodeInput{ID: "n12", TypeKey: "trigger.manual", Config: json.RawMessage(`{}`)})
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "multiple trigger nodes")
}

func TestValidateTemplateGraphMissingEnd(t *testing.T) {
	nodes, edges := buildExampleGraph()
	filtered := nodes[:0]
	for _, n := range nodes {
		if n.TypeKey != "flow.end" {
			filtered = append(filtered, n)
		}
	}
	keptEdges := edges[:0]
	for _, e := range edges {
		if e.TargetNodeID != "n10" {
			keptEdges = append(keptEdges, e)
		}
	}
	errs := validateExample(t, filtered, keptEdges)
	requireMessage(t, errs, "missing flow.end node")
}

func TestValidateTemplateGraphUnknownTypeKey(t *testing.T) {
	nodes, edges := buildExampleGraph()
	nodes[1].TypeKey = "robot.think"
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "unknown type_key: robot.think")
}

func TestValidateTemplateGraphEdgeReferencesUnknownNode(t *testing.T) {
	nodes, edges := buildExampleGraph()
	edges = append(edges, workflow.TemplateEdgeInput{ID: "e14", SourceNodeID: "ghost", SourcePort: "success", TargetNodeID: "n2", TargetPort: "input"})
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "edge references unknown node")
}

func TestValidateTemplateGraphInvalidSourcePort(t *testing.T) {
	nodes, edges := buildExampleGraph()
	edges[0].SourcePort = "bogus"
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "invalid source port: bogus")
}

func TestValidateTemplateGraphInvalidTargetPort(t *testing.T) {
	nodes, edges := buildExampleGraph()
	edges[0].TargetPort = "bogus"
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "invalid target port: bogus")
}

func TestValidateTemplateGraphDuplicateEdge(t *testing.T) {
	nodes, edges := buildExampleGraph()
	duplicated := edges[0]
	duplicated.ID = "e14"
	edges = append(edges, duplicated)
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "duplicate edge")
}

func TestValidateTemplateGraphMultipleEdgesFromOneSourcePort(t *testing.T) {
	nodes, edges := buildExampleGraph()
	edges = append(edges, workflow.TemplateEdgeInput{ID: "e14", SourceNodeID: "n1", SourcePort: "success", TargetNodeID: "n3", TargetPort: "input"})
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "multiple edges from one source port")
}

func TestValidateTemplateGraphCycleDetected(t *testing.T) {
	nodes, edges := buildExampleGraph()
	nodes = append(nodes,
		workflow.TemplateNodeInput{ID: "n14", TypeKey: "data.set", Config: json.RawMessage(`{"name":"loop_a","value":1}`)},
		workflow.TemplateNodeInput{ID: "n15", TypeKey: "data.set", Config: json.RawMessage(`{"name":"loop_b","value":1}`)},
	)
	edges = append(edges,
		workflow.TemplateEdgeInput{ID: "e14", SourceNodeID: "n14", SourcePort: "success", TargetNodeID: "n15", TargetPort: "input"},
		workflow.TemplateEdgeInput{ID: "e15", SourceNodeID: "n15", SourcePort: "success", TargetNodeID: "n14", TargetPort: "input"},
	)
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "cycle detected")
}

func TestValidateTemplateGraphNodeNotReachableFromTrigger(t *testing.T) {
	nodes, edges := buildExampleGraph()
	nodes = append(nodes, workflow.TemplateNodeInput{ID: "n16", TypeKey: "data.set", Config: json.RawMessage(`{"name":"orphan","value":1}`)})
	edges = append(edges, workflow.TemplateEdgeInput{ID: "e14", SourceNodeID: "n16", SourcePort: "success", TargetNodeID: "n10", TargetPort: "completed"})
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "node not reachable from trigger")
	requireMessage(t, errs, "missing input edge")
}

func TestValidateTemplateGraphNodeCannotReachEnd(t *testing.T) {
	nodes, edges := buildExampleGraph()
	nodes = append(nodes, workflow.TemplateNodeInput{ID: "n17", TypeKey: "data.set", Config: json.RawMessage(`{"name":"dead_end","value":1}`)})
	edges = append(edges, workflow.TemplateEdgeInput{ID: "e14", SourceNodeID: "n7", SourcePort: "timeout", TargetNodeID: "n17", TargetPort: "input"})
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "node cannot reach flow.end")
	requireMessage(t, errs, "missing output edge")
}

func TestValidateTemplateGraphInvalidConfig(t *testing.T) {
	nodes, edges := buildExampleGraph()
	nodes[2].Config = json.RawMessage(`{}`)
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "invalid config: instruction is required")
}

// TestValidateTemplateGraphConditionAllAnyConflict rejects a flow.condition
// node whose condition fills both all and any groups (save passes, run fails).
func TestValidateTemplateGraphConditionAllAnyConflict(t *testing.T) {
	nodes := []workflow.TemplateNodeInput{
		{ID: "c1", TypeKey: "trigger.manual", Config: json.RawMessage(`{}`)},
		{ID: "c2", TypeKey: "flow.condition", Config: json.RawMessage(`{
			"condition": {
				"all": [{"path": "trigger.a", "op": "eq", "value": "1"}],
				"any": [{"path": "trigger.b", "op": "eq", "value": "2"}]
			}
		}`)},
		{ID: "c3", TypeKey: "flow.end", Config: json.RawMessage(`{}`)},
	}
	edges := []workflow.TemplateEdgeInput{
		{ID: "ce1", SourceNodeID: "c1", SourcePort: "success", TargetNodeID: "c2", TargetPort: "input"},
		{ID: "ce2", SourceNodeID: "c2", SourcePort: "true", TargetNodeID: "c3", TargetPort: "completed"},
		{ID: "ce3", SourceNodeID: "c2", SourcePort: "false", TargetNodeID: "c3", TargetPort: "failed"},
	}
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "condition all and any cannot be combined")
}

func TestValidateTemplateGraphSwitchDerivedPorts(t *testing.T) {
	nodes := []workflow.TemplateNodeInput{
		{ID: "s1", TypeKey: "trigger.manual", Config: json.RawMessage(`{}`)},
		{ID: "s2", TypeKey: "flow.switch", Config: json.RawMessage(`{"rules":[
			{"branch":"alpha","condition":{"all":[{"path":"trigger.k","op":"eq","value":"a"}]}},
			{"branch":"beta","condition":{"any":[{"path":"trigger.k","op":"eq","value":"b"}]}}
		]}`)},
		{ID: "s3", TypeKey: "flow.join", Config: json.RawMessage(`{"mode":"any"}`)},
		{ID: "s4", TypeKey: "flow.end", Config: json.RawMessage(`{}`)},
	}
	edges := []workflow.TemplateEdgeInput{
		{ID: "se1", SourceNodeID: "s1", SourcePort: "success", TargetNodeID: "s2", TargetPort: "input"},
		{ID: "se2", SourceNodeID: "s2", SourcePort: "alpha", TargetNodeID: "s3", TargetPort: "input"},
		{ID: "se3", SourceNodeID: "s2", SourcePort: "beta", TargetNodeID: "s3", TargetPort: "input"},
		{ID: "se4", SourceNodeID: "s2", SourcePort: "default", TargetNodeID: "s4", TargetPort: "failed"},
		{ID: "se5", SourceNodeID: "s3", SourcePort: "success", TargetNodeID: "s4", TargetPort: "completed"},
	}
	requireNoErrors(t, validateExample(t, nodes, edges))

	edges[1].SourcePort = "gamma"
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "invalid source port: gamma")
}

func TestValidateTemplateGraphParallelDerivedPorts(t *testing.T) {
	nodes := []workflow.TemplateNodeInput{
		{ID: "p1", TypeKey: "trigger.manual", Config: json.RawMessage(`{}`)},
		{ID: "p2", TypeKey: "flow.parallel", Config: json.RawMessage(`{"branches":["alpha","beta"]}`)},
		{ID: "p3", TypeKey: "flow.join", Config: json.RawMessage(`{"mode":"all"}`)},
		{ID: "p4", TypeKey: "flow.end", Config: json.RawMessage(`{}`)},
	}
	edges := []workflow.TemplateEdgeInput{
		{ID: "pe1", SourceNodeID: "p1", SourcePort: "success", TargetNodeID: "p2", TargetPort: "input"},
		{ID: "pe2", SourceNodeID: "p2", SourcePort: "alpha", TargetNodeID: "p3", TargetPort: "input"},
		{ID: "pe3", SourceNodeID: "p2", SourcePort: "beta", TargetNodeID: "p3", TargetPort: "input"},
		{ID: "pe4", SourceNodeID: "p3", SourcePort: "success", TargetNodeID: "p4", TargetPort: "completed"},
	}
	requireNoErrors(t, validateExample(t, nodes, edges))

	edges[2].SourcePort = "default"
	errs := validateExample(t, nodes, edges)
	requireMessage(t, errs, "invalid source port: default")
}

func TestValidationErrorAttribution(t *testing.T) {
	nodes, edges := buildExampleGraph()
	edges[0].SourcePort = "bogus"
	errs := validateExample(t, nodes, edges)
	for _, err := range errs {
		if strings.Contains(err.Message, "invalid source port") && err.EdgeID != "e1" {
			t.Errorf("source port error attributed to edge %q, want e1", err.EdgeID)
		}
	}
	requireMessage(t, errs, "invalid source port")
}

func TestValidationErrorErrorMethod(t *testing.T) {
	nodeErr := workflow.ValidationError{NodeID: "n1", Message: "boom"}
	if got, want := nodeErr.Error(), "boom (node n1)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	edgeErr := workflow.ValidationError{EdgeID: "e1", Message: "boom"}
	if got, want := edgeErr.Error(), "boom (edge e1)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	graphErr := workflow.ValidationError{Message: "boom"}
	if got, want := graphErr.Error(), "boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestValidateTemplateGraphTriggerScheduleConfig(t *testing.T) {
	valid := func() (workflow.TemplateNodeInput, workflow.TemplateEdgeInput, workflow.TemplateNodeInput, workflow.TemplateEdgeInput) {
		trigger := workflow.TemplateNodeInput{
			ID: "11111111-1111-4111-8111-111111111111", TypeKey: "trigger.schedule",
			Config: json.RawMessage(`{"interval_minutes":30,"project_id":"22222222-2222-4222-8222-222222222222"}`),
		}
		end := workflow.TemplateNodeInput{ID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", TypeKey: "flow.end"}
		edge := workflow.TemplateEdgeInput{ID: "edge-1", SourceNodeID: trigger.ID, SourcePort: "success", TargetNodeID: end.ID, TargetPort: "completed"}
		return trigger, edge, end, edge
	}

	trigger, edge, end, _ := valid()
	if issues := validateExample(t, []workflow.TemplateNodeInput{trigger, end}, []workflow.TemplateEdgeInput{edge}); len(issues) != 0 {
		t.Fatalf("valid schedule template rejected: %v", issues)
	}

	trigger, edge, end, _ = valid()
	trigger.Config = json.RawMessage(`{"interval_minutes":30}`)
	if issues := validateExample(t, []workflow.TemplateNodeInput{trigger, end}, []workflow.TemplateEdgeInput{edge}); len(issues) == 0 {
		t.Fatalf("schedule without project_id accepted")
	}

	trigger, edge, end, _ = valid()
	trigger.Config = json.RawMessage(`{"interval_minutes":30,"project_id":"not-a-uuid"}`)
	if issues := validateExample(t, []workflow.TemplateNodeInput{trigger, end}, []workflow.TemplateEdgeInput{edge}); len(issues) == 0 {
		t.Fatalf("schedule with a non-UUID project_id accepted")
	}
}
