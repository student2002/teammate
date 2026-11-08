package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// maxPatternLength bounds regexp patterns in matches rules and validators.
const maxPatternLength = 256

// conditionOps is the closed set of condition operators (plan §4.3).
var conditionOps = map[string]bool{
	"eq":       true,
	"neq":      true,
	"exists":   true,
	"contains": true,
	"in":       true,
	"gt":       true,
	"gte":      true,
	"lt":       true,
	"lte":      true,
	"matches":  true,
}

// ResolvePath resolves a restricted dot path ("trigger.issue.labels[0]")
// against a decoded JSON value. Segments are object field names with optional
// "[n]" array index suffixes; anything else — empty segments, negative or
// non-numeric indexes, out-of-bounds access, missing keys, or traversal into
// scalars — reports not-found. No functions or dynamic expressions exist.
func ResolvePath(root any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	current := root
	for _, part := range strings.Split(path, ".") {
		name, indexes, ok := parsePathSegment(part)
		if !ok {
			return nil, false
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := object[name]
		if !ok {
			return nil, false
		}
		for _, index := range indexes {
			array, ok := value.([]any)
			if !ok || index >= len(array) {
				return nil, false
			}
			value = array[index]
		}
		current = value
	}
	return current, true
}

// parsePathSegment splits "name[0][2]" into the field name and its indexes.
func parsePathSegment(part string) (string, []int, bool) {
	name := part
	rest := ""
	if open := strings.Index(part, "["); open >= 0 {
		name = part[:open]
		rest = part[open:]
	}
	if name == "" {
		return "", nil, false
	}
	var indexes []int
	for rest != "" {
		if !strings.HasPrefix(rest, "[") {
			return "", nil, false
		}
		closing := strings.Index(rest, "]")
		if closing < 0 {
			return "", nil, false
		}
		index, err := strconv.Atoi(rest[1:closing])
		if err != nil || index < 0 {
			return "", nil, false
		}
		indexes = append(indexes, index)
		rest = rest[closing+1:]
	}
	return name, indexes, true
}

// RenderText expands "{{ path }}" placeholders in text. When the whole text is
// exactly one placeholder the resolved value is returned unchanged (JSON type
// preserved); when it does not resolve the original text is returned. When
// placeholders are mixed with literal characters the result is a Go string
// where resolved values are converted with valueToString and unresolved
// placeholders stay literal.
func RenderText(text string, resolve func(string) (any, bool)) any {
	var builder strings.Builder
	position := 0
	for {
		start := strings.Index(text[position:], "{{")
		if start < 0 {
			builder.WriteString(text[position:])
			break
		}
		start += position
		builder.WriteString(text[position:start])
		closeRel := strings.Index(text[start+2:], "}}")
		if closeRel < 0 {
			builder.WriteString(text[start:])
			break
		}
		end := start + 2 + closeRel // index of the opening brace of "}}"
		placeholderEnd := end + 2
		value, ok := resolve(strings.TrimSpace(text[start+2 : end]))
		if ok && start == 0 && placeholderEnd == len(text) {
			return value
		}
		if ok {
			builder.WriteString(valueToString(value))
		} else {
			builder.WriteString(text[start:placeholderEnd])
		}
		position = placeholderEnd
	}
	return builder.String()
}

// valueToString renders a decoded JSON value as text: strings verbatim,
// numbers and booleans in their literal form, everything else JSON-encoded.
func valueToString(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprintf("%v", value)
		}
		return string(encoded)
	}
}

// conditionRuleJSON is one rule of a condition AST.
type conditionRuleJSON struct {
	Path  string          `json:"path"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

// conditionASTJSON is the {"all": [...]} / {"any": [...]} condition shape.
type conditionASTJSON struct {
	All []conditionRuleJSON `json:"all"`
	Any []conditionRuleJSON `json:"any"`
}

// EvaluateCondition evaluates a condition AST against the resolver. An "all"
// group is true when every rule is true; an "any" group is true when at least
// one rule is true. Empty groups, both groups present, and malformed rules or
// values are errors.
func EvaluateCondition(ast json.RawMessage, resolve func(string) (any, bool)) (bool, error) {
	var parsed conditionASTJSON
	if err := json.Unmarshal(ast, &parsed); err != nil {
		return false, fmt.Errorf("evaluate condition: %w", err)
	}
	switch {
	case len(parsed.All) > 0 && len(parsed.Any) > 0:
		return false, fmt.Errorf("evaluate condition: all and any cannot be combined")
	case len(parsed.All) > 0:
		for _, rule := range parsed.All {
			matched, err := evaluateConditionRule(rule, resolve)
			if err != nil {
				return false, fmt.Errorf("evaluate condition: %w", err)
			}
			if !matched {
				return false, nil
			}
		}
		return true, nil
	case len(parsed.Any) > 0:
		for _, rule := range parsed.Any {
			matched, err := evaluateConditionRule(rule, resolve)
			if err != nil {
				return false, fmt.Errorf("evaluate condition: %w", err)
			}
			if matched {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("evaluate condition: all/any group is empty")
	}
}

func evaluateConditionRule(rule conditionRuleJSON, resolve func(string) (any, bool)) (bool, error) {
	if strings.TrimSpace(rule.Path) == "" {
		return false, fmt.Errorf("rule path is required")
	}
	left, leftOK := resolve(rule.Path)
	switch rule.Op {
	case "exists":
		return leftOK, nil
	case "eq", "neq":
		right, err := decodeRuleValue(rule.Value)
		if err != nil {
			return false, fmt.Errorf("op %s: %w", rule.Op, err)
		}
		equal, err := valuesEqual(left, right)
		if err != nil {
			return false, fmt.Errorf("op %s: %w", rule.Op, err)
		}
		if rule.Op == "eq" {
			return equal, nil
		}
		return !equal, nil
	case "contains":
		right, err := decodeRuleValue(rule.Value)
		if err != nil {
			return false, fmt.Errorf("op contains: %w", err)
		}
		return valuesContains(left, right)
	case "in":
		right, err := decodeRuleValue(rule.Value)
		if err != nil {
			return false, fmt.Errorf("op in: %w", err)
		}
		candidates, ok := right.([]any)
		if !ok {
			return false, fmt.Errorf("op in: value must be an array")
		}
		for _, candidate := range candidates {
			equal, err := valuesEqual(left, candidate)
			if err != nil {
				return false, fmt.Errorf("op in: %w", err)
			}
			if equal {
				return true, nil
			}
		}
		return false, nil
	case "gt", "gte", "lt", "lte":
		right, err := decodeRuleValue(rule.Value)
		if err != nil {
			return false, fmt.Errorf("op %s: %w", rule.Op, err)
		}
		leftNumber, leftIsNumber := asNumber(left)
		rightNumber, rightIsNumber := asNumber(right)
		if !leftIsNumber || !rightIsNumber {
			return false, fmt.Errorf("op %s: both sides must be numbers", rule.Op)
		}
		switch rule.Op {
		case "gt":
			return leftNumber > rightNumber, nil
		case "gte":
			return leftNumber >= rightNumber, nil
		case "lt":
			return leftNumber < rightNumber, nil
		default:
			return leftNumber <= rightNumber, nil
		}
	case "matches":
		pattern, err := ruleValueString(rule.Value)
		if err != nil {
			return false, fmt.Errorf("op matches: %w", err)
		}
		if len(pattern) > maxPatternLength {
			return false, fmt.Errorf("op matches: pattern must be at most %d characters", maxPatternLength)
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return false, fmt.Errorf("op matches: invalid pattern: %w", err)
		}
		return compiled.MatchString(valueToString(left)), nil
	default:
		return false, fmt.Errorf("unsupported op: %s", rule.Op)
	}
}

// decodeRuleValue decodes the rule value payload; an absent value behaves
// like JSON null.
func decodeRuleValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("value is not valid JSON: %w", err)
	}
	return value, nil
}

func ruleValueString(raw json.RawMessage) (string, error) {
	value, err := decodeRuleValue(raw)
	if err != nil {
		return "", err
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("value must be a string")
	}
	return text, nil
}

// asNumber interprets a value as a number: JSON numbers directly, strings via
// ParseFloat. Everything else is not a number.
func asNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number, err == nil
	default:
		return 0, false
	}
}

// valuesEqual compares two values: numerically when both sides interpret as
// numbers, otherwise by their string form.
func valuesEqual(left, right any) (bool, error) {
	leftNumber, leftIsNumber := asNumber(left)
	rightNumber, rightIsNumber := asNumber(right)
	if leftIsNumber && rightIsNumber {
		return leftNumber == rightNumber, nil
	}
	return valueToString(left) == valueToString(right), nil
}

// valuesContains implements the contains op: element match (eq semantics) on
// arrays, substring match on the string form of everything else.
func valuesContains(left, right any) (bool, error) {
	if array, ok := left.([]any); ok {
		for _, element := range array {
			equal, err := valuesEqual(element, right)
			if err != nil {
				return false, err
			}
			if equal {
				return true, nil
			}
		}
		return false, nil
	}
	return strings.Contains(valueToString(left), valueToString(right)), nil
}
