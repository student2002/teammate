// Package workflow_test contains the pure-logic tests of the workflow
// package (plan §17.1): node registry, template graph validation and the
// expression evaluator.
package workflow_test

import (
	"encoding/json"
	"testing"

	"github.com/teammate/server/internal/workflow"
)

// builtinCatalog is the expected built-in node catalog (plan §2.1), pinned
// byte-for-byte against test/workflow/testdata/node-registry.json.
var builtinCatalog = []workflow.NodeDefinition{
	{TypeKey: "trigger.manual", Category: "trigger", ExecutorKind: "server"},
	{TypeKey: "trigger.schedule", Category: "trigger", ExecutorKind: "server"},
	{TypeKey: "trigger.github", Category: "trigger", ExecutorKind: "server"},
	{TypeKey: "trigger.webhook", Category: "trigger", ExecutorKind: "server"},
	{TypeKey: "agent.task", Category: "agent", ExecutorKind: "agentd"},
	{TypeKey: "action.github", Category: "integration", ExecutorKind: "server"},
	{TypeKey: "action.http", Category: "integration", ExecutorKind: "server"},
	{TypeKey: "action.command", Category: "action", ExecutorKind: "agentd"},
	{TypeKey: "action.create_task", Category: "task", ExecutorKind: "server"},
	{TypeKey: "workflow.call", Category: "reuse", ExecutorKind: "server"},
	{TypeKey: "data.set", Category: "data", ExecutorKind: "server"},
	{TypeKey: "data.map", Category: "data", ExecutorKind: "server"},
	{TypeKey: "flow.condition", Category: "logic", ExecutorKind: "server"},
	{TypeKey: "flow.switch", Category: "logic", ExecutorKind: "server"},
	{TypeKey: "flow.parallel", Category: "logic", ExecutorKind: "server"},
	{TypeKey: "flow.join", Category: "logic", ExecutorKind: "server"},
	{TypeKey: "flow.wait", Category: "wait", ExecutorKind: "server"},
	{TypeKey: "human.input", Category: "human", ExecutorKind: "human"},
	{TypeKey: "flow.end", Category: "end", ExecutorKind: "server"},
}

// expectedPorts pairs each type key with its fixed input and output port keys.
var expectedPorts = map[string]struct {
	inputs  []string
	outputs []string
}{
	"trigger.manual":     {nil, []string{"success"}},
	"trigger.schedule":   {nil, []string{"success"}},
	"trigger.github":     {nil, []string{"success"}},
	"trigger.webhook":    {nil, []string{"success"}},
	"agent.task":         {[]string{"input"}, []string{"success", "failed"}},
	"action.github":      {[]string{"input"}, []string{"success", "failed"}},
	"action.http":        {[]string{"input"}, []string{"success", "failed"}},
	"action.command":     {[]string{"input"}, []string{"success", "failed"}},
	"action.create_task": {[]string{"input"}, []string{"success", "failed"}},
	"workflow.call":      {[]string{"input"}, []string{"success", "failed"}},
	"data.set":           {[]string{"input"}, []string{"success", "failed"}},
	"data.map":           {[]string{"input"}, []string{"success", "failed"}},
	"flow.condition":     {[]string{"input"}, []string{"true", "false"}},
	"flow.switch":        {[]string{"input"}, nil},
	"flow.parallel":      {[]string{"input"}, nil},
	"flow.join":          {[]string{"input"}, []string{"success"}},
	"flow.wait":          {[]string{"input"}, []string{"resumed", "timeout"}},
	"human.input":        {[]string{"input"}, []string{"submitted", "cancelled", "timeout"}},
	"flow.end":           {[]string{"completed", "failed"}, nil},
}

// expectedOutputPortsKind pins the output_ports_kind of every built-in type.
var expectedOutputPortsKind = map[string]string{
	"flow.switch":   workflow.OutputPortsSwitchBranches,
	"flow.parallel": workflow.OutputPortsParallelBranches,
}

func TestBuiltinRegistryContainsFullCatalog(t *testing.T) {
	registry, err := workflow.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry() error: %v", err)
	}
	list := registry.List()
	if len(list) != len(builtinCatalog) {
		t.Fatalf("builtin registry has %d node types, want %d", len(list), len(builtinCatalog))
	}
	byKey := make(map[string]workflow.NodeDefinition, len(list))
	for _, def := range list {
		byKey[def.TypeKey] = def
	}
	for _, want := range builtinCatalog {
		got, ok := byKey[want.TypeKey]
		if !ok {
			t.Errorf("missing node type %s", want.TypeKey)
			continue
		}
		if got.Category != want.Category {
			t.Errorf("%s category = %q, want %q", want.TypeKey, got.Category, want.Category)
		}
		if got.ExecutorKind != want.ExecutorKind {
			t.Errorf("%s executor_kind = %q, want %q", want.TypeKey, got.ExecutorKind, want.ExecutorKind)
		}
		ports, ok := expectedPorts[want.TypeKey]
		if !ok {
			t.Fatalf("test catalog missing port expectation for %s", want.TypeKey)
		}
		if diff := comparePortKeys(got.InputPorts, ports.inputs); diff != "" {
			t.Errorf("%s input ports %s", want.TypeKey, diff)
		}
		if diff := comparePortKeys(got.OutputPorts, ports.outputs); diff != "" {
			t.Errorf("%s output ports %s", want.TypeKey, diff)
		}
		kind := got.OutputPortsKind
		if kind == "" {
			kind = workflow.OutputPortsFixed
		}
		if wantKind, ok := expectedOutputPortsKind[want.TypeKey]; ok {
			if kind != wantKind {
				t.Errorf("%s output_ports_kind = %q, want %q", want.TypeKey, kind, wantKind)
			}
		} else if kind != workflow.OutputPortsFixed {
			t.Errorf("%s output_ports_kind = %q, want %q", want.TypeKey, kind, workflow.OutputPortsFixed)
		}
		if got.Validate == nil {
			t.Errorf("%s has no Validate function", want.TypeKey)
		}
	}
}

func comparePortKeys(got []workflow.PortDefinition, want []string) string {
	gotKeys := make([]string, 0, len(got))
	for _, port := range got {
		gotKeys = append(gotKeys, port.Key)
	}
	if len(gotKeys) != len(want) {
		return "got " + jsonDump(gotKeys) + ", want " + jsonDump(want)
	}
	for i := range want {
		if gotKeys[i] != want[i] {
			return "got " + jsonDump(gotKeys) + ", want " + jsonDump(want)
		}
	}
	return ""
}

func jsonDump(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestRegistryRegisterDuplicateTypeKey(t *testing.T) {
	registry := workflow.NewRegistry()
	first := workflow.NodeDefinition{TypeKey: "custom.node", Category: "logic", ExecutorKind: "server"}
	if err := registry.Register(first); err != nil {
		t.Fatalf("first Register() error: %v", err)
	}
	if err := registry.Register(first); err == nil {
		t.Fatalf("duplicate Register() expected an error")
	}
}

func TestMustRegisterPanicsOnDuplicate(t *testing.T) {
	registry := workflow.NewRegistry()
	registry.MustRegister(workflow.NodeDefinition{TypeKey: "custom.node"})
	defer func() {
		if recover() == nil {
			t.Fatalf("MustRegister() on duplicate type_key expected a panic")
		}
	}()
	registry.MustRegister(workflow.NodeDefinition{TypeKey: "custom.node"})
}

func TestRegistryGetUnknownTypeKey(t *testing.T) {
	registry, err := workflow.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry() error: %v", err)
	}
	if _, ok := registry.Get("no.such"); ok {
		t.Fatalf("Get(unknown) expected ok=false")
	}
	if _, ok := registry.Get("agent.task"); !ok {
		t.Fatalf("Get(agent.task) expected ok=true")
	}
}

func TestRegistryListSortedByTypeKey(t *testing.T) {
	registry := workflow.NewRegistry()
	for _, key := range []string{"z.node", "a.node", "m.node"} {
		registry.MustRegister(workflow.NodeDefinition{TypeKey: key})
	}
	list := registry.List()
	want := []string{"a.node", "m.node", "z.node"}
	if len(list) != len(want) {
		t.Fatalf("List() length = %d, want %d", len(list), len(want))
	}
	for i, def := range list {
		if def.TypeKey != want[i] {
			t.Errorf("List()[%d] = %q, want %q", i, def.TypeKey, want[i])
		}
	}
}

func TestOutputPortsFixedIgnoresConfig(t *testing.T) {
	registry, err := workflow.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry() error: %v", err)
	}
	ports, err := registry.OutputPorts("agent.task", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("OutputPorts(agent.task) error: %v", err)
	}
	if diff := comparePortKeys(ports, []string{"success", "failed"}); diff != "" {
		t.Errorf("agent.task output ports %s", diff)
	}
}

func TestOutputPortsSwitchBranchesAppendsDefault(t *testing.T) {
	registry, err := workflow.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry() error: %v", err)
	}
	// No rules → only the default port.
	ports, err := registry.OutputPorts("flow.switch", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("OutputPorts(flow.switch) error: %v", err)
	}
	if diff := comparePortKeys(ports, []string{"default"}); diff != "" {
		t.Errorf("flow.switch empty rules output ports %s", diff)
	}
	config := `{"rules":[
		{"branch":"alpha","condition":{"all":[{"path":"trigger.k","op":"eq","value":"a"}]}},
		{"branch":"beta","condition":{"any":[{"path":"trigger.k","op":"eq","value":"b"}]}}
	]}`
	ports, err = registry.OutputPorts("flow.switch", json.RawMessage(config))
	if err != nil {
		t.Fatalf("OutputPorts(flow.switch rules) error: %v", err)
	}
	if diff := comparePortKeys(ports, []string{"alpha", "beta", "default"}); diff != "" {
		t.Errorf("flow.switch output ports %s", diff)
	}
}

func TestOutputPortsParallelBranches(t *testing.T) {
	registry, err := workflow.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry() error: %v", err)
	}
	ports, err := registry.OutputPorts("flow.parallel", json.RawMessage(`{"branches":["alpha","beta"]}`))
	if err != nil {
		t.Fatalf("OutputPorts(flow.parallel) error: %v", err)
	}
	if diff := comparePortKeys(ports, []string{"alpha", "beta"}); diff != "" {
		t.Errorf("flow.parallel output ports %s", diff)
	}
}

func TestOutputPortsInvalidConfigs(t *testing.T) {
	registry, err := workflow.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry() error: %v", err)
	}
	cases := []struct {
		name   string
		config string
	}{
		{"non-array rules", `{"rules":"alpha"}`},
		{"rule not object", `{"rules":["alpha"]}`},
		{"missing branch", `{"rules":[{"condition":{"all":[]}}]}`},
		{"empty branch name", `{"rules":[{"branch":" ","condition":{"all":[]}}]}`},
		{"reserved branch name", `{"rules":[{"branch":"default","condition":{"all":[]}}]}`},
		{"duplicate branches", `{"rules":[
			{"branch":"alpha","condition":{"all":[]}},
			{"branch":"alpha","condition":{"all":[]}}
		]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := registry.OutputPorts("flow.switch", json.RawMessage(tc.config)); err == nil {
				t.Fatalf("OutputPorts with %s expected an error", tc.name)
			}
		})
	}
	if _, err := registry.OutputPorts("no.such", json.RawMessage(`{}`)); err == nil {
		t.Fatalf("OutputPorts(unknown type_key) expected an error")
	}
}
