// sync_exec_test.go covers the synchronous node executor: data.set/data.map
// variable writes with expression rendering, condition/switch branch
// evaluation and error cases.
package workflow_test

import (
	"encoding/json"
	"testing"

	"github.com/teammate/server/internal/workflow"
)

// syncContext is a minimal run context {trigger, vars} for resolve/applyVar.
func syncContext() (map[string]any, func(string) (any, bool), func(string, any)) {
	root := map[string]any{
		"trigger": map[string]any{"issue": map[string]any{"number": float64(7), "title": "bug found"}},
		"vars":    map[string]any{},
	}
	resolve := func(path string) (any, bool) { return workflow.ResolvePath(root, path) }
	applyVar := func(name string, value any) { root["vars"].(map[string]any)[name] = value }
	return root, resolve, applyVar
}

func TestExecuteSyncNodeDataSet(t *testing.T) {
	root, resolve, applyVar := syncContext()

	// Pure placeholder keeps the JSON type.
	output, branch, err := workflow.ExecuteSyncNode("data.set", json.RawMessage(`{"name":"issue_number","value":"{{ trigger.issue.number }}"}`), resolve, applyVar)
	if err != nil {
		t.Fatalf("data.set: %v", err)
	}
	if branch != "success" || string(output) != "{}" {
		t.Fatalf("output = %s, branch = %s", output, branch)
	}

	// Mixed text renders as string.
	_, _, err = workflow.ExecuteSyncNode("data.set", json.RawMessage(`{"name":"label","value":"issue-{{ trigger.issue.number }}"}`), resolve, applyVar)
	if err != nil {
		t.Fatalf("data.set mixed: %v", err)
	}

	// Unresolved placeholder stays literal.
	if _, _, err := workflow.ExecuteSyncNode("data.set", json.RawMessage(`{"name":"missing","value":"{{ vars.nope }}"}`), resolve, applyVar); err != nil {
		t.Fatalf("data.set unresolved: %v", err)
	}

	vars := root["vars"].(map[string]any)
	if vars["issue_number"] != float64(7) {
		t.Fatalf("issue_number = %#v, want 7", vars["issue_number"])
	}
	if vars["label"] != "issue-7" {
		t.Fatalf("label = %#v, want issue-7", vars["label"])
	}
	if vars["missing"] != "{{ vars.nope }}" {
		t.Fatalf("missing = %#v, want literal placeholder", vars["missing"])
	}
}

func TestExecuteSyncNodeDataSetNonStringValue(t *testing.T) {
	root, resolve, applyVar := syncContext()
	if _, _, err := workflow.ExecuteSyncNode("data.set", json.RawMessage(`{"name":"flag","value":true}`), resolve, applyVar); err != nil {
		t.Fatalf("data.set bool: %v", err)
	}
	vars := root["vars"].(map[string]any)
	if vars["flag"] != true {
		t.Fatalf("flag = %#v, want true", vars["flag"])
	}
}

func TestExecuteSyncNodeDataMap(t *testing.T) {
	root, resolve, applyVar := syncContext()
	config := json.RawMessage(`{
		"mappings": [
			{"target": "title", "source": "{{ trigger.issue.title }}"},
			{"target": "number", "source": "{{ trigger.issue.number }}"},
			{"target": "empty", "source": null}
		]
	}`)
	if _, _, err := workflow.ExecuteSyncNode("data.map", config, resolve, applyVar); err != nil {
		t.Fatalf("data.map: %v", err)
	}
	vars := root["vars"].(map[string]any)
	if vars["title"] != "bug found" || vars["number"] != float64(7) || vars["empty"] != nil {
		t.Fatalf("vars = %#v", vars)
	}
}

func TestExecuteSyncNodeCondition(t *testing.T) {
	_, resolve, _ := syncContext()

	config := json.RawMessage(`{"condition": {"all": [{"path": "trigger.issue.number", "op": "gte", "value": 5}]}}`)
	output, branch, err := workflow.ExecuteSyncNode("flow.condition", config, resolve, nil)
	if err != nil {
		t.Fatalf("condition true: %v", err)
	}
	if branch != "true" {
		t.Fatalf("branch = %s, want true", branch)
	}
	var decoded map[string]string
	if err := json.Unmarshal(output, &decoded); err != nil || decoded["branch"] != "true" {
		t.Fatalf("output = %s, want {\"branch\":\"true\"}", output)
	}

	config = json.RawMessage(`{"condition": {"all": [{"path": "trigger.issue.number", "op": "lt", "value": 5}]}}`)
	_, branch, err = workflow.ExecuteSyncNode("flow.condition", config, resolve, nil)
	if err != nil {
		t.Fatalf("condition false: %v", err)
	}
	if branch != "false" {
		t.Fatalf("branch = %s, want false", branch)
	}
}

func TestExecuteSyncNodeSwitch(t *testing.T) {
	_, resolve, _ := syncContext()

	// No rule matches → default.
	config := json.RawMessage(`{"rules": [
		{"branch": "bug found", "condition": {"all": [{"path": "trigger.issue.title", "op": "contains", "value": "crash"}]}},
		{"branch": "other", "condition": {"all": [{"path": "trigger.issue.title", "op": "contains", "value": "typo"}]}}
	]}`)
	if _, branch, err := workflow.ExecuteSyncNode("flow.switch", config, resolve, nil); err != nil || branch != "default" {
		t.Fatalf("switch unmatched = %s, err = %v, want default", branch, err)
	}

	// First matching rule wins.
	config = json.RawMessage(`{"rules": [
		{"branch": "bug found", "condition": {"all": [{"path": "trigger.issue.title", "op": "contains", "value": "bug"}]}},
		{"branch": "other", "condition": {"all": [{"path": "trigger.issue.title", "op": "neq", "value": ""}]}}
	]}`)
	output, branch, err := workflow.ExecuteSyncNode("flow.switch", config, resolve, nil)
	if err != nil || branch != "bug found" {
		t.Fatalf("switch matched = %s, err = %v", branch, err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(output, &decoded); err != nil || decoded["branch"] != "bug found" {
		t.Fatalf("output = %s", output)
	}

	// A later rule matches when the first does not.
	config = json.RawMessage(`{"rules": [
		{"branch": "a", "condition": {"all": [{"path": "trigger.issue.title", "op": "eq", "value": "nope"}]}},
		{"branch": "b", "condition": {"any": [{"path": "trigger.issue.number", "op": "gte", "value": 1}]}}
	]}`)
	if _, branch, err = workflow.ExecuteSyncNode("flow.switch", config, resolve, nil); err != nil || branch != "b" {
		t.Fatalf("switch second rule = %s, err = %v", branch, err)
	}

	// Empty rules fall through to default.
	config = json.RawMessage(`{"rules": []}`)
	if _, branch, err := workflow.ExecuteSyncNode("flow.switch", config, resolve, nil); err != nil || branch != "default" {
		t.Fatalf("switch empty = %s, err = %v, want default", branch, err)
	}
}

func TestExecuteSyncNodeTrivialTypes(t *testing.T) {
	_, resolve, _ := syncContext()
	for _, typeKey := range []string{"flow.parallel", "flow.join"} {
		config := json.RawMessage(`{}`)
		if typeKey == "flow.parallel" {
			config = json.RawMessage(`{"branches":["p1"]}`)
		}
		output, branch, err := workflow.ExecuteSyncNode(typeKey, config, resolve, nil)
		if err != nil || branch != "success" || string(output) != "{}" {
			t.Fatalf("%s: output = %s, branch = %s, err = %v", typeKey, output, branch, err)
		}
	}
}

func TestExecuteSyncNodeErrors(t *testing.T) {
	_, resolve, _ := syncContext()

	if _, _, err := workflow.ExecuteSyncNode("data.set", json.RawMessage(`{}`), resolve, nil); err == nil {
		t.Fatalf("data.set without name: expected error")
	}
	if _, _, err := workflow.ExecuteSyncNode("data.map", json.RawMessage(`{"mappings": "nope"}`), resolve, nil); err == nil {
		t.Fatalf("data.map bad mappings: expected error")
	}
	if _, _, err := workflow.ExecuteSyncNode("flow.condition", json.RawMessage(`{}`), resolve, nil); err == nil {
		t.Fatalf("condition without condition: expected error")
	}
	if _, _, err := workflow.ExecuteSyncNode("flow.switch", json.RawMessage(`{"rules": "nope"}`), resolve, nil); err == nil {
		t.Fatalf("switch with non-array rules: expected error")
	}
	if _, _, err := workflow.ExecuteSyncNode("flow.switch", json.RawMessage(`{"rules": [{"branch": "a"}]}`), resolve, nil); err == nil {
		t.Fatalf("switch rule without condition: expected error")
	}
	if _, _, err := workflow.ExecuteSyncNode("agent.task", json.RawMessage(`{}`), resolve, nil); err == nil {
		t.Fatalf("agent.task is not synchronous: expected error")
	}
}

func TestIsSyncNode(t *testing.T) {
	for _, typeKey := range []string{"data.set", "data.map", "flow.condition", "flow.switch", "flow.parallel", "flow.join"} {
		if !workflow.IsSyncNode(typeKey) {
			t.Fatalf("%s should be a sync node", typeKey)
		}
	}
	for _, typeKey := range []string{"agent.task", "flow.end", "flow.wait", "human.input", "action.http"} {
		if workflow.IsSyncNode(typeKey) {
			t.Fatalf("%s should not be a sync node", typeKey)
		}
	}
}
