package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// varNamePattern restricts data.set variable names to identifier shape.
var varNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// githubEvents maps each supported trigger/wait event to its allowed GitHub
// action names (plan §3.3). "push" carries no action filter.
var githubEvents = map[string][]string{
	"issues":              {"opened", "reopened", "edited", "labeled", "unlabeled", "assigned", "closed"},
	"issue_comment":       {"created"},
	"pull_request":        {"opened", "reopened", "synchronize", "closed"},
	"pull_request_review": {"submitted"},
	"workflow_run":        {"completed"},
	"push":                nil,
}

// decodeConfig parses a node config into an object. Missing or null configs
// behave like an empty object; any non-object JSON is an error.
func decodeConfig(config json.RawMessage) (map[string]any, error) {
	if len(config) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(config, &m); err != nil {
		return nil, fmt.Errorf("config must be a JSON object: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// trimNonEmpty reports the trimmed string; ok is false for blank values.
func trimNonEmpty(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	return trimmed, trimmed != ""
}

// cfgString returns the trimmed string at key; ok is false when the key is
// absent, null or blank.
func cfgString(m map[string]any, key string) (string, bool) {
	s, ok := m[key].(string)
	if !ok {
		return "", false
	}
	return trimNonEmpty(s)
}

// cfgNumber returns the number at key; ok is false unless it is a number > 0.
func cfgNumber(m map[string]any, key string) (float64, bool) {
	f, ok := m[key].(float64)
	if !ok || f <= 0 {
		return 0, false
	}
	return f, true
}

// cfgObject returns the object at key, which must be present.
func cfgObject(m map[string]any, key string) (map[string]any, error) {
	obj, ok := m[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s is required and must be an object", key)
	}
	return obj, nil
}

// cfgStringArray returns the string array at key. present is false when the
// key is absent or null; a present value that is not a string array is an
// error.
func cfgStringArray(m map[string]any, key string) ([]string, bool, error) {
	raw, present := m[key]
	if !present || raw == nil {
		return nil, false, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, true, fmt.Errorf("%s must be an array of strings", key)
	}
	out := make([]string, 0, len(arr))
	for _, el := range arr {
		s, ok := el.(string)
		if !ok {
			return nil, true, fmt.Errorf("%s must contain only strings", key)
		}
		out = append(out, s)
	}
	return out, true, nil
}

func hasDuplicates(items []string) bool {
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item] {
			return true
		}
		seen[item] = true
	}
	return false
}

// validateAnyObject accepts any JSON object config; it only enforces the
// object shape.
func validateAnyObject(config json.RawMessage) error {
	_, err := decodeConfig(config)
	return err
}

func validateTriggerSchedule(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	if v, ok := cfgNumber(m, "interval_minutes"); !ok || v <= 0 {
		return fmt.Errorf("interval_minutes must be a number > 0")
	}
	// The fired run must land in a concrete project (plan §12.3 runs are
	// project-scoped); the config carries it explicitly.
	projectID, ok := cfgString(m, "project_id")
	if !ok {
		return fmt.Errorf("project_id is required")
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return fmt.Errorf("project_id must be a UUID")
	}
	return nil
}

func validateTriggerGitHub(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	if _, ok := cfgString(m, "integration_id"); !ok {
		return fmt.Errorf("integration_id is required")
	}
	event, ok := cfgString(m, "event")
	if !ok {
		return fmt.Errorf("event is required")
	}
	allowed, known := githubEvents[event]
	if !known {
		return fmt.Errorf("unsupported event: %s", event)
	}
	actions, present, err := cfgStringArray(m, "actions")
	if err != nil {
		return err
	}
	if !present || len(actions) == 0 {
		if event != "push" {
			return fmt.Errorf("actions is required for event %s", event)
		}
		return nil
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowedSet[a] = true
	}
	for _, action := range actions {
		if !allowedSet[action] {
			return fmt.Errorf("action %q is not allowed for event %s", action, event)
		}
	}
	return nil
}

func validateAgentTask(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	if _, ok := cfgString(m, "instruction"); !ok {
		return fmt.Errorf("instruction is required")
	}
	assignee, err := cfgObject(m, "assignee")
	if err != nil {
		return err
	}
	mode, ok := cfgString(assignee, "mode")
	if !ok || (mode != "any_agent" && mode != "specific_agent") {
		return fmt.Errorf("assignee.mode must be any_agent or specific_agent")
	}
	if mode == "specific_agent" {
		if _, ok := cfgString(assignee, "agent_id"); !ok {
			return fmt.Errorf("assignee.agent_id is required for specific_agent")
		}
	}
	if _, ok := cfgNumber(m, "timeout_minutes"); !ok {
		return fmt.Errorf("timeout_minutes must be a number > 0")
	}
	retry, err := cfgObject(m, "retry")
	if err != nil {
		return err
	}
	maxAttempts, ok := cfgNumber(retry, "max_attempts")
	if !ok || maxAttempts < 1 {
		return fmt.Errorf("retry.max_attempts must be a number >= 1")
	}
	if raw, present := retry["backoff_seconds"]; present && raw != nil {
		backoff, ok := raw.(float64)
		if !ok || backoff < 0 {
			return fmt.Errorf("retry.backoff_seconds must be a number >= 0")
		}
	}
	if raw, present := m["input_map"]; present && raw != nil {
		inputMap, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("input_map must be an object with string values")
		}
		for key, value := range inputMap {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("input_map.%s must be a string", key)
			}
		}
	}
	for _, key := range []string{"readonly_dirs", "full_control_dirs"} {
		if _, _, err := cfgStringArray(m, key); err != nil {
			return err
		}
	}
	return nil
}

func validateActionGitHub(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	operation, ok := cfgString(m, "operation")
	if !ok {
		return fmt.Errorf("operation is required")
	}
	if integrationID, ok := cfgString(m, "integration_id"); ok {
		if _, err := uuid.Parse(integrationID); err != nil {
			return fmt.Errorf("integration_id must be a valid uuid")
		}
	}
	switch operation {
	case "add_comment", "add_labels", "remove_labels", "assign_issue", "unassign_issue", "close_issue", "reopen_issue":
		if _, ok := cfgString(m, "issue"); !ok {
			return fmt.Errorf("issue is required for operation %s", operation)
		}
	case "create_pull_request":
		for _, key := range []string{"title", "head", "base"} {
			if _, ok := cfgString(m, key); !ok {
				return fmt.Errorf("%s is required for create_pull_request", key)
			}
		}
	case "request_reviewers":
		if _, ok := cfgString(m, "pr"); !ok {
			return fmt.Errorf("pr is required for request_reviewers")
		}
		reviewers, present, err := cfgStringArray(m, "reviewers")
		if err != nil {
			return err
		}
		if !present || len(reviewers) == 0 {
			return fmt.Errorf("reviewers is required for request_reviewers")
		}
	default:
		return fmt.Errorf("unsupported operation: %s", operation)
	}
	return nil
}

func validateActionHTTP(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	method, ok := cfgString(m, "method")
	if !ok {
		return fmt.Errorf("method is required")
	}
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return fmt.Errorf("method must be one of GET/POST/PUT/PATCH/DELETE")
	}
	rawURL, ok := cfgString(m, "url")
	if !ok {
		return fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("url is not parseable: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	return nil
}

func validateActionCommand(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	command, err := cfgObject(m, "command")
	if err != nil {
		return err
	}
	if _, ok := cfgString(command, "program"); !ok {
		return fmt.Errorf("command.program is required")
	}
	if _, _, err := cfgStringArray(command, "args"); err != nil {
		return err
	}
	if workdir, ok := cfgString(command, "workdir"); ok {
		if err := validateRelativePath(workdir); err != nil {
			return fmt.Errorf("command.workdir: %w", err)
		}
	}
	return nil
}

// validateRelativePath accepts an empty or relative working directory:
// absolute POSIX paths, rooted/UNC Windows paths, drive letters and ".."
// traversal segments are rejected.
func validateRelativePath(path string) error {
	if path == "" {
		return nil
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return fmt.Errorf("must be a relative path")
	}
	if len(path) >= 2 && path[1] == ':' && isASCIILetter(path[0]) {
		return fmt.Errorf("must be a relative path")
	}
	for _, segment := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("must not contain ..")
		}
	}
	return nil
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func validateActionCreateTask(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	if _, ok := cfgString(m, "title"); !ok {
		return fmt.Errorf("title is required")
	}
	return nil
}

func validateWorkflowCall(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	templateID, ok := cfgString(m, "template_id")
	if !ok {
		return fmt.Errorf("template_id is required")
	}
	if _, err := uuid.Parse(templateID); err != nil {
		return fmt.Errorf("template_id must be a UUID: %w", err)
	}
	return nil
}

func validateDataSet(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	name, ok := cfgString(m, "name")
	if !ok {
		return fmt.Errorf("name is required")
	}
	if !varNamePattern.MatchString(name) {
		return fmt.Errorf("name must match ^[A-Za-z_][A-Za-z0-9_]*$")
	}
	if _, present := m["value"]; !present {
		return fmt.Errorf("value is required")
	}
	return nil
}

func validateDataMap(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	raw, present := m["mappings"]
	if !present || raw == nil {
		return fmt.Errorf("mappings is required")
	}
	arr, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("mappings must be an array")
	}
	if len(arr) == 0 {
		return fmt.Errorf("mappings must contain at least one entry")
	}
	for i, el := range arr {
		entry, ok := el.(map[string]any)
		if !ok {
			return fmt.Errorf("mappings[%d] must be an object", i)
		}
		target, ok := cfgString(entry, "target")
		if !ok {
			return fmt.Errorf("mappings[%d].target is required", i)
		}
		if !varNamePattern.MatchString(target) {
			return fmt.Errorf("mappings[%d].target must match ^[A-Za-z_][A-Za-z0-9_]*$", i)
		}
		if _, ok := entry["source"].(string); !ok {
			return fmt.Errorf("mappings[%d].source must be a string", i)
		}
	}
	return nil
}

func validateFlowCondition(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	condition, err := cfgObject(m, "condition")
	if err != nil {
		return err
	}
	return validateConditionGroup(condition)
}

// validateConditionGroup checks one condition AST ({all:[…]} / {any:[…]}): at
// least one rule overall and every rule structurally valid. Shared by
// flow.condition and each flow.switch routing rule.
func validateConditionGroup(condition map[string]any) error {
	allRules, err := conditionRules(condition, "all")
	if err != nil {
		return err
	}
	anyRules, err := conditionRules(condition, "any")
	if err != nil {
		return err
	}
	if len(allRules) == 0 && len(anyRules) == 0 {
		return fmt.Errorf("condition requires at least one rule in all or any")
	}
	if len(allRules) > 0 && len(anyRules) > 0 {
		return fmt.Errorf("condition all and any cannot be combined")
	}
	for _, group := range [][]map[string]any{allRules, anyRules} {
		for _, rule := range group {
			if err := validateConditionRule(rule); err != nil {
				return err
			}
		}
	}
	return nil
}

func conditionRules(condition map[string]any, key string) ([]map[string]any, error) {
	raw, present := condition[key]
	if !present || raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("condition.%s must be an array", key)
	}
	rules := make([]map[string]any, 0, len(arr))
	for i, el := range arr {
		rule, ok := el.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("condition.%s[%d] must be an object", key, i)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func validateConditionRule(rule map[string]any) error {
	if _, ok := cfgString(rule, "path"); !ok {
		return fmt.Errorf("condition rule path is required")
	}
	op, ok := cfgString(rule, "op")
	if !ok {
		return fmt.Errorf("condition rule op is required")
	}
	if !conditionOps[op] {
		return fmt.Errorf("unsupported condition op: %s", op)
	}
	if op == "matches" {
		pattern, ok := rule["value"].(string)
		if !ok {
			return fmt.Errorf("matches value must be a string")
		}
		if len(pattern) > maxPatternLength {
			return fmt.Errorf("matches pattern must be at most %d characters", maxPatternLength)
		}
	}
	return nil
}

func validateFlowSwitch(config json.RawMessage) error {
	rules, err := switchRulesFromConfig(config)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		return fmt.Errorf("switch requires at least one routing rule")
	}
	for _, rule := range rules {
		var condition map[string]any
		if err := json.Unmarshal(rule.Condition, &condition); err != nil {
			return fmt.Errorf("rules[%s].condition must be an object: %w", rule.Branch, err)
		}
		if err := validateConditionGroup(condition); err != nil {
			return fmt.Errorf("rules[%s]: %w", rule.Branch, err)
		}
	}
	return nil
}

func validateFlowParallel(config json.RawMessage) error {
	_, err := branchesFromConfig(config)
	return err
}

func validateFlowJoin(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	mode, ok := cfgString(m, "mode")
	if !ok || (mode != "all" && mode != "any") {
		return fmt.Errorf("mode must be all or any")
	}
	return nil
}

func validateFlowWait(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	mode, ok := cfgString(m, "mode")
	if !ok {
		return fmt.Errorf("mode is required")
	}
	switch mode {
	case "duration":
		if _, ok := cfgNumber(m, "minutes"); !ok {
			return fmt.Errorf("minutes must be a number > 0")
		}
	case "github_event":
		event, ok := cfgString(m, "event")
		if !ok {
			return fmt.Errorf("event is required")
		}
		if _, known := githubEvents[event]; !known {
			return fmt.Errorf("unsupported event: %s", event)
		}
	default:
		return fmt.Errorf("mode must be duration or github_event")
	}
	return nil
}

func validateHumanInput(config json.RawMessage) error {
	m, err := decodeConfig(config)
	if err != nil {
		return err
	}
	if _, ok := cfgString(m, "prompt"); !ok {
		return fmt.Errorf("prompt is required")
	}
	fields, present, err := cfgStringArray(m, "fields")
	if err != nil {
		return err
	}
	if present && hasDuplicates(fields) {
		return fmt.Errorf("fields must not contain duplicates")
	}
	return nil
}
