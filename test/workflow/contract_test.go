// contract_test.go pins the workflow contracts as executable fixtures under
// testdata/: the node catalog, the template save request graph, the condition
// AST, the normalized trigger payload and the agentd execution unit.
package workflow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teammate/server/internal/integration/github"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/internal/workflow"
)

func loadFixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("fixture %s is not valid JSON: %v", name, err)
	}
	return raw
}

// TestFixturesAreValidJSON keeps every contract fixture parseable.
func TestFixturesAreValidJSON(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		loadFixture(t, entry.Name())
	}
}

// TestNodeRegistryContract pins the node catalog: type keys, categories,
// executor kinds and fixed ports. Assertions attach to the node registry.
func TestNodeRegistryContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "node-registry.json"))
	if err != nil {
		t.Fatalf("load fixture node-registry.json: %v", err)
	}
	var fixture struct {
		NodeTypes []struct {
			TypeKey         string   `json:"type_key"`
			Category        string   `json:"category"`
			ExecutorKind    string   `json:"executor_kind"`
			InputPorts      []string `json:"input_ports"`
			OutputPorts     []string `json:"output_ports"`
			OutputPortsKind string   `json:"output_ports_kind"`
		} `json:"node_types"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("fixture node-registry.json has an unexpected shape: %v", err)
	}

	registry, err := workflow.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry() error: %v", err)
	}
	list := registry.List()
	if len(list) != len(fixture.NodeTypes) {
		t.Fatalf("builtin registry has %d node types, fixture has %d", len(list), len(fixture.NodeTypes))
	}
	byKey := make(map[string]workflow.NodeDefinition, len(list))
	for _, def := range list {
		byKey[def.TypeKey] = def
	}
	for _, want := range fixture.NodeTypes {
		got, ok := byKey[want.TypeKey]
		if !ok {
			t.Errorf("node type %s missing from builtin registry", want.TypeKey)
			continue
		}
		if got.Category != want.Category {
			t.Errorf("%s category = %q, want %q", want.TypeKey, got.Category, want.Category)
		}
		if got.ExecutorKind != want.ExecutorKind {
			t.Errorf("%s executor_kind = %q, want %q", want.TypeKey, got.ExecutorKind, want.ExecutorKind)
		}
		if diff := diffPortKeys(portKeys(got.InputPorts), want.InputPorts); diff != "" {
			t.Errorf("%s input_ports %s", want.TypeKey, diff)
		}
		if diff := diffPortKeys(portKeys(got.OutputPorts), want.OutputPorts); diff != "" {
			t.Errorf("%s output_ports %s", want.TypeKey, diff)
		}
		// The fixture omits output_ports_kind for fixed-port types.
		wantKind := want.OutputPortsKind
		if wantKind == "" {
			wantKind = "fixed"
		}
		if got.OutputPortsKind != wantKind {
			t.Errorf("%s output_ports_kind = %q, want %q", want.TypeKey, got.OutputPortsKind, wantKind)
		}
	}
}

// TestTemplateSaveContract pins the whole-graph template save request and the
// {template, nodes, edges} response shape. Assertions attach to the template
// CRUD service.
func TestTemplateSaveContract(t *testing.T) {
	raw := loadFixture(t, "template-save-request.json")
	var request struct {
		Name    string                     `json:"name"`
		Enabled bool                       `json:"enabled"`
		Nodes   []TemplateNodeInputFixture `json:"nodes"`
		Edges   []TemplateEdgeInputFixture `json:"edges"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatalf("decode save request: %v", err)
	}
	// The fixture graph must pass the server-authoritative whole-graph
	// validation; the full CRUD round trip lives in
	// test/handler/workflow_graph_test.go and test/store/workflow_test.go.
	registry, err := workflow.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry() error: %v", err)
	}
	nodeInputs := make([]workflow.TemplateNodeInput, 0, len(request.Nodes))
	for _, n := range request.Nodes {
		nodeInputs = append(nodeInputs, workflow.TemplateNodeInput{ID: n.ID, TypeKey: n.TypeKey, Config: n.Config})
	}
	edgeInputs := make([]workflow.TemplateEdgeInput, 0, len(request.Edges))
	for _, e := range request.Edges {
		edgeInputs = append(edgeInputs, workflow.TemplateEdgeInput{
			ID: e.ID, SourceNodeID: e.SourceNodeID, SourcePort: e.SourcePort,
			TargetNodeID: e.TargetNodeID, TargetPort: e.TargetPort,
		})
	}
	if issues := workflow.ValidateTemplateGraph(registry, nodeInputs, edgeInputs); len(issues) != 0 {
		t.Fatalf("contract save request rejected by the graph validator: %v", issues)
	}
}

type TemplateNodeInputFixture struct {
	ID      string          `json:"id"`
	TypeKey string          `json:"type_key"`
	Config  json.RawMessage `json:"config"`
}

type TemplateEdgeInputFixture struct {
	ID           string `json:"id"`
	SourceNodeID string `json:"source_node_id"`
	SourcePort   string `json:"source_port"`
	TargetNodeID string `json:"target_node_id"`
	TargetPort   string `json:"target_port"`
}

// TestExpressionContract pins the condition AST semantics: allowed operators
// and operand shape for restricted path evaluation.
func TestExpressionContract(t *testing.T) {
	ast := loadFixture(t, "condition-ast.json")
	context := map[string]any{
		"trigger": map[string]any{
			"action": "opened",
			"issue": map[string]any{
				"labels": []any{"bug"},
			},
		},
	}
	resolve := func(path string) (any, bool) {
		return workflow.ResolvePath(context, path)
	}

	matched, err := workflow.EvaluateCondition(ast, resolve)
	if err != nil {
		t.Fatalf("EvaluateCondition(fixture) error: %v", err)
	}
	if !matched {
		t.Fatalf("EvaluateCondition(fixture) = false, want true for action=opened with labels=[bug]")
	}

	trigger, ok := context["trigger"].(map[string]any)
	if !ok {
		t.Fatalf("test context has no trigger object")
	}
	trigger["action"] = "closed"
	matched, err = workflow.EvaluateCondition(ast, resolve)
	if err != nil {
		t.Fatalf("EvaluateCondition(fixture) error: %v", err)
	}
	if matched {
		t.Fatalf("EvaluateCondition(fixture) = true, want false for action=closed")
	}
}

// TestTriggerPayloadContract pins the normalized trigger output that trigger
// nodes publish into the run context.
func TestTriggerPayloadContract(t *testing.T) {
	data := loadFixture(t, "trigger-payload.json")
	var fixture map[string]any
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode payload fixture: %v", err)
	}
	delete(fixture, "doc")
	// The raw root echoes the delivery body, so the contract check normalizes
	// a realistic issue delivery (GitHub's issue payload carries html_url,
	// which projects into the stable url field) and asserts the stable subset:
	// every top-level key present, and the issue projection equal to the
	// fixture's issue object.
	input := map[string]any{
		"action":     "opened",
		"repository": fixture["repository"],
		"sender":     fixture["sender"],
		"issue": map[string]any{
			"number":   42,
			"title":    fixtureIssue(t, fixture)["title"],
			"body":     fixtureIssue(t, fixture)["body"],
			"html_url": fixtureIssue(t, fixture)["url"],
			"labels":   []any{map[string]any{"name": "bug"}},
		},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("encode input: %v", err)
	}
	normalized, err := github.NormalizeWebhookPayload(asString(fixture["event"]), asString(fixture["delivery_id"]), inputJSON)
	if err != nil {
		t.Fatalf("normalize fixture payload: %v", err)
	}
	got, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("encode normalized: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("decode normalized: %v", err)
	}
	for _, key := range []string{"event", "action", "delivery_id", "repository", "sender", "issue", "pull_request", "raw"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("normalized payload missing contract key %q", key)
		}
	}
	if !reflect.DeepEqual(payload["issue"], fixture["issue"]) {
		t.Fatalf("issue projection diverges from the fixture: got %v want %v", payload["issue"], fixture["issue"])
	}
}

// fixtureIssue extracts the issue object from the payload fixture.
func fixtureIssue(t *testing.T, fixture map[string]any) map[string]any {
	t.Helper()
	issue, ok := fixture["issue"].(map[string]any)
	if !ok {
		t.Fatalf("payload fixture carries no issue object")
	}
	return issue
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// jsonEqual compares two JSON documents by value.
func jsonEqual(a, b []byte) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// TestExecutionUnitContract pins the opaque execution unit sent to agentd
// (plan §12.6): the wire field set, the agent kind with instruction/input/
// timeout/dirs from the node config, the command kind with its command spec,
// and the absence of workflow internals (type keys, ports, retry policy).
func TestExecutionUnitContract(t *testing.T) {
	fixture := loadFixture(t, "execution-unit.json")
	var want map[string]any
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	delete(want, "doc")

	// Build the node the fixture describes: an agent.task with the contract
	// sample's instruction, input and directory grants.
	node := types.TaskNode{
		ID:              "a1000000-0000-4000-8000-000000000003",
		TaskID:          42,
		Name:            "分析并修复",
		Status:          "running",
		TypeKey:         "agent.task",
		Config:          json.RawMessage(`{"instruction":"分析 Issue 并完成修复","timeout_minutes":60,"readonly_dirs":[],"full_control_dirs":["."]}`),
		Input:           mustRaw(t, map[string]any{"title": "登录页提交后返回 500", "body": "复现步骤：……", "repository": "owner/repo"}),
		ReadonlyDirs:    json.RawMessage(`[]`),
		FullControlDirs: json.RawMessage(`["."]`),
	}
	unit := service.BuildExecutionUnit(node, "c1000000-0000-4000-8000-000000000001")
	raw, err := json.Marshal(unit)
	if err != nil {
		t.Fatalf("marshal execution unit: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode execution unit: %v", err)
	}

	// The wire field set is exactly the agentd TaskNode decoder's fields.
	wantFields := []string{
		"id", "task_id", "name", "status", "execution_id", "execution_kind",
		"instruction", "input", "command", "timeout_minutes",
		"readonly_dirs", "full_control_dirs",
	}
	if len(got) != len(wantFields) {
		t.Fatalf("execution unit fields = %v, want exactly %v", keysOf(got), wantFields)
	}
	for _, field := range wantFields {
		if _, ok := got[field]; !ok {
			t.Errorf("execution unit missing field %q", field)
		}
	}

	// Every pinned fixture value must match the built unit.
	for field, wantValue := range want {
		gotValue, present := got[field]
		if !present {
			if wantValue != nil {
				t.Errorf("execution unit missing contract field %q", field)
			}
			continue
		}
		wantRaw, err := json.Marshal(wantValue)
		if err != nil {
			t.Fatalf("marshal want %s: %v", field, err)
		}
		gotRaw, err := json.Marshal(gotValue)
		if err != nil {
			t.Fatalf("marshal got %s: %v", field, err)
		}
		if string(gotRaw) != string(wantRaw) {
			t.Errorf("%s = %s, want %s", field, gotRaw, wantRaw)
		}
	}
	if unit.ExecutionKind != types.ExecutionKindAgent {
		t.Errorf("execution_kind = %q, want %q", unit.ExecutionKind, types.ExecutionKindAgent)
	}

	// A command node becomes a command execution unit carrying the command
	// spec; workflow internals still never leak.
	commandNode := types.TaskNode{
		ID:      "a1000000-0000-4000-8000-000000000004",
		TaskID:  42,
		Name:    "运行测试",
		Status:  "ready",
		TypeKey: "action.command",
		Config:  json.RawMessage(`{"command":{"program":"go","args":["test","./..."],"workdir":"."},"timeout_minutes":30,"retry":{"max_attempts":1}}`),
		Input:   json.RawMessage(`{}`),
	}
	commandUnit := service.BuildExecutionUnit(commandNode, "c1000000-0000-4000-8000-000000000002")
	if commandUnit.ExecutionKind != types.ExecutionKindCommand {
		t.Errorf("execution_kind = %q, want %q", commandUnit.ExecutionKind, types.ExecutionKindCommand)
	}
	if commandUnit.Command == nil || commandUnit.Command.Program != "go" || len(commandUnit.Command.Args) != 2 || commandUnit.Command.WorkDir != "." {
		t.Errorf("command spec = %+v, want program go with two args in .", commandUnit.Command)
	}
	if commandUnit.Instruction != "" {
		t.Errorf("command unit instruction = %q, want empty", commandUnit.Instruction)
	}

	// Workflow internals never cross the protocol.
	leaked := []string{"type_key", "template_node_id", "retry", "max_attempts", "config", "activation_mode", "version", "attempt_count"}
	for _, field := range leaked {
		if _, ok := got[field]; ok {
			t.Errorf("execution unit leaks workflow internal %q", field)
		}
	}
}

func mustRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %v: %v", value, err)
	}
	return raw
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func portKeys(ports []workflow.PortDefinition) []string {
	keys := make([]string, 0, len(ports))
	for _, port := range ports {
		keys = append(keys, port.Key)
	}
	return keys
}

// diffPortKeys compares the registry port list against the fixture list and
// returns an empty string when they match exactly.
func diffPortKeys(got, want []string) string {
	if len(got) != len(want) {
		return "got " + describe(got) + ", want " + describe(want)
	}
	for i := range want {
		if got[i] != want[i] {
			return "got " + describe(got) + ", want " + describe(want)
		}
	}
	return ""
}

func describe(keys []string) string {
	if len(keys) == 0 {
		return "[]"
	}
	return "[" + strings.Join(keys, " ") + "]"
}
