package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TemplateNodeInput is one node of a template under validation.
type TemplateNodeInput struct {
	ID      string
	TypeKey string
	Config  json.RawMessage
}

// TemplateEdgeInput is one edge of a template under validation.
type TemplateEdgeInput struct {
	ID           string
	SourceNodeID string
	SourcePort   string
	TargetNodeID string
	TargetPort   string
}

// ValidationError identifies the template element a violation belongs to.
// Graph-level violations (trigger/end counts, cycles) carry empty NodeID and
// EdgeID.
type ValidationError struct {
	NodeID  string
	EdgeID  string
	Message string
}

func (e ValidationError) Error() string {
	switch {
	case e.NodeID != "" && e.EdgeID != "":
		return fmt.Sprintf("%s (node %s, edge %s)", e.Message, e.NodeID, e.EdgeID)
	case e.NodeID != "":
		return fmt.Sprintf("%s (node %s)", e.Message, e.NodeID)
	case e.EdgeID != "":
		return fmt.Sprintf("%s (edge %s)", e.Message, e.EdgeID)
	default:
		return e.Message
	}
}

// ValidateTemplateGraph checks a template against the registry rules of plan
// §5.1 and returns every violation found. The result is empty for a valid
// graph.
func ValidateTemplateGraph(r *Registry, nodes []TemplateNodeInput, edges []TemplateEdgeInput) []ValidationError {
	var errs []ValidationError

	byID := make(map[string]TemplateNodeInput, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}

	triggerID := ""
	triggerCount, endCount := 0, 0
	endIDs := make([]string, 0, 1)
	for _, n := range nodes {
		if strings.HasPrefix(n.TypeKey, "trigger.") {
			triggerCount++
			if triggerCount == 1 {
				triggerID = n.ID
			}
		}
		if n.TypeKey == "flow.end" {
			endCount++
			endIDs = append(endIDs, n.ID)
		}
	}
	if triggerCount == 0 {
		errs = append(errs, ValidationError{Message: "missing trigger node"})
	}
	if triggerCount > 1 {
		errs = append(errs, ValidationError{Message: "multiple trigger nodes"})
	}
	if endCount == 0 {
		errs = append(errs, ValidationError{Message: "missing flow.end node"})
	}
	if endCount > 1 {
		errs = append(errs, ValidationError{Message: "multiple flow.end nodes"})
	}

	for _, n := range nodes {
		def, ok := r.Get(n.TypeKey)
		if !ok {
			errs = append(errs, ValidationError{NodeID: n.ID, Message: fmt.Sprintf("unknown type_key: %s", n.TypeKey)})
			continue
		}
		if def.Validate == nil {
			continue
		}
		if err := def.Validate(n.Config); err != nil {
			errs = append(errs, ValidationError{NodeID: n.ID, Message: fmt.Sprintf("invalid config: %v", err)})
		}
	}

	seenEdgeTargets := make(map[string]bool, len(edges))
	usedSourcePorts := make(map[string]bool, len(edges))
	for _, e := range edges {
		source, sourceOK := byID[e.SourceNodeID]
		target, targetOK := byID[e.TargetNodeID]
		if !sourceOK || !targetOK {
			errs = append(errs, ValidationError{EdgeID: e.ID, Message: "edge references unknown node"})
			continue
		}
		if sourcePorts, err := r.OutputPorts(source.TypeKey, source.Config); err == nil {
			if !portMatches(sourcePorts, e.SourcePort) {
				errs = append(errs, ValidationError{EdgeID: e.ID, Message: fmt.Sprintf("invalid source port: %s", e.SourcePort)})
			}
		}
		if targetDef, ok := r.Get(target.TypeKey); ok {
			if !portMatches(targetDef.InputPorts, e.TargetPort) {
				errs = append(errs, ValidationError{EdgeID: e.ID, Message: fmt.Sprintf("invalid target port: %s", e.TargetPort)})
			}
		}
		targetKey := e.SourceNodeID + "|" + e.SourcePort + "|" + e.TargetNodeID
		if seenEdgeTargets[targetKey] {
			errs = append(errs, ValidationError{EdgeID: e.ID, Message: "duplicate edge"})
		}
		seenEdgeTargets[targetKey] = true
		portKey := e.SourceNodeID + "|" + e.SourcePort
		if usedSourcePorts[portKey] {
			errs = append(errs, ValidationError{EdgeID: e.ID, Message: "multiple edges from one source port"})
		}
		usedSourcePorts[portKey] = true
	}

	if graphHasCycle(nodes, edges) {
		errs = append(errs, ValidationError{Message: "cycle detected"})
	}

	if triggerCount == 1 {
		fromTrigger := reachableNodes([]string{triggerID}, edges, true)
		toEnd := reachableNodes(endIDs, edges, false)
		for _, n := range nodes {
			if n.ID != triggerID && !fromTrigger[n.ID] {
				errs = append(errs, ValidationError{NodeID: n.ID, Message: "node not reachable from trigger"})
				continue
			}
			if endCount == 1 && n.TypeKey != "flow.end" && !toEnd[n.ID] {
				errs = append(errs, ValidationError{NodeID: n.ID, Message: "node cannot reach flow.end"})
			}
		}
	}

	for _, n := range nodes {
		if !strings.HasPrefix(n.TypeKey, "trigger.") && !hasEdgeTouching(edges, n.ID, true) {
			errs = append(errs, ValidationError{NodeID: n.ID, Message: "missing input edge"})
		}
		if n.TypeKey != "flow.end" && !hasEdgeTouching(edges, n.ID, false) {
			errs = append(errs, ValidationError{NodeID: n.ID, Message: "missing output edge"})
		}
	}

	return errs
}

func portMatches(ports []PortDefinition, key string) bool {
	for _, port := range ports {
		if port.Key == key {
			return true
		}
	}
	return false
}

// graphHasCycle reports whether the directed graph contains a cycle, using a
// three-color depth-first search (white/gray/black).
func graphHasCycle(nodes []TemplateNodeInput, edges []TemplateEdgeInput) bool {
	adjacency := make(map[string][]string, len(nodes))
	for _, e := range edges {
		adjacency[e.SourceNodeID] = append(adjacency[e.SourceNodeID], e.TargetNodeID)
	}
	const (
		white = iota
		gray
		black
	)
	color := make(map[string]int, len(nodes))
	var visit func(id string) bool
	visit = func(id string) bool {
		switch color[id] {
		case gray:
			return true
		case black:
			return false
		}
		color[id] = gray
		for _, next := range adjacency[id] {
			if visit(next) {
				return true
			}
		}
		color[id] = black
		return false
	}
	for _, n := range nodes {
		if visit(n.ID) {
			return true
		}
	}
	return false
}

// reachableNodes returns the set of node ids reachable from roots, following
// edges forward when forward is true and backward otherwise.
func reachableNodes(roots []string, edges []TemplateEdgeInput, forward bool) map[string]bool {
	adjacency := make(map[string][]string, len(edges))
	for _, e := range edges {
		from, to := e.SourceNodeID, e.TargetNodeID
		if !forward {
			from, to = to, from
		}
		adjacency[from] = append(adjacency[from], to)
	}
	seen := make(map[string]bool, len(roots))
	stack := append([]string(nil), roots...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, adjacency[id]...)
	}
	return seen
}

// hasEdgeTouching reports whether any edge enters (incoming=true) or leaves
// (incoming=false) the node.
func hasEdgeTouching(edges []TemplateEdgeInput, nodeID string, incoming bool) bool {
	for _, e := range edges {
		if incoming && e.TargetNodeID == nodeID {
			return true
		}
		if !incoming && e.SourceNodeID == nodeID {
			return true
		}
	}
	return false
}
