// sync_exec.go executes the synchronous server nodes of the V2 engine
// (plan §9.2): data.set/data.map write flow variables, flow.condition and
// flow.switch evaluate against the run context and emit one branch,
// flow.parallel/flow.join complete without computation. The caller supplies
// the context access functions, so this file stays free of storage concerns.
package workflow

import (
	"encoding/json"
	"fmt"
)

// SyncNodeTypes lists the node types the engine executes inline during an
// advance. flow.end is driven by its activated inbound ports instead and is
// deliberately absent.
var SyncNodeTypes = map[string]bool{
	"data.set":           true,
	"data.map":           true,
	TypeKeyFlowCondition: true,
	TypeKeyFlowSwitch:    true,
	TypeKeyFlowParallel:  true,
	"flow.join":          true,
}

// IsSyncNode reports whether the engine executes typeKey inline.
func IsSyncNode(typeKey string) bool { return SyncNodeTypes[typeKey] }

// ExecuteSyncNode runs one synchronous node against the run context.
// resolve reads a restricted dot path from the run context; applyVar writes a
// flow variable into context.vars.
//
// The returned output follows the node output JSON convention (plan §9.2):
// condition/switch return {"branch": <name>}, the other sync nodes return {}.
// The returned branch is the emitted branch name ("success" for data/join
// nodes, true/false for condition, the matched branch for switch and empty for
// parallel, whose ports derive from config.branches).
func ExecuteSyncNode(typeKey string, config json.RawMessage, resolve func(string) (any, bool), applyVar func(name string, value any)) (json.RawMessage, string, error) {
	m, err := decodeConfig(config)
	if err != nil {
		return nil, "", fmt.Errorf("execute %s: %w", typeKey, err)
	}

	switch typeKey {
	case "data.set":
		name, ok := cfgString(m, "name")
		if !ok {
			return nil, "", fmt.Errorf("execute data.set: name is required")
		}
		value := renderConfigValue(m["value"], resolve)
		applyVar(name, value)
		return json.RawMessage("{}"), "success", nil

	case "data.map":
		raw, present := m["mappings"]
		if !present || raw == nil {
			return nil, "", fmt.Errorf("execute data.map: mappings is required")
		}
		entries, ok := raw.([]any)
		if !ok {
			return nil, "", fmt.Errorf("execute data.map: mappings must be an array")
		}
		for i, el := range entries {
			entry, ok := el.(map[string]any)
			if !ok {
				return nil, "", fmt.Errorf("execute data.map: mappings[%d] must be an object", i)
			}
			target, ok := cfgString(entry, "target")
			if !ok {
				return nil, "", fmt.Errorf("execute data.map: mappings[%d].target is required", i)
			}
			value := renderConfigValue(entry["source"], resolve)
			applyVar(target, value)
		}
		return json.RawMessage("{}"), "success", nil

	case TypeKeyFlowCondition:
		raw, present := m["condition"]
		if !present || raw == nil {
			return nil, "", fmt.Errorf("execute flow.condition: condition is required")
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, "", fmt.Errorf("execute flow.condition: encode condition: %w", err)
		}
		matched, err := EvaluateCondition(encoded, resolve)
		if err != nil {
			return nil, "", fmt.Errorf("execute flow.condition: %w", err)
		}
		branch := "false"
		if matched {
			branch = "true"
		}
		return mustBranchOutput(branch), branch, nil

	case TypeKeyFlowSwitch:
		rules, err := switchRulesFromConfig(config)
		if err != nil {
			return nil, "", fmt.Errorf("execute flow.switch: %w", err)
		}
		for _, rule := range rules {
			matched, err := EvaluateCondition(rule.Condition, resolve)
			if err != nil {
				return nil, "", fmt.Errorf("execute flow.switch: rule %s: %w", rule.Branch, err)
			}
			if matched {
				return mustBranchOutput(rule.Branch), rule.Branch, nil
			}
		}
		return mustBranchOutput(defaultBranchPort), defaultBranchPort, nil

	case TypeKeyFlowParallel, "flow.join":
		return json.RawMessage("{}"), "success", nil

	default:
		return nil, "", fmt.Errorf("execute node: %s is not a synchronous node", typeKey)
	}
}

// renderConfigValue evaluates a config value against the context: strings go
// through text template rendering (pure placeholders keep their JSON type),
// every other JSON value passes through unchanged. A missing value yields nil.
func renderConfigValue(raw any, resolve func(string) (any, bool)) any {
	if raw == nil {
		return nil
	}
	if text, ok := raw.(string); ok {
		return RenderText(text, resolve)
	}
	return raw
}

func mustBranchOutput(branch string) json.RawMessage {
	encoded, err := json.Marshal(map[string]string{"branch": branch})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}
