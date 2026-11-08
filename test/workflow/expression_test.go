package workflow_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/teammate/server/internal/workflow"
)

func testContext() map[string]any {
	return map[string]any{
		"trigger": map[string]any{
			"action": "opened",
			"issue": map[string]any{
				"number": float64(42),
				"title":  "bug found",
				"labels": []any{"bug", "ui"},
				"body":   nil,
			},
		},
		"vars": map[string]any{
			"count":  float64(3),
			"ratio":  1.5,
			"flag":   true,
			"empty":  "",
			"number": "42",
			"list":   []any{float64(1), float64(2), float64(3)},
			"object": map[string]any{"k": "v"},
		},
	}
}

func testResolve(path string) (any, bool) {
	return workflow.ResolvePath(testContext(), path)
}

func TestResolvePathObjectAndArray(t *testing.T) {
	ctx := testContext()
	cases := []struct {
		path string
		want any
	}{
		{"trigger.action", "opened"},
		{"trigger.issue.title", "bug found"},
		{"trigger.issue.number", float64(42)},
		{"trigger.issue.labels[0]", "bug"},
		{"trigger.issue.labels[1]", "ui"},
		{"vars.list[2]", float64(3)},
		{"vars.object.k", "v"},
		{"trigger.issue.body", nil},
	}
	for _, tc := range cases {
		got, ok := workflow.ResolvePath(ctx, tc.path)
		if !ok {
			t.Errorf("ResolvePath(%q) reported not-found, want %v", tc.path, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("ResolvePath(%q) = %#v, want %#v", tc.path, got, tc.want)
		}
	}
}

func TestResolvePathNotFound(t *testing.T) {
	ctx := testContext()
	paths := []string{
		"",                         // empty path
		"trigger.missing",          // missing key
		"trigger.issue.x.y",        // missing nested key
		"trigger..action",          // empty segment
		".trigger",                 // leading empty segment
		"trigger.",                 // trailing empty segment
		"trigger.issue.labels[5]",  // out of bounds
		"trigger.issue.labels[-1]", // negative index
		"trigger.issue.labels[a]",  // non-numeric index
		"trigger.issue.labels[0",   // unclosed bracket
		"trigger.issue.number[0]",  // index into a scalar
		"trigger.issue.title.x",    // traversal into a scalar
		"foo()",                    // function-call style is not a path
		"vars.list[0][0]",          // index into a scalar element
	}
	for _, path := range paths {
		if _, ok := workflow.ResolvePath(ctx, path); ok {
			t.Errorf("ResolvePath(%q) reported found, want not-found", path)
		}
	}
	if _, ok := workflow.ResolvePath(ctx, "trigger"); !ok {
		t.Errorf("ResolvePath(trigger) reported not-found, want found")
	}
	scalarRoot, ok := workflow.ResolvePath(float64(1), "x")
	if ok || scalarRoot != nil {
		t.Errorf("ResolvePath on scalar root = (%v, %v), want (nil, false)", scalarRoot, ok)
	}
}

func TestRenderTextPurePlaceholderKeepsJSONType(t *testing.T) {
	cases := []struct {
		text string
		want any
	}{
		{"{{ trigger.action }}", "opened"},
		{"{{ trigger.issue.number }}", float64(42)},
		{"{{ vars.flag }}", true},
		{"{{ vars.empty }}", ""},
		{"{{ trigger.issue.body }}", nil},
		{"{{ vars.object }}", map[string]any{"k": "v"}},
		{"{{ vars.list }}", []any{float64(1), float64(2), float64(3)}},
		{"{{trigger.issue.labels[0]}}", "bug"},
	}
	for _, tc := range cases {
		got := workflow.RenderText(tc.text, testResolve)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("RenderText(%q) = %#v, want %#v", tc.text, got, tc.want)
		}
	}
}

func TestRenderTextPurePlaceholderUnresolvedReturnsOriginal(t *testing.T) {
	text := "{{ vars.missing }}"
	if got := workflow.RenderText(text, testResolve); got != text {
		t.Errorf("RenderText(%q) = %#v, want the original text", text, got)
	}
}

func TestRenderTextMixedConcatenation(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"修复 Issue #{{ trigger.issue.number }}：{{ trigger.issue.title }}", "修复 Issue #42：bug found"},
		{"flag={{ vars.flag }}", "flag=true"},
		{"ratio={{ vars.ratio }}!", "ratio=1.5!"},
		{"list={{ vars.list }}", "list=[1,2,3]"},
		{"object={{ vars.object }}.", `object={"k":"v"}.`},
		{"plain text", "plain text"},
		{"", ""},
		{"{{ vars.count }}{{ vars.count }}", "33"},
	}
	for _, tc := range cases {
		got := workflow.RenderText(tc.text, testResolve)
		text, ok := got.(string)
		if !ok {
			t.Errorf("RenderText(%q) = %#v, want a string", tc.text, got)
			continue
		}
		if text != tc.want {
			t.Errorf("RenderText(%q) = %q, want %q", tc.text, text, tc.want)
		}
	}
}

func TestRenderTextMixedUnresolvedPlaceholderStaysLiteral(t *testing.T) {
	text := "a {{ vars.missing }} b"
	if got := workflow.RenderText(text, testResolve); got != text {
		t.Errorf("RenderText(%q) = %#v, want the literal placeholder kept", text, got)
	}
}

func TestRenderTextUnclosedPlaceholder(t *testing.T) {
	text := "a {{ vars.count b"
	if got := workflow.RenderText(text, testResolve); got != text {
		t.Errorf("RenderText(%q) = %#v, want unchanged", text, got)
	}
}

func mustJSON(t *testing.T, source string) json.RawMessage {
	t.Helper()
	return json.RawMessage(source)
}

func TestEvaluateConditionComparisonOps(t *testing.T) {
	cases := []struct {
		name string
		ast  string
		want bool
	}{
		{"eq string match", `{"all":[{"path":"trigger.action","op":"eq","value":"opened"}]}`, true},
		{"eq string mismatch", `{"all":[{"path":"trigger.action","op":"eq","value":"closed"}]}`, false},
		{"eq numeric both numbers", `{"all":[{"path":"trigger.issue.number","op":"eq","value":42}]}`, true},
		{"eq numeric string coercion", `{"all":[{"path":"vars.number","op":"eq","value":42}]}`, true},
		{"eq numeric string vs number string", `{"all":[{"path":"vars.number","op":"eq","value":"42"}]}`, true},
		{"eq numeric mismatch", `{"all":[{"path":"trigger.issue.number","op":"eq","value":43}]}`, false},
		{"eq boolean", `{"all":[{"path":"vars.flag","op":"eq","value":true}]}`, true},
		{"eq boolean string form", `{"all":[{"path":"vars.flag","op":"eq","value":"true"}]}`, true},
		{"eq null", `{"all":[{"path":"trigger.issue.body","op":"eq","value":null}]}`, true},
		{"neq", `{"all":[{"path":"trigger.action","op":"neq","value":"closed"}]}`, true},
		{"neq equal", `{"all":[{"path":"trigger.action","op":"neq","value":"opened"}]}`, false},
		{"exists true", `{"all":[{"path":"trigger.issue.number","op":"exists"}]}`, true},
		{"exists false", `{"all":[{"path":"trigger.missing","op":"exists"}]}`, false},
		{"contains substring", `{"all":[{"path":"trigger.issue.title","op":"contains","value":"bug"}]}`, true},
		{"contains substring miss", `{"all":[{"path":"trigger.issue.title","op":"contains","value":"crash"}]}`, false},
		{"contains array element", `{"all":[{"path":"trigger.issue.labels","op":"contains","value":"bug"}]}`, true},
		{"contains array element miss", `{"all":[{"path":"trigger.issue.labels","op":"contains","value":"ui2"}]}`, false},
		{"contains array numeric element", `{"all":[{"path":"vars.list","op":"contains","value":2}]}`, true},
		{"in hit", `{"all":[{"path":"trigger.action","op":"in","value":["opened","edited"]}]}`, true},
		{"in miss", `{"all":[{"path":"trigger.action","op":"in","value":["closed","edited"]}]}`, false},
		{"gt true", `{"all":[{"path":"trigger.issue.number","op":"gt","value":40}]}`, true},
		{"gt equal is not greater", `{"all":[{"path":"trigger.issue.number","op":"gt","value":42}]}`, false},
		{"gte equal", `{"all":[{"path":"trigger.issue.number","op":"gte","value":42}]}`, true},
		{"lt false", `{"all":[{"path":"trigger.issue.number","op":"lt","value":40}]}`, false},
		{"lte", `{"all":[{"path":"trigger.issue.number","op":"lte","value":42}]}`, true},
		{"matches hit", `{"all":[{"path":"trigger.issue.title","op":"matches","value":"^bug"}]}`, true},
		{"matches miss", `{"all":[{"path":"trigger.issue.title","op":"matches","value":"^crash"}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := workflow.EvaluateCondition(mustJSON(t, tc.ast), testResolve)
			if err != nil {
				t.Fatalf("EvaluateCondition() error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("EvaluateCondition() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateConditionAllAndAnyGroups(t *testing.T) {
	ast := `{"all":[
		{"path":"trigger.action","op":"eq","value":"opened"},
		{"path":"trigger.issue.labels","op":"contains","value":"bug"}
	]}`
	got, err := workflow.EvaluateCondition(mustJSON(t, ast), testResolve)
	if err != nil || !got {
		t.Fatalf("all group with both rules true = (%v, %v), want (true, nil)", got, err)
	}

	ast = `{"all":[
		{"path":"trigger.action","op":"eq","value":"opened"},
		{"path":"trigger.issue.labels","op":"contains","value":"ui2"}
	]}`
	got, err = workflow.EvaluateCondition(mustJSON(t, ast), testResolve)
	if err != nil || got {
		t.Fatalf("all group with one false rule = (%v, %v), want (false, nil)", got, err)
	}

	ast = `{"any":[
		{"path":"trigger.action","op":"eq","value":"closed"},
		{"path":"trigger.issue.labels","op":"contains","value":"bug"}
	]}`
	got, err = workflow.EvaluateCondition(mustJSON(t, ast), testResolve)
	if err != nil || !got {
		t.Fatalf("any group with one true rule = (%v, %v), want (true, nil)", got, err)
	}

	ast = `{"any":[
		{"path":"trigger.action","op":"eq","value":"closed"},
		{"path":"trigger.issue.labels","op":"contains","value":"ui2"}
	]}`
	got, err = workflow.EvaluateCondition(mustJSON(t, ast), testResolve)
	if err != nil || got {
		t.Fatalf("any group with all rules false = (%v, %v), want (false, nil)", got, err)
	}
}

func TestEvaluateConditionErrors(t *testing.T) {
	cases := []struct {
		name string
		ast  string
	}{
		{"invalid json", `{"all":[`},
		{"empty object", `{}`},
		{"empty all", `{"all":[]}`},
		{"empty any", `{"any":[]}`},
		{"both groups", `{"all":[{"path":"trigger.action","op":"exists"}],"any":[{"path":"trigger.action","op":"exists"}]}`},
		{"unknown op", `{"all":[{"path":"trigger.action","op":"regex","value":"x"}]}`},
		{"missing path", `{"all":[{"op":"exists"}]}`},
		{"in with non-array value", `{"all":[{"path":"trigger.action","op":"in","value":"opened"}]}`},
		{"gt with non-number path", `{"all":[{"path":"trigger.issue.title","op":"gt","value":1}]}`},
		{"gt with non-number value", `{"all":[{"path":"trigger.issue.number","op":"gt","value":"abc"}]}`},
		{"matches with non-string value", `{"all":[{"path":"trigger.action","op":"matches","value":1}]}`},
		{"matches with invalid pattern", `{"all":[{"path":"trigger.action","op":"matches","value":"[["}]}`},
		{"matches with overlong pattern", `{"all":[{"path":"trigger.action","op":"matches","value":"a257chars"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast := tc.ast
			if strings.Contains(ast, "a257chars") {
				ast = strings.Replace(ast, "a257chars", strings.Repeat("a", 257), 1)
			}
			if _, err := workflow.EvaluateCondition(mustJSON(t, ast), testResolve); err == nil {
				t.Fatalf("EvaluateCondition(%s) expected an error", tc.name)
			}
		})
	}
}

func TestEvaluateConditionMatchesPatternLengthBoundary(t *testing.T) {
	pattern256 := strings.Repeat("a", 256)
	ast := `{"all":[{"path":"trigger.action","op":"matches","value":"` + pattern256 + `"}]}`
	if _, err := workflow.EvaluateCondition(mustJSON(t, ast), testResolve); err != nil {
		t.Fatalf("256-char pattern should be accepted, got error: %v", err)
	}
}
