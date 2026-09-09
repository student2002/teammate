// converter.go implements bidirectional conversion between sqlc-generated types (db package) and domain types (types package).
//
// This file is the "type isolation boundary" of the Store layer in the four-layer architecture:
//   - Store's public method signatures only expose types.* domain types
//   - Store internally continues to use sqlc-generated db.* types to interact with underlying queries
//   - This conversion layer converts between db.* ↔ types.* at the start and end of Store method bodies
//
// Field mapping rules (domain style, does not depend on database/sql):
//   - uuid.UUID → string（.String() / uuid.Parse）
//   - uuid.NullUUID → *string (nil means NULL)
//   - []uuid.UUID → []string
//   - sql.NullString → *string
//   - sql.NullTime → *time.Time
//   - sql.NullInt32 → *int32
//   - pqtype.NullRawMessage → json.RawMessage (nil means NULL)
//   - pqtype.Inet → string (raw CIDR/IP)
//   - Enum types (string aliases like TaskStatus) → string, zero-cost pass-through
//
// Error conventions:
//   - All toDomainXxx and FromDomainXxxParams return error (uuid.Parse may fail)
//   - Errors are wrapped with fmt.Errorf, never returned bare
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// ---------------------------------------------------------------------------
// Basic conversion helper functions
// ---------------------------------------------------------------------------

// uuidToString converts a uuid.UUID to a string.
func uuidToString(u uuid.UUID) string { return u.String() }

// stringToUUID converts a string to a uuid.UUID, returning an error on parse failure.
func stringToUUID(s string) (uuid.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse uuid %q: %w", s, err)
	}
	return u, nil
}

// nullUUIDToString converts a uuid.NullUUID to a *string; nil means NULL.
func nullUUIDToString(nu uuid.NullUUID) *string {
	if !nu.Valid {
		return nil
	}
	s := nu.UUID.String()
	return &s
}

// stringToNullUUID converts a *string to a uuid.NullUUID; nil means NULL.
// Returns a zero-value NullUUID (Valid=false) on parse failure.
func stringToNullUUID(s *string) uuid.NullUUID {
	if s == nil {
		return uuid.NullUUID{}
	}
	u, err := uuid.Parse(*s)
	if err != nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: u, Valid: true}
}

// nullUUIDToStringRequired is identical to nullUUIDToString; kept for semantic distinction.
func nullUUIDToStringRequired(nu uuid.NullUUID) (string, error) {
	if !nu.Valid {
		return "", fmt.Errorf("null uuid where required expected")
	}
	return nu.UUID.String(), nil
}

// uuidSliceToStringSlice converts a []uuid.UUID to a []string.
func uuidSliceToStringSlice(us []uuid.UUID) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, u.String())
	}
	return out
}

// stringSliceToUUIDSlice converts a []string to a []uuid.UUID, returning an error on parse failure.
func stringSliceToUUIDSlice(ss []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		u, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("parse uuid %q in slice: %w", s, err)
		}
		out = append(out, u)
	}
	return out, nil
}

// nullTimeToPtr converts a sql.NullTime to a *time.Time; nil means NULL.
func nullTimeToPtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	t := nt.Time
	return &t
}

// ptrToNullTime converts a *time.Time to a sql.NullTime; nil means NULL.
func ptrToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// nullStringToPtr converts a sql.NullString to a *string; nil means NULL.
func nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

// nullStringToValue converts a sql.NullString to a string; NULL degrades to an empty string.
// Used when the Type field in a domain struct is a string (not a *string).
func nullStringToValue(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}

// ptrToNullString converts a *string to a sql.NullString; nil means NULL.
func ptrToNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// nullInt32ToPtr converts a sql.NullInt32 to a *int32; nil means NULL.
func nullInt32ToPtr(ni sql.NullInt32) *int32 {
	if !ni.Valid {
		return nil
	}
	v := ni.Int32
	return &v
}

// ptrToNullInt32 converts a *int32 to a sql.NullInt32; nil means NULL.
func ptrToNullInt32(i *int32) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *i, Valid: true}
}

// nullRawToRaw converts a pqtype.NullRawMessage to a json.RawMessage; nil means NULL.
func nullRawToRaw(nrm pqtype.NullRawMessage) json.RawMessage {
	if !nrm.Valid {
		return nil
	}
	return nrm.RawMessage
}

// rawToNullRaw converts a json.RawMessage to a pqtype.NullRawMessage; nil or empty means NULL.
// Important: an empty json.RawMessage("") must return Valid:false, otherwise pgx will send the
// empty []byte as binary to the jsonb column, triggering a PG error "invalid input syntax for type json".
func rawToNullRaw(rm json.RawMessage) pqtype.NullRawMessage {
	if len(rm) == 0 {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: rm, Valid: true}
}

// inetToString converts a pqtype.Inet to a string (raw CIDR/IP).
func inetToString(inet pqtype.Inet) string {
	return inet.IPNet.String()
}

// stringToInet converts a string to a pqtype.Inet, returning a zero value on parse failure.
func stringToInet(s string) pqtype.Inet {
	if s == "" {
		return pqtype.Inet{}
	}
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		if ip := net.ParseIP(s); ip != nil {
			bitCount := len(ip) * 8
			mask := net.CIDRMask(bitCount, bitCount)
			return pqtype.Inet{IPNet: net.IPNet{IP: ip, Mask: mask}, Valid: true}
		}
		return pqtype.Inet{}
	}
	return pqtype.Inet{IPNet: *ipnet, Valid: true}
}

// ===========================================================================
// Task domain
// ===========================================================================

// ToDomainTask converts db.Task to types.Task.
func ToDomainTask(t db.Task) (types.Task, error) {
	return types.Task{
		ID:           t.ID,
		ProjectID:    t.ProjectID.String(),
		WorkflowName: t.WorkflowName,
		Title:        t.Title,
		Description:  t.Description.String,
		Constraints:  t.Constraints.String,
		Type:         string(t.Type),
		Priority:     string(t.Priority),
		Status:       string(t.Status),
		AuthorType:   t.AuthorType,
		AuthorID:     t.AuthorID.String(),
		DueDate:      nullTimeToPtr(t.DueDate),
		Labels:       t.Labels,
		Sequence:     int(t.Sequence),
		ParentTaskID: nullInt32ToPtr(t.ParentTaskID),
		GitBranch:    nullStringToPtr(t.GitBranch),
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}, nil
}

// toDomainTaskSlice converts []db.Task to []types.Task.
func ToDomainTaskSlice(ts []db.Task) ([]types.Task, error) {
	out := make([]types.Task, 0, len(ts))
	for _, t := range ts {
		d, err := ToDomainTask(t)
		if err != nil {
			return nil, fmt.Errorf("convert task: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateTaskParams converts types.CreateTaskParams to db.CreateTaskParams.
func FromDomainCreateTaskParams(p types.CreateTaskParams) (db.CreateTaskParams, error) {
	projectUUID, err := stringToUUID(p.ProjectID)
	if err != nil {
		return db.CreateTaskParams{}, fmt.Errorf("convert project id: %w", err)
	}
	authorUUID, err := stringToUUID(p.AuthorID)
	if err != nil {
		return db.CreateTaskParams{}, fmt.Errorf("convert author id: %w", err)
	}
	return db.CreateTaskParams{
		ProjectID:    projectUUID,
		Title:        p.Title,
		Description:  ptrToNullString(p.Description),
		Constraints:  ptrToNullString(p.Constraints),
		Type:         db.TaskType(p.Type),
		Priority:     db.TaskPriority(p.Priority),
		Status:       db.TaskStatus(p.Status),
		AuthorType:   p.AuthorType,
		AuthorID:     authorUUID,
		DueDate:      ptrToNullTime(p.DueDate),
		Labels:       p.Labels,
		Sequence:     p.Sequence,
		WorkflowName: p.WorkflowName,
	}, nil
}

// FromDomainUpdateTaskParams converts types.UpdateTaskParams to db.UpdateTaskParams.
func FromDomainUpdateTaskParams(p types.UpdateTaskParams) (db.UpdateTaskParams, error) {
	return db.UpdateTaskParams{
		ID:          p.ID,
		Title:       p.Title,
		Description: ptrToNullString(p.Description),
		Priority:    db.TaskPriority(p.Priority),
		Labels:      p.Labels,
		DueDate:     ptrToNullTime(p.DueDate),
		Constraints: ptrToNullString(p.Constraints),
		Status:      db.TaskStatus(p.Status),
	}, nil
}

// FromDomainUpdateTaskStatusParams converts types.UpdateTaskStatusParams to db.UpdateTaskStatusParams.
func FromDomainUpdateTaskStatusParams(p types.UpdateTaskStatusParams) (db.UpdateTaskStatusParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateTaskStatusParams{}, fmt.Errorf("convert task id: %w", err)
	}
	return db.UpdateTaskStatusParams{ID: int32(id.ID()), Status: db.TaskStatus(p.Status)}, nil
}

// FromDomainUpdateTaskGitBranchParams converts types.UpdateTaskGitBranchParams to db.UpdateTaskGitBranchParams.
func FromDomainUpdateTaskGitBranchParams(p types.UpdateTaskGitBranchParams) (db.UpdateTaskGitBranchParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateTaskGitBranchParams{}, fmt.Errorf("convert task id: %w", err)
	}
	return db.UpdateTaskGitBranchParams{ID: int32(id.ID()), GitBranch: ptrToNullString(p.GitBranch)}, nil
}

// ===========================================================================

// ===========================================================================
// TaskNode domain
// ===========================================================================

// ToDomainTaskNode converts db.TaskNode to types.TaskNode.
func ToDomainTaskNode(n db.TaskNode) (types.TaskNode, error) {
	return types.TaskNode{
		ID:                   n.ID.String(),
		TaskID:               n.TaskID,
		Name:                 n.Name,
		Description:          n.Description.String,
		SortOrder:            int(n.SortOrder),
		NodeType:             string(n.NodeType),
		Status:               string(n.Status),
		AssigneeType:         string(n.AssigneeType),
		AssigneeID:           nullUUIDToString(n.AssigneeID),
		ReservedForAgentID:   nullUUIDToString(n.ReservedForAgentID),
		RejectCount:          int(n.RejectCount),
		MaxRejectCycles:      int(n.MaxRejectCycles),
		TimeoutMinutes:       int(n.TimeoutMinutes),
		Version:              int(n.Version),
		CompletedAt:          nullTimeToPtr(n.CompletedAt),
		CompletedBy:          nullUUIDToString(n.CompletedBy),
		Summary:              n.Summary,
		PreviousSummary:      n.PreviousSummary,
		ReservationExpiresAt: nullTimeToPtr(n.ReservationExpiresAt),
		ReadonlyDirs:         nullRawToRaw(n.ReadonlyDirs),
		FullControlDirs:      nullRawToRaw(n.FullControlDirs),
		DependsOn:            uuidSliceToStringSlice(n.DependsOn),
		CreatedAt:            n.CreatedAt,
		UpdatedAt:            n.UpdatedAt,
	}, nil
}

// toDomainTaskNodeSlice converts []db.TaskNode to []types.TaskNode.
func ToDomainTaskNodeSlice(ns []db.TaskNode) ([]types.TaskNode, error) {
	out := make([]types.TaskNode, 0, len(ns))
	for _, n := range ns {
		d, err := ToDomainTaskNode(n)
		if err != nil {
			return nil, fmt.Errorf("convert task node: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateTaskNodeParams converts types.CreateTaskNodeParams to db.CreateTaskNodeParams.
func FromDomainCreateTaskNodeParams(p types.CreateTaskNodeParams) (db.CreateTaskNodeParams, error) {
	assigneeID := stringToNullUUID(p.AssigneeID)
	reserved := stringToNullUUID(p.ReservedForAgentID)
	deps, err := stringSliceToUUIDSlice(p.DependsOn)
	if err != nil {
		return db.CreateTaskNodeParams{}, fmt.Errorf("convert depends_on: %w", err)
	}
	return db.CreateTaskNodeParams{
		TaskID:             p.TaskID,
		Name:               p.Name,
		Description:        ptrToNullString(p.Description),
		SortOrder:          p.SortOrder,
		NodeType:           db.NodeType(p.NodeType),
		Status:             db.TaskNodeStatus(p.Status),
		AssigneeType:       db.AssigneeType(p.AssigneeType),
		AssigneeID:         assigneeID,
		ReservedForAgentID: reserved,
		MaxRejectCycles:    p.MaxRejectCycles,
		TimeoutMinutes:     p.TimeoutMinutes,
		ReadonlyDirs:       rawToNullRaw(p.ReadonlyDirs),
		FullControlDirs:    rawToNullRaw(p.FullControlDirs),
		DependsOn:          deps,
	}, nil
}

// FromDomainClaimTaskNodeParams converts types.ClaimTaskNodeParams to db.ClaimTaskNodeParams.
func FromDomainClaimTaskNodeParams(p types.ClaimTaskNodeParams) (db.ClaimTaskNodeParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.ClaimTaskNodeParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.ClaimTaskNodeParams{
		ID:         id,
		AssigneeID: stringToNullUUID(p.AssigneeID),
		Version:    p.Version,
	}, nil
}

// FromDomainClaimTaskNodeByHumanParams converts types.ClaimTaskNodeByHumanParams to db.ClaimTaskNodeByHumanParams.
func FromDomainClaimTaskNodeByHumanParams(p types.ClaimTaskNodeByHumanParams) (db.ClaimTaskNodeByHumanParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.ClaimTaskNodeByHumanParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.ClaimTaskNodeByHumanParams{
		ID:         id,
		AssigneeID: stringToNullUUID(p.AssigneeID),
		Version:    p.Version,
	}, nil
}

// FromDomainReclaimTaskNodeParams converts types.ReclaimTaskNodeParams to db.ReclaimTaskNodeParams.
func FromDomainReclaimTaskNodeParams(p types.ReclaimTaskNodeParams) (db.ReclaimTaskNodeParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.ReclaimTaskNodeParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.ReclaimTaskNodeParams{ID: id, Version: p.Version}, nil
}

// FromDomainResetRejectCountParams converts types.ResetRejectCountParams to db.ResetRejectCountParams.
func FromDomainResetRejectCountParams(p types.ResetRejectCountParams) (db.ResetRejectCountParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.ResetRejectCountParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.ResetRejectCountParams{ID: id}, nil
}

// FromDomainUpdateNodeSummaryParams converts types.UpdateNodeSummaryParams to db.UpdateNodeSummaryParams.
func FromDomainUpdateNodeSummaryParams(p types.UpdateNodeSummaryParams) (db.UpdateNodeSummaryParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateNodeSummaryParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.UpdateNodeSummaryParams{ID: id, Summary: p.Summary}, nil
}

// FromDomainUpdateTaskNodeStatusParams converts types.UpdateTaskNodeStatusParams to db.UpdateTaskNodeStatusParams.
func FromDomainUpdateTaskNodeStatusParams(p types.UpdateTaskNodeStatusParams) (db.UpdateTaskNodeStatusParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateTaskNodeStatusParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.UpdateTaskNodeStatusParams{
		ID:                   id,
		Status:               db.TaskNodeStatus(p.Status),
		AssigneeType:         db.AssigneeType(p.AssigneeType),
		AssigneeID:           stringToNullUUID(p.AssigneeID),
		ReservedForAgentID:   stringToNullUUID(p.ReservedForAgentID),
		RejectCount:          p.RejectCount,
		CompletedAt:          ptrToNullTime(p.CompletedAt),
		CompletedBy:          stringToNullUUID(p.CompletedBy),
		ReservationExpiresAt: ptrToNullTime(p.ReservationExpiresAt),
		Version:              p.Version,
		Status_2:             db.TaskNodeStatus(p.ExpectedCurrentStatus),
	}, nil
}

// FromDomainGetNextTaskNodeParams converts types.GetNextTaskNodeParams to db.GetNextTaskNodeParams.
func FromDomainGetNextTaskNodeParams(p types.GetNextTaskNodeParams) (db.GetNextTaskNodeParams, error) {
	id, err := stringToUUID(p.NodeID)
	if err != nil {
		return db.GetNextTaskNodeParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.GetNextTaskNodeParams{TaskID: p.TaskID, ID: id}, nil
}

// FromDomainGetPrevTaskNodeParams converts types.GetPrevTaskNodeParams to db.GetPrevTaskNodeParams.
func FromDomainGetPrevTaskNodeParams(p types.GetPrevTaskNodeParams) (db.GetPrevTaskNodeParams, error) {
	id, err := stringToUUID(p.NodeID)
	if err != nil {
		return db.GetPrevTaskNodeParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.GetPrevTaskNodeParams{TaskID: p.TaskID, ID: id}, nil
}

// FromDomainGetPrevStandardNodeAssigneeParams converts types.GetPrevStandardNodeAssigneeParams to db.GetPrevStandardNodeAssigneeParams.
func FromDomainGetPrevStandardNodeAssigneeParams(p types.GetPrevStandardNodeAssigneeParams) (db.GetPrevStandardNodeAssigneeParams, error) {
	id, err := stringToUUID(p.NodeID)
	if err != nil {
		return db.GetPrevStandardNodeAssigneeParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.GetPrevStandardNodeAssigneeParams{TaskID: p.TaskID, ID: id}, nil
}

// ===========================================================================
// Comment domain
// ===========================================================================

// ToDomainComment converts db.Comment to types.Comment.
func ToDomainComment(c db.Comment) (types.Comment, error) {
	return types.Comment{
		ID:           c.ID.String(),
		TaskID:       c.TaskID,
		NodeID:       nullUUIDToString(c.NodeID),
		SourceNodeID: nullUUIDToString(c.SourceNodeID),
		ParentID:     nullUUIDToString(c.ParentID),
		AuthorType:   c.AuthorType,
		AuthorID:     c.AuthorID.String(),
		Content:      c.Content,
		CommentType:  c.CommentType,
		Metadata:     nullRawToRaw(c.Metadata),
		Mentions:     uuidSliceToStringSlice(c.Mentions),
		EditedAt:     nullTimeToPtr(c.EditedAt),
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}, nil
}

// toDomainCommentSlice converts []db.Comment to []types.Comment.
func ToDomainCommentSlice(cs []db.Comment) ([]types.Comment, error) {
	out := make([]types.Comment, 0, len(cs))
	for _, c := range cs {
		d, err := ToDomainComment(c)
		if err != nil {
			return nil, fmt.Errorf("convert comment: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateCommentParams converts types.CreateCommentParams to db.CreateCommentParams.
func FromDomainCreateCommentParams(p types.CreateCommentParams) (db.CreateCommentParams, error) {
	nodeID := stringToNullUUID(p.NodeID)
	sourceNodeID := stringToNullUUID(p.SourceNodeID)
	parentID := stringToNullUUID(p.ParentID)
	authorID, err := stringToUUID(p.AuthorID)
	if err != nil {
		return db.CreateCommentParams{}, fmt.Errorf("convert author id: %w", err)
	}
	mentions, err := stringSliceToUUIDSlice(p.Mentions)
	if err != nil {
		return db.CreateCommentParams{}, fmt.Errorf("convert mentions: %w", err)
	}
	return db.CreateCommentParams{
		TaskID:       p.TaskID,
		NodeID:       nodeID,
		SourceNodeID: sourceNodeID,
		ParentID:     parentID,
		AuthorType:   p.AuthorType,
		AuthorID:     authorID,
		Content:      p.Content,
		CommentType:  p.CommentType,
		Metadata:     rawToNullRaw(p.Metadata),
		Mentions:     mentions,
	}, nil
}

// FromDomainUpdateCommentParams converts types.UpdateCommentParams to db.UpdateCommentParams.
func FromDomainUpdateCommentParams(p types.UpdateCommentParams) (db.UpdateCommentParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateCommentParams{}, fmt.Errorf("convert comment id: %w", err)
	}
	return db.UpdateCommentParams{ID: id, Content: p.Content}, nil
}

// ===========================================================================
// NodeTransition domain
// ===========================================================================

// ToDomainNodeTransition converts db.NodeTransition to types.NodeTransition.
func ToDomainNodeTransition(nt db.NodeTransition) (types.NodeTransition, error) {
	return types.NodeTransition{
		ID:           nt.ID.String(),
		TaskNodeID:   nt.TaskNodeID.String(),
		FromStatus:   string(nt.FromStatus),
		ToStatus:     string(nt.ToStatus),
		Action:       string(nt.Action),
		TargetNodeID: nullUUIDToString(nt.TargetNodeID),
		Comment:      nt.Comment.String,
		OperatorID:   nullUUIDToString(nt.OperatorID),
		OperatorType: nt.OperatorType,
		CreatedAt:    nt.CreatedAt,
	}, nil
}

// toDomainNodeTransitionSlice converts []db.NodeTransition to []types.NodeTransition.
func ToDomainNodeTransitionSlice(nts []db.NodeTransition) ([]types.NodeTransition, error) {
	out := make([]types.NodeTransition, 0, len(nts))
	for _, nt := range nts {
		d, err := ToDomainNodeTransition(nt)
		if err != nil {
			return nil, fmt.Errorf("convert node transition: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateNodeTransitionParams converts types.CreateNodeTransitionParams to db.CreateNodeTransitionParams.
func FromDomainCreateNodeTransitionParams(p types.CreateNodeTransitionParams) (db.CreateNodeTransitionParams, error) {
	taskNodeID, err := stringToUUID(p.TaskNodeID)
	if err != nil {
		return db.CreateNodeTransitionParams{}, fmt.Errorf("convert task node id: %w", err)
	}
	operatorID := stringToNullUUID(p.OperatorID)
	targetNodeID := stringToNullUUID(p.TargetNodeID)
	return db.CreateNodeTransitionParams{
		TaskNodeID:   taskNodeID,
		FromStatus:   db.TaskNodeStatus(p.FromStatus),
		ToStatus:     db.TaskNodeStatus(p.ToStatus),
		Action:       db.TransitionAction(p.Action),
		TargetNodeID: targetNodeID,
		Comment:      ptrToNullString(p.Comment),
		OperatorID:   operatorID,
		OperatorType: p.OperatorType,
	}, nil
}

// ===========================================================================
// TokenUsage domain
// ===========================================================================

// toDomainTokenUsage converts db.TokenUsage to types.TokenUsage.
func ToDomainTokenUsage(tu db.TokenUsage) (types.TokenUsage, error) {
	return types.TokenUsage{
		ID:           tu.ID,
		TaskNodeID:   tu.TaskNodeID.String(),
		AgentID:      tu.AgentID.String(),
		InputTokens:  tu.InputTokens,
		OutputTokens: tu.OutputTokens,
		TotalTokens:  tu.TotalTokens,
		CostEstimate: nullStringToPtr(tu.CostEstimate),
		CreatedAt:    tu.CreatedAt,
	}, nil
}

// FromDomainCreateTokenUsageParams converts types.CreateTokenUsageParams to db.CreateTokenUsageParams.
func FromDomainCreateTokenUsageParams(p types.CreateTokenUsageParams) (db.CreateTokenUsageParams, error) {
	taskNodeID, err := stringToUUID(p.TaskNodeID)
	if err != nil {
		return db.CreateTokenUsageParams{}, fmt.Errorf("convert task node id: %w", err)
	}
	agentID, err := stringToUUID(p.AgentID)
	if err != nil {
		return db.CreateTokenUsageParams{}, fmt.Errorf("convert agent id: %w", err)
	}
	return db.CreateTokenUsageParams{
		TaskNodeID:   taskNodeID,
		AgentID:      agentID,
		InputTokens:  p.InputTokens,
		OutputTokens: p.OutputTokens,
		TotalTokens:  p.TotalTokens,
		CostEstimate: ptrToNullString(p.CostEstimate),
	}, nil
}

// ===========================================================================
// Agent domain
// ===========================================================================

// ToDomainAgent converts db.Agent to types.Agent.
func ToDomainAgent(a db.Agent) (types.Agent, error) {
	return types.Agent{
		ID:            a.ID.String(),
		WorkspaceID:   a.WorkspaceID.String(),
		Name:          a.Name,
		Provider:      string(a.Provider),
		Instructions:  a.Instructions,
		Model:         a.Model.String,
		Status:        string(a.Status),
		CustomEnv:     nullRawToRaw(a.CustomEnv),
		ExtraArgs:     a.ExtraArgs,
		GitName:       a.GitName.String,
		GitEmail:      a.GitEmail.String,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}, nil
}

// toDomainAgentSlice converts []db.Agent to []types.Agent.
func ToDomainAgentSlice(as []db.Agent) ([]types.Agent, error) {
	out := make([]types.Agent, 0, len(as))
	for _, a := range as {
		d, err := ToDomainAgent(a)
		if err != nil {
			return nil, fmt.Errorf("convert agent: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateAgentParams converts types.CreateAgentParams to db.CreateAgentParams.
func FromDomainCreateAgentParams(p types.CreateAgentParams) (db.CreateAgentParams, error) {
	ws, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.CreateAgentParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	return db.CreateAgentParams{
		WorkspaceID:  ws,
		Name:         p.Name,
		Provider:     db.AgentProvider(p.Provider),
		Instructions: p.Instructions,
		Model:        ptrToNullString(p.Model),
		Status:       db.AgentStatus(p.Status),
		CustomEnv:    rawToNullRaw(p.CustomEnv),
		ExtraArgs:    p.ExtraArgs,
		GitName:      ptrToNullString(p.GitName),
		GitEmail:     ptrToNullString(p.GitEmail),
	}, nil
}

// FromDomainUpdateAgentParams converts types.UpdateAgentParams to db.UpdateAgentParams.
func FromDomainUpdateAgentParams(p types.UpdateAgentParams) (db.UpdateAgentParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateAgentParams{}, fmt.Errorf("convert agent id: %w", err)
	}
	return db.UpdateAgentParams{
		ID:           id,
		Instructions: sql.NullString{String: p.Instructions, Valid: p.Instructions != ""},
		Model:        ptrToNullString(p.Model),
		Status:       db.NullAgentStatus{AgentStatus: db.AgentStatus(p.Status), Valid: p.Status != ""},
		CustomEnv:    rawToNullRaw(p.CustomEnv),
		ExtraArgs:    p.ExtraArgs,
		GitName:      ptrToNullString(p.GitName),
		GitEmail:     ptrToNullString(p.GitEmail),
	}, nil
}

// FromDomainUpdateAgentStatusParams converts types.UpdateAgentStatusParams to db.UpdateAgentStatusParams.
func FromDomainUpdateAgentStatusParams(p types.UpdateAgentStatusParams) (db.UpdateAgentStatusParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateAgentStatusParams{}, fmt.Errorf("convert agent id: %w", err)
	}
	return db.UpdateAgentStatusParams{ID: id, Status: db.AgentStatus(p.Status)}, nil
}

// ===========================================================================
// Workspace domain
// ===========================================================================

// ToDomainWorkspace converts db.Workspace to types.Workspace.
func ToDomainWorkspace(w db.Workspace) (types.Workspace, error) {
	return types.Workspace{
		ID:          w.ID.String(),
		Name:        w.Name,
		Description: w.Description.String,
		IssuePrefix: w.IssuePrefix,
		IsDefault:   w.IsDefault,
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
	}, nil
}

// toDomainWorkspaceSlice converts []db.Workspace to []types.Workspace.
func ToDomainWorkspaceSlice(ws []db.Workspace) ([]types.Workspace, error) {
	out := make([]types.Workspace, 0, len(ws))
	for _, w := range ws {
		d, err := ToDomainWorkspace(w)
		if err != nil {
			return nil, fmt.Errorf("convert workspace: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateWorkspaceParams converts types.CreateWorkspaceParams to db.CreateWorkspaceParams.
func FromDomainCreateWorkspaceParams(p types.CreateWorkspaceParams) (db.CreateWorkspaceParams, error) {
	return db.CreateWorkspaceParams{
		Name:        p.Name,
		Description: ptrToNullString(p.Description),
		IssuePrefix: p.IssuePrefix,
		IsDefault:   p.IsDefault,
	}, nil
}

// FromDomainUpdateWorkspaceParams converts types.UpdateWorkspaceParams to db.UpdateWorkspaceParams.
func FromDomainUpdateWorkspaceParams(p types.UpdateWorkspaceParams) (db.UpdateWorkspaceParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateWorkspaceParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	return db.UpdateWorkspaceParams{
		ID:          id,
		Name:        p.Name,
		Description: ptrToNullString(p.Description),
	}, nil
}

// ===========================================================================
// Project domain
// ===========================================================================

// ToDomainProject converts db.Project to types.Project.
func ToDomainProject(p db.Project) (types.Project, error) {
	return types.Project{
		ID:                p.ID.String(),
		WorkspaceID:       p.WorkspaceID.String(),
		Name:              p.Name,
		Description:       p.Description.String,
		Icon:              p.Icon.String,
		Status:            string(p.Status),
		RepoURL:           p.RepoUrl.String,
		Context:           p.Context.String,
		DefaultWorkflowID: nullUUIDToString(p.DefaultWorkflowID),
		CreatedAt:         p.CreatedAt,
		UpdatedAt:         p.UpdatedAt,
	}, nil
}

// toDomainProjectSlice converts []db.Project to []types.Project.
func ToDomainProjectSlice(ps []db.Project) ([]types.Project, error) {
	out := make([]types.Project, 0, len(ps))
	for _, p := range ps {
		d, err := ToDomainProject(p)
		if err != nil {
			return nil, fmt.Errorf("convert project: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateProjectParams converts types.CreateProjectParams to db.CreateProjectParams.
func FromDomainCreateProjectParams(p types.CreateProjectParams) (db.CreateProjectParams, error) {
	ws, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.CreateProjectParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	return db.CreateProjectParams{
		WorkspaceID: ws,
		Name:        p.Name,
		Description: ptrToNullString(p.Description),
		Icon:        ptrToNullString(p.Icon),
		Status:      db.ProjectStatus(p.Status),
		RepoUrl:     ptrToNullString(p.RepoURL),
		Context:     ptrToNullString(p.Context),
	}, nil
}

// FromDomainUpdateProjectParams converts types.UpdateProjectParams to db.UpdateProjectParams.
func FromDomainUpdateProjectParams(p types.UpdateProjectParams) (db.UpdateProjectParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateProjectParams{}, fmt.Errorf("convert project id: %w", err)
	}
	wfID := stringToNullUUID(p.DefaultWorkflowID)
	return db.UpdateProjectParams{
		ID:                id,
		Name:              p.Name,
		Description:       ptrToNullString(p.Description),
		Status:            db.ProjectStatus(p.Status),
		RepoUrl:           ptrToNullString(p.RepoURL),
		Context:           ptrToNullString(p.Context),
		DefaultWorkflowID: wfID,
		MaxReviewCycles:   ptrToNullInt32(p.MaxReviewCycles),
	}, nil
}

// ===========================================================================
// ListTasks / Subtask / CountTasksByStatus param supplement (required for Task 3 pilot)
// ===========================================================================

// FromDomainListTasksParams converts types.ListTasksParams to db.ListTasksParams.
//
// Note: types.ListTasksParams currently only has a WorkspaceID field; the db version needs ProjectID and Status.
// The caller must fill in Status at the service layer; here it is passed through as a zero value.
func FromDomainListTasksParams(p types.ListTasksParams) (db.ListTasksParams, error) {
	pid, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.ListTasksParams{}, fmt.Errorf("convert project id: %w", err)
	}
	// types.ListTasksParams has no status field; pass NULL (the SQL @status IS NULL branch matches all statuses)
	return db.ListTasksParams{ProjectID: pid, Status: db.NullTaskStatus{}}, nil
}

// FromDomainListTasksPaginatedParams converts types.ListTasksPaginatedParams to db.ListTasksPaginatedParams.
func FromDomainListTasksPaginatedParams(p types.ListTasksPaginatedParams) (db.ListTasksPaginatedParams, error) {
	pid, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.ListTasksPaginatedParams{}, fmt.Errorf("convert project id: %w", err)
	}
	var statusVal db.NullTaskStatus
	if len(p.Statuses) > 0 {
		// Only takes the first status filter; multi-status filtering requires extending the param semantics in the types package
		statusVal = db.NullTaskStatus{TaskStatus: db.TaskStatus(p.Statuses[0]), Valid: true}
	}
	return db.ListTasksPaginatedParams{
		ProjectID:   pid,
		Status:      statusVal,
		SearchQuery: sql.NullString{},
		Offset:      p.Offset,
		Limit:       p.Limit,
	}, nil
}

// FromDomainCreateSubtaskParams converts types.CreateSubtaskParams to db.CreateSubtaskParams.
func FromDomainCreateSubtaskParams(p types.CreateSubtaskParams) (db.CreateSubtaskParams, error) {
	pid, err := stringToUUID(p.ProjectID)
	if err != nil {
		return db.CreateSubtaskParams{}, fmt.Errorf("convert project id: %w", err)
	}
	authorID, err := stringToUUID(p.AuthorID)
	if err != nil {
		return db.CreateSubtaskParams{}, fmt.Errorf("convert author id: %w", err)
	}
	return db.CreateSubtaskParams{
		ProjectID:    pid,
		Title:        p.Title,
		Description:  ptrToNullString(p.Description),
		Constraints:  ptrToNullString(p.Constraints),
		Type:         db.TaskType(p.Type),
		Priority:     db.TaskPriority(p.Priority),
		Status:       db.TaskStatus(p.Status),
		AuthorType:   p.AuthorType,
		AuthorID:     authorID,
		DueDate:      ptrToNullTime(p.DueDate),
		Labels:       p.Labels,
		Sequence:     p.Sequence,
		WorkflowName: p.WorkflowName,
		ParentTaskID: ptrToNullInt32(p.ParentTaskID),
	}, nil
}

// FromDomainCountTasksByStatusParams converts types.CountTasksByStatusParams to db.CountTasksByStatusParams.
func FromDomainCountTasksByStatusParams(p types.CountTasksByStatusParams) (db.CountTasksByStatusParams, error) {
	pid, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.CountTasksByStatusParams{}, fmt.Errorf("convert project id: %w", err)
	}
	var statusVal db.NullTaskStatus
	if len(p.Statuses) > 0 {
		statusVal = db.NullTaskStatus{TaskStatus: db.TaskStatus(p.Statuses[0]), Valid: true}
	}
	return db.CountTasksByStatusParams{
		ProjectID:   pid,
		Status:      statusVal,
		SearchQuery: sql.NullString{},
	}, nil
}

// ===========================================================================
// Invitation domain
// ===========================================================================

// ToDomainInvitation converts db.Invitation to types.Invitation.
func ToDomainInvitation(i db.Invitation) (types.Invitation, error) {
	return types.Invitation{
		ID:          i.ID.String(),
		WorkspaceID: i.WorkspaceID.String(),
		Email:       i.Email,
		Role:        i.Role,
		TokenHash:   i.TokenHash,
		InvitedBy:   nullUUIDToString(i.InvitedBy),
		ExpiresAt:   i.ExpiresAt,
		AcceptedAt:  nullTimeToPtr(i.AcceptedAt),
		CreatedAt:   i.CreatedAt,
	}, nil
}

// ToDomainInvitationSlice converts []db.Invitation to []types.Invitation.
func ToDomainInvitationSlice(is []db.Invitation) ([]types.Invitation, error) {
	out := make([]types.Invitation, 0, len(is))
	for _, i := range is {
		d, err := ToDomainInvitation(i)
		if err != nil {
			return nil, fmt.Errorf("convert invitation: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateInvitationParams assembles domain-style params into db.CreateInvitationParams.
//
// Note: types.CreateInvitationParams and db.CreateInvitationParams fields are one-to-one but of different types;
// this function accepts db-style input params like uuid.UUID/uuid.NullUUID, keeping the method signature consistent with store/invitation.go.
// In the future, if store/invitation.go uniformly changes to string, this function's input params can be changed accordingly to string.
func fromDomainCreateInvitationParams(workspaceID uuid.UUID, email, role, tokenHash string, invitedBy uuid.UUID, expiresAt time.Time) db.CreateInvitationParams {
	return db.CreateInvitationParams{
		WorkspaceID: workspaceID,
		Email:       email,
		Role:        role,
		TokenHash:   tokenHash,
		InvitedBy:   uuid.NullUUID{UUID: invitedBy, Valid: invitedBy != uuid.Nil},
		ExpiresAt:   expiresAt,
	}
}

// ===========================================================================
// AgentPermission domain
// ===========================================================================

// ToDomainAgentPermission converts db.AgentPermission to types.AgentPermission.
func ToDomainAgentPermission(p db.AgentPermission) (types.AgentPermission, error) {
	return types.AgentPermission{
		ID:           p.ID.String(),
		AgentID:      p.AgentID.String(),
		Permission:   p.Permission,
		ResourceType: p.ResourceType,
		ResourceID:   nullUUIDToString(p.ResourceID),
		GrantedBy:    nullUUIDToString(p.GrantedBy),
		CreatedAt:    p.CreatedAt,
	}, nil
}

// ToDomainAgentPermissionSlice converts []db.AgentPermission to []types.AgentPermission.
func ToDomainAgentPermissionSlice(ps []db.AgentPermission) ([]types.AgentPermission, error) {
	out := make([]types.AgentPermission, 0, len(ps))
	for _, p := range ps {
		d, err := ToDomainAgentPermission(p)
		if err != nil {
			return nil, fmt.Errorf("convert agent permission: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// ===========================================================================
// Memory domain
// ===========================================================================

// ToDomainMemory converts db.Memory to types.Memory.
//
// Note: db.Memory has an Embedding interface{} field; types.Memory intentionally does not contain Embedding
// (app code never reads embedding, only writes it). This function discards Embedding.
func ToDomainMemory(m db.Memory) (types.Memory, error) {
	return types.Memory{
		ID:           m.ID.String(),
		WorkspaceID:  m.WorkspaceID.String(),
		SourceTaskID: nullInt32ToPtr(m.SourceTaskID),
		Type:         string(m.Type),
		Title:        m.Title,
		Content:      m.Content,
		Tags:         m.Tags,
		Confidence:   float64(m.Confidence),
		Verified:     m.Verified,
		Stale:        m.Stale,
		Metadata:     nullRawToRaw(m.Metadata),
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}, nil
}

// ToDomainMemorySlice converts []db.Memory to []types.Memory.
func ToDomainMemorySlice(ms []db.Memory) ([]types.Memory, error) {
	out := make([]types.Memory, 0, len(ms))
	for _, m := range ms {
		d, err := ToDomainMemory(m)
		if err != nil {
			return nil, fmt.Errorf("convert memory: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateMemoryParams converts types.CreateMemoryParams to db.CreateMemoryParams.
func FromDomainCreateMemoryParams(p types.CreateMemoryParams) (db.CreateMemoryParams, error) {
	ws, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.CreateMemoryParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	return db.CreateMemoryParams{
		WorkspaceID:  ws,
		SourceTaskID: ptrToNullInt32(p.SourceTaskID),
		Type:         db.MemoryType(p.Type),
		Title:        p.Title,
		Content:      p.Content,
		Tags:         p.Tags,
		Confidence:   p.Confidence,
		Verified:     p.Verified,
		Metadata:     rawToNullRaw(p.Metadata),
	}, nil
}

// FromDomainListMemoriesByWorkspaceParams assembles domain-style params into db.ListMemoriesByWorkspaceParams.
//
// Note: the db version wraps nullable filter conditions with sql.NullBool/sql.NullFloat64/sql.NullInt32,
// the domain version uses *bool/*float32/*int32 pointers. This function does the pointer -> NullXX conversion.
func FromDomainListMemoriesByWorkspaceParams(workspaceID uuid.UUID, verified *bool, minConfidence *float32, limit *int32) db.ListMemoriesByWorkspaceParams {
	var verifiedVal sql.NullBool
	if verified != nil {
		verifiedVal = sql.NullBool{Bool: *verified, Valid: true}
	}
	var minConfVal sql.NullFloat64
	if minConfidence != nil {
		minConfVal = sql.NullFloat64{Float64: float64(*minConfidence), Valid: true}
	}
	var limitVal sql.NullInt32
	if limit != nil {
		limitVal = sql.NullInt32{Int32: *limit, Valid: true}
	}
	return db.ListMemoriesByWorkspaceParams{
		WorkspaceID:   workspaceID,
		Verified:      verifiedVal,
		MinConfidence: minConfVal,
		Limit:         limitVal,
	}
}

// ===========================================================================
// Search domain
// ===========================================================================

// FromDomainSearchTasksByWorkspaceParams assembles domain-style params into db.SearchTasksByWorkspaceParams.
func FromDomainSearchTasksByWorkspaceParams(workspaceID uuid.UUID, pattern string) db.SearchTasksByWorkspaceParams {
	return db.SearchTasksByWorkspaceParams{
		WorkspaceID: workspaceID,
		Title:       pattern,
	}
}

// FromDomainSearchTasksByWorkspaceAndProjectParams assembles domain-style params into db.SearchTasksByWorkspaceAndProjectParams.
func FromDomainSearchTasksByWorkspaceAndProjectParams(workspaceID, projectID uuid.UUID, pattern string) db.SearchTasksByWorkspaceAndProjectParams {
	return db.SearchTasksByWorkspaceAndProjectParams{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		Title:       pattern,
	}
}

// FromDomainSearchAgentsByWorkspaceParams assembles domain-style params into db.SearchAgentsByWorkspaceParams.
func FromDomainSearchAgentsByWorkspaceParams(workspaceID uuid.UUID, pattern string) db.SearchAgentsByWorkspaceParams {
	return db.SearchAgentsByWorkspaceParams{
		WorkspaceID: workspaceID,
		Name:        pattern,
	}
}

// ===========================================================================
// Community domain
// ===========================================================================

// ToDomainCommunityWorkflow converts db.CommunityWorkflow to types.CommunityWorkflow.
func ToDomainCommunityWorkflow(c db.CommunityWorkflow) (types.CommunityWorkflow, error) {
	return types.CommunityWorkflow{
		ID:                           c.ID.String(),
		Name:                         c.Name,
		Description:                  c.Description.String,
		Author:                       c.Author,
		Version:                      c.Version,
		WorkflowDefinition:           c.WorkflowDefinition,
		RequiredSkills:               nullRawToRaw(c.RequiredSkills),
		RequiredMcpServers:           nullRawToRaw(c.RequiredMcpServers),
		RecommendedAgentInstructions: nullRawToRaw(c.RecommendedAgentInstructions),
		Downloads:                    int(c.Downloads),
		IsOfficial:                   c.IsOfficial,
		CreatedAt:                    c.CreatedAt,
		UpdatedAt:                    c.UpdatedAt,
	}, nil
}

// ToDomainCommunityWorkflowSlice converts []db.CommunityWorkflow to []types.CommunityWorkflow.
func ToDomainCommunityWorkflowSlice(cs []db.CommunityWorkflow) ([]types.CommunityWorkflow, error) {
	out := make([]types.CommunityWorkflow, 0, len(cs))
	for _, c := range cs {
		d, err := ToDomainCommunityWorkflow(c)
		if err != nil {
			return nil, fmt.Errorf("convert community workflow: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateCommunityWorkflowParams converts types.CreateCommunityWorkflowParams to db.CreateCommunityWorkflowParams.
func FromDomainCreateCommunityWorkflowParams(p types.CreateCommunityWorkflowParams) (db.CreateCommunityWorkflowParams, error) {
	return db.CreateCommunityWorkflowParams{
		Name:                         p.Name,
		Description:                  ptrToNullString(p.Description),
		Author:                       p.Author,
		Version:                      p.Version,
		WorkflowDefinition:           p.WorkflowDefinition,
		RequiredSkills:               rawToNullRaw(p.RequiredSkills),
		RequiredMcpServers:           rawToNullRaw(p.RequiredMcpServers),
		RecommendedAgentInstructions: rawToNullRaw(p.RecommendedAgentInstructions),
	}, nil
}

// ===========================================================================
// Workflow domain
// ===========================================================================

// ToDomainWorkflowTemplate converts db.WorkflowTemplate to types.WorkflowTemplate.
func ToDomainWorkflowTemplate(t db.WorkflowTemplate) (types.WorkflowTemplate, error) {
	return types.WorkflowTemplate{
		ID:              t.ID.String(),
		WorkspaceID:     t.WorkspaceID.String(),
		Name:            t.Name,
		Description:     t.Description.String,
		IsBuiltin:       t.IsBuiltin,
		TriggerType:     string(t.TriggerType),
		TriggerConfig:   t.TriggerConfig,
		TriggerEnabled:  t.TriggerEnabled,
		NextRunAt:       nullTimeToPtr(t.NextRunAt),
		LastTriggeredAt: nullTimeToPtr(t.LastTriggeredAt),
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}, nil
}

// ToDomainWorkflowTemplateSlice converts []db.WorkflowTemplate to []types.WorkflowTemplate.
func ToDomainWorkflowTemplateSlice(ts []db.WorkflowTemplate) ([]types.WorkflowTemplate, error) {
	out := make([]types.WorkflowTemplate, 0, len(ts))
	for _, t := range ts {
		d, err := ToDomainWorkflowTemplate(t)
		if err != nil {
			return nil, fmt.Errorf("convert workflow template: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// ToDomainWorkflowTemplateNode converts db.WorkflowTemplateNode to types.WorkflowTemplateNode.
func ToDomainWorkflowTemplateNode(n db.WorkflowTemplateNode) (types.WorkflowTemplateNode, error) {
	return types.WorkflowTemplateNode{
		ID:              n.ID.String(),
		TemplateID:      n.TemplateID.String(),
		Name:            n.Name,
		Description:     n.Description.String,
		SortOrder:       int(n.SortOrder),
		NodeType:        string(n.NodeType),
		AssigneeType:    string(n.AssigneeType),
		AssigneeID:      nullUUIDToString(n.AssigneeID),
		TimeoutMinutes:  int(n.TimeoutMinutes),
		ReadonlyDirs:    nullRawToRaw(n.ReadonlyDirs),
		FullControlDirs: nullRawToRaw(n.FullControlDirs),
		Artifact:        nullRawToRaw(n.Artifact),
		DependsOn:       uuidSliceToStringSlice(n.DependsOn),
		MaxRejectCycles: int(n.MaxRejectCycles),
		CreatedAt:       n.CreatedAt,
	}, nil
}

// ToDomainWorkflowTemplateNodeSlice converts []db.WorkflowTemplateNode to []types.WorkflowTemplateNode.
func ToDomainWorkflowTemplateNodeSlice(ns []db.WorkflowTemplateNode) ([]types.WorkflowTemplateNode, error) {
	out := make([]types.WorkflowTemplateNode, 0, len(ns))
	for _, n := range ns {
		d, err := ToDomainWorkflowTemplateNode(n)
		if err != nil {
			return nil, fmt.Errorf("convert workflow template node: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateWorkflowTemplateParams converts types.CreateWorkflowTemplateParams to db.CreateWorkflowTemplateParams.
func FromDomainCreateWorkflowTemplateParams(p types.CreateWorkflowTemplateParams) (db.CreateWorkflowTemplateParams, error) {
	ws, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.CreateWorkflowTemplateParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	return db.CreateWorkflowTemplateParams{
		WorkspaceID:     ws,
		Name:            p.Name,
		Description:     ptrToNullString(p.Description),
		IsBuiltin:       p.IsBuiltin,
		TriggerType:     db.WorkflowTriggerType(p.TriggerType),
		TriggerConfig:   p.TriggerConfig,
		TriggerEnabled:  p.TriggerEnabled,
		NextRunAt:       ptrToNullTime(p.NextRunAt),
		LastTriggeredAt: ptrToNullTime(p.LastTriggeredAt),
	}, nil
}

// FromDomainUpdateWorkflowTemplateParams converts types.UpdateWorkflowTemplateParams to db.UpdateWorkflowTemplateParams.
//
// Note: the db version ID is uuid.UUID; the domain version ID is string.
func FromDomainUpdateWorkflowTemplateParams(p types.UpdateWorkflowTemplateParams) (db.UpdateWorkflowTemplateParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateWorkflowTemplateParams{}, fmt.Errorf("convert id: %w", err)
	}
	return db.UpdateWorkflowTemplateParams{
		ID:             id,
		Name:           p.Name,
		Description:    ptrToNullString(p.Description),
		TriggerType:    db.WorkflowTriggerType(p.TriggerType),
		TriggerConfig:  p.TriggerConfig,
		TriggerEnabled: p.TriggerEnabled,
		NextRunAt:      ptrToNullTime(p.NextRunAt),
	}, nil
}

// FromDomainCreateTemplateNodeParams converts types.CreateTemplateNodeParams to db.CreateTemplateNodeParams.
func FromDomainCreateTemplateNodeParams(p types.CreateTemplateNodeParams) (db.CreateTemplateNodeParams, error) {
	// TemplateID allows empty: CreateWorkflowTemplate first creates the template within the transaction, then backfills the nodes' TemplateID
	var tplID uuid.UUID
	if p.TemplateID != "" {
		var err error
		tplID, err = stringToUUID(p.TemplateID)
		if err != nil {
			return db.CreateTemplateNodeParams{}, fmt.Errorf("convert template id: %w", err)
		}
	}
	var assigneeID uuid.NullUUID
	if p.AssigneeID != nil {
		if u, err := uuid.Parse(*p.AssigneeID); err == nil {
			assigneeID = uuid.NullUUID{UUID: u, Valid: true}
		}
	}
	dependsOn, err := stringSliceToUUIDSlice(p.DependsOn)
	if err != nil {
		return db.CreateTemplateNodeParams{}, fmt.Errorf("convert depends on: %w", err)
	}
	return db.CreateTemplateNodeParams{
		TemplateID:      tplID,
		Name:            p.Name,
		Description:     ptrToNullString(p.Description),
		SortOrder:       int32(p.SortOrder),
		NodeType:        db.NodeType(p.NodeType),
		AssigneeType:    db.AssigneeType(p.AssigneeType),
		AssigneeID:      assigneeID,
		TimeoutMinutes:  int32(p.TimeoutMinutes),
		MaxRejectCycles: int32(p.MaxRejectCycles),
		ReadonlyDirs:    rawToNullRaw(p.ReadonlyDirs),
		FullControlDirs: rawToNullRaw(p.FullControlDirs),
		Artifact:        rawToNullRaw(p.Artifact),
		DependsOn:       dependsOn,
	}, nil
}

// FromDomainCreateTemplateNodeParamsSlice batch converts []types.CreateTemplateNodeParams to []db.CreateTemplateNodeParams.
func FromDomainCreateTemplateNodeParamsSlice(ps []types.CreateTemplateNodeParams) ([]db.CreateTemplateNodeParams, error) {
	out := make([]db.CreateTemplateNodeParams, 0, len(ps))
	for _, p := range ps {
		d, err := FromDomainCreateTemplateNodeParams(p)
		if err != nil {
			return nil, fmt.Errorf("convert create template node params: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// ===========================================================================
// Skill domain
// ===========================================================================

// ToDomainSkill converts db.Skill to types.Skill.
func ToDomainSkill(s db.Skill) (types.Skill, error) {
	return types.Skill{
		ID:             s.ID.String(),
		WorkspaceID:    s.WorkspaceID.String(),
		Name:           s.Name,
		Description:    s.Description.String,
		Category:       s.Category.String,
		PromptTemplate: s.PromptTemplate.String,
		CreatedAt:      s.CreatedAt,
	}, nil
}

// ToDomainSkillSlice converts []db.Skill to []types.Skill.
func ToDomainSkillSlice(ss []db.Skill) ([]types.Skill, error) {
	out := make([]types.Skill, 0, len(ss))
	for _, s := range ss {
		d, err := ToDomainSkill(s)
		if err != nil {
			return nil, fmt.Errorf("convert skill: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateSkillParams converts types.CreateSkillParams to db.CreateSkillParams.
func FromDomainCreateSkillParams(p types.CreateSkillParams) (db.CreateSkillParams, error) {
	ws, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.CreateSkillParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	return db.CreateSkillParams{
		WorkspaceID:    ws,
		Name:           p.Name,
		Description:    ptrToNullString(p.Description),
		Category:       ptrToNullString(p.Category),
		PromptTemplate: ptrToNullString(p.PromptTemplate),
	}, nil
}

// FromDomainUpdateSkillParams converts types.UpdateSkillParams to db.UpdateSkillParams.
func FromDomainUpdateSkillParams(p types.UpdateSkillParams) (db.UpdateSkillParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateSkillParams{}, fmt.Errorf("convert id: %w", err)
	}
	return db.UpdateSkillParams{
		ID:             id,
		Name:           p.Name,
		Description:    ptrToNullString(p.Description),
		Category:       ptrToNullString(p.Category),
		PromptTemplate: ptrToNullString(p.PromptTemplate),
	}, nil
}

// ===========================================================================
// Runtime domain
// ===========================================================================

// ToDomainRuntime converts db.Runtime to types.Runtime.
func ToDomainRuntime(r db.Runtime) (types.Runtime, error) {
	return types.Runtime{
		ID:               r.ID.String(),
		AgentID:          r.AgentID.String(),
		DaemonID:         r.DaemonID,
		Provider:         string(r.Provider),
		Version:          r.Version.String,
		Status:           string(r.Status),
		SessionTokenHash: r.SessionTokenHash.String,
		SessionExpiresAt: nullTimeToPtr(r.SessionExpiresAt),
		PublicKey:        r.PublicKey.String,
		LastHeartbeat:    nullTimeToPtr(r.LastHeartbeat),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}, nil
}

// ToDomainRuntimeSlice converts []db.Runtime to []types.Runtime.
func ToDomainRuntimeSlice(rs []db.Runtime) ([]types.Runtime, error) {
	out := make([]types.Runtime, 0, len(rs))
	for _, r := range rs {
		d, err := ToDomainRuntime(r)
		if err != nil {
			return nil, fmt.Errorf("convert runtime: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateRuntimeParams converts types.CreateRuntimeParams to db.CreateRuntimeParams.
func FromDomainCreateRuntimeParams(p types.CreateRuntimeParams) (db.CreateRuntimeParams, error) {
	agentID, err := stringToUUID(p.AgentID)
	if err != nil {
		return db.CreateRuntimeParams{}, fmt.Errorf("convert agent id: %w", err)
	}
	return db.CreateRuntimeParams{
		AgentID:          agentID,
		DaemonID:         p.DaemonID,
		Provider:         db.AgentProvider(p.Provider),
		Version:          ptrToNullString(p.Version),
		Status:           db.RuntimeStatus(p.Status),
		SessionTokenHash: ptrToNullString(p.SessionTokenHash),
		SessionExpiresAt: ptrToNullTime(p.SessionExpiresAt),
		PublicKey:        ptrToNullString(p.PublicKey),
	}, nil
}

// ===========================================================================
// Member domain (Auth subdomain)
// ===========================================================================

// ToDomainMember converts db.Member to types.Member.
//
// Note: db.Member contains the PasswordHash sensitive field; types.Member intentionally does not (the domain layer does not expose the password hash).
// This function discards the PasswordHash field.
func ToDomainMember(m db.Member) (types.Member, error) {
	return types.Member{
		ID:        m.ID.String(),
		Name:      m.Name,
		Email:     m.Email,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

// ===========================================================================
// GitCredential domain (Auth subdomain)
// ===========================================================================

// ToDomainGitCredential converts db.GitCredential to types.GitCredential.
func ToDomainGitCredential(g db.GitCredential) (types.GitCredential, error) {
	return types.GitCredential{
		ID:           g.ID.String(),
		ProjectID:    g.ProjectID.String(),
		RepoURL:      g.RepoUrl,
		Username:     g.Username,
		EncryptedPAT: g.EncryptedPat,
		CreatedBy:    nullUUIDToString(g.CreatedBy),
		CreatedAt:    g.CreatedAt,
		UpdatedAt:    g.UpdatedAt,
	}, nil
}

// ToDomainGitCredentialSlice converts []db.GitCredential to []types.GitCredential.
func ToDomainGitCredentialSlice(gs []db.GitCredential) ([]types.GitCredential, error) {
	out := make([]types.GitCredential, 0, len(gs))
	for _, g := range gs {
		d, err := ToDomainGitCredential(g)
		if err != nil {
			return nil, fmt.Errorf("convert git credential: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// ===========================================================================
// Agent domain converters (AgentSkill, AgentMcpServer, McpServer, Rows)
// ===========================================================================

// FromDomainAddAgentSkillParams converts types.AddAgentSkillParams to db.AddAgentSkillParams.
func FromDomainAddAgentSkillParams(p types.AddAgentSkillParams) (db.AddAgentSkillParams, error) {
	agentID, err := stringToUUID(p.AgentID)
	if err != nil {
		return db.AddAgentSkillParams{}, fmt.Errorf("convert agent id: %w", err)
	}
	skillID, err := stringToUUID(p.SkillID)
	if err != nil {
		return db.AddAgentSkillParams{}, fmt.Errorf("convert skill id: %w", err)
	}
	return db.AddAgentSkillParams{
		AgentID: agentID,
		SkillID: skillID,
		Enabled: p.Enabled,
	}, nil
}

// FromDomainRemoveAgentSkillParams converts types.RemoveAgentSkillParams to db.RemoveAgentSkillParams.
func FromDomainRemoveAgentSkillParams(p types.RemoveAgentSkillParams) (db.RemoveAgentSkillParams, error) {
	agentID, err := stringToUUID(p.AgentID)
	if err != nil {
		return db.RemoveAgentSkillParams{}, fmt.Errorf("convert agent id: %w", err)
	}
	skillID, err := stringToUUID(p.SkillID)
	if err != nil {
		return db.RemoveAgentSkillParams{}, fmt.Errorf("convert skill id: %w", err)
	}
	return db.RemoveAgentSkillParams{
		AgentID: agentID,
		SkillID: skillID,
	}, nil
}

// FromDomainAddAgentMcpServerParams converts types.AddAgentMcpServerParams to db.AddAgentMcpServerParams.
func FromDomainAddAgentMcpServerParams(p types.AddAgentMcpServerParams) (db.AddAgentMcpServerParams, error) {
	agentID, err := stringToUUID(p.AgentID)
	if err != nil {
		return db.AddAgentMcpServerParams{}, fmt.Errorf("convert agent id: %w", err)
	}
	mcpServerID, err := stringToUUID(p.McpServerID)
	if err != nil {
		return db.AddAgentMcpServerParams{}, fmt.Errorf("convert mcp server id: %w", err)
	}
	return db.AddAgentMcpServerParams{
		AgentID:     agentID,
		McpServerID: mcpServerID,
		Enabled:     p.Enabled,
	}, nil
}

// FromDomainRemoveAgentMcpServerParams converts types.RemoveAgentMcpServerParams to db.RemoveAgentMcpServerParams.
func FromDomainRemoveAgentMcpServerParams(p types.RemoveAgentMcpServerParams) (db.RemoveAgentMcpServerParams, error) {
	agentID, err := stringToUUID(p.AgentID)
	if err != nil {
		return db.RemoveAgentMcpServerParams{}, fmt.Errorf("convert agent id: %w", err)
	}
	mcpServerID, err := stringToUUID(p.McpServerID)
	if err != nil {
		return db.RemoveAgentMcpServerParams{}, fmt.Errorf("convert mcp server id: %w", err)
	}
	return db.RemoveAgentMcpServerParams{
		AgentID:     agentID,
		McpServerID: mcpServerID,
	}, nil
}

// ToDomainAgentSkill converts db.AgentSkill to types.AgentSkill.
func ToDomainAgentSkill(as db.AgentSkill) (types.AgentSkill, error) {
	return types.AgentSkill{
		AgentID:   as.AgentID.String(),
		SkillID:   as.SkillID.String(),
		Enabled:   as.Enabled,
		CreatedAt: as.CreatedAt,
	}, nil
}

// ToDomainAgentMcpServer converts db.AgentMcpServer to types.AgentMcpServer.
func ToDomainAgentMcpServer(ams db.AgentMcpServer) (types.AgentMcpServer, error) {
	return types.AgentMcpServer{
		AgentID:     ams.AgentID.String(),
		McpServerID: ams.McpServerID.String(),
		Enabled:     ams.Enabled,
		CreatedAt:   ams.CreatedAt,
	}, nil
}

// ToDomainListAgentSkillsRow converts db.ListAgentSkillsRow to types.ListAgentSkillsRow.
func ToDomainListAgentSkillsRow(r db.ListAgentSkillsRow) (types.ListAgentSkillsRow, error) {
	return types.ListAgentSkillsRow{
		ID:             r.ID.String(),
		WorkspaceID:    r.WorkspaceID.String(),
		Name:           r.Name,
		Description:    nullStringToPtr(r.Description),
		Category:       nullStringToPtr(r.Category),
		PromptTemplate: nullStringToPtr(r.PromptTemplate),
		CreatedAt:      r.CreatedAt,
		Enabled:        r.Enabled,
		AssignedAt:     r.AssignedAt,
	}, nil
}

// ToDomainListAgentSkillsRowSlice converts []db.ListAgentSkillsRow to []types.ListAgentSkillsRow.
func ToDomainListAgentSkillsRowSlice(rs []db.ListAgentSkillsRow) ([]types.ListAgentSkillsRow, error) {
	out := make([]types.ListAgentSkillsRow, 0, len(rs))
	for _, r := range rs {
		d, err := ToDomainListAgentSkillsRow(r)
		if err != nil {
			return nil, fmt.Errorf("convert list agent skills row: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// ToDomainListAgentMcpServersRow converts db.ListAgentMcpServersRow to types.ListAgentMcpServersRow.
func ToDomainListAgentMcpServersRow(r db.ListAgentMcpServersRow) (types.ListAgentMcpServersRow, error) {
	return types.ListAgentMcpServersRow{
		ID:          r.ID.String(),
		WorkspaceID: r.WorkspaceID.String(),
		Name:        r.Name,
		Url:         r.Url,
		Type:        nullStringToPtr(r.Type),
		AuthType:    string(r.AuthType),
		EnvVars:     nullRawToRaw(r.EnvVars),
		Status:      r.Status,
		CreatedAt:   r.CreatedAt,
		Enabled:     r.Enabled,
		AssignedAt:  r.AssignedAt,
	}, nil
}

// ToDomainListAgentMcpServersRowSlice converts []db.ListAgentMcpServersRow to []types.ListAgentMcpServersRow.
func ToDomainListAgentMcpServersRowSlice(rs []db.ListAgentMcpServersRow) ([]types.ListAgentMcpServersRow, error) {
	out := make([]types.ListAgentMcpServersRow, 0, len(rs))
	for _, r := range rs {
		d, err := ToDomainListAgentMcpServersRow(r)
		if err != nil {
			return nil, fmt.Errorf("convert list agent mcp servers row: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainUpdateMcpServerParams converts types.UpdateMcpServerParams to db.UpdateMcpServerParams.
//
// Note: types.UpdateMcpServerParams does not contain Status/Url fields (the update only changes name/url/type/auth/env);
// here only the existing fields are mapped. To update status, use UpdateMcpServerStatus.
func FromDomainUpdateMcpServerParams(p types.UpdateMcpServerParams) (db.UpdateMcpServerParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateMcpServerParams{}, fmt.Errorf("convert mcp server id: %w", err)
	}
	return db.UpdateMcpServerParams{
		ID:       id,
		Name:     p.Name,
		Url:      p.URL,
		Type:     ptrToNullString(p.Type),
		AuthType: db.McpAuthType(p.AuthType),
		EnvVars:  rawToNullRaw(p.EnvVars),
		Status:   "active", // default status; status is not changed on update
	}, nil
}

// ToDomainMcpServer converts db.McpServer to types.McpServer.
func ToDomainMcpServer(ms db.McpServer) (types.McpServer, error) {
	return types.McpServer{
		ID:          ms.ID.String(),
		WorkspaceID: ms.WorkspaceID.String(),
		Name:        ms.Name,
		URL:         ms.Url,
		Type:        nullStringToValue(ms.Type),
		AuthType:    string(ms.AuthType),
		EnvVars:     nullRawToRaw(ms.EnvVars),
		Status:      ms.Status,
		CreatedAt:   ms.CreatedAt,
	}, nil
}

// fromDomainCreateMcpServerParams converts types.CreateMcpServerParams to db.CreateMcpServerParams.
func fromDomainCreateMcpServerParams(p types.CreateMcpServerParams) (db.CreateMcpServerParams, error) {
	wsUUID, err := uuid.Parse(p.WorkspaceID)
	if err != nil {
		return db.CreateMcpServerParams{}, fmt.Errorf("parse workspace id: %w", err)
	}
	return db.CreateMcpServerParams{
		WorkspaceID: wsUUID,
		Name:        p.Name,
		Url:         p.URL,
		Type:        ptrToNullString(p.Type),
		AuthType:    db.McpAuthType(p.AuthType),
		EnvVars:     rawToNullRaw(p.EnvVars),
	}, nil
}

// fromDomainUpdateMcpServerStatusParams converts types.UpdateMcpServerStatusParams to db.UpdateMcpServerStatusParams.
func fromDomainUpdateMcpServerStatusParams(p types.UpdateMcpServerStatusParams) (db.UpdateMcpServerStatusParams, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return db.UpdateMcpServerStatusParams{}, fmt.Errorf("parse mcp server id: %w", err)
	}
	return db.UpdateMcpServerStatusParams{
		ID:     id,
		Status: p.Status,
	}, nil
}

// ToDomainGetInProgressNodesByAgentRow converts db.GetInProgressNodesByAgentRow to types.GetInProgressNodesByAgentRow.
func ToDomainGetInProgressNodesByAgentRow(r db.GetInProgressNodesByAgentRow) (types.GetInProgressNodesByAgentRow, error) {
	return types.GetInProgressNodesByAgentRow{
		ID:                   r.ID.String(),
		TaskID:               r.TaskID,
		Name:                 r.Name,
		Description:          r.Description.String,
		SortOrder:            r.SortOrder,
		NodeType:             string(r.NodeType),
		Status:               string(r.Status),
		AssigneeType:         string(r.AssigneeType),
		AssigneeID:           nullUUIDToString(r.AssigneeID),
		ReservedForAgentID:   nullUUIDToString(r.ReservedForAgentID),
		RejectCount:          r.RejectCount,
		MaxRejectCycles:      r.MaxRejectCycles,
		TimeoutMinutes:       r.TimeoutMinutes,
		Version:              r.Version,
		CompletedAt:          nullTimeToPtr(r.CompletedAt),
		CompletedBy:          nullUUIDToString(r.CompletedBy),
		Summary:              r.Summary,
		PreviousSummary:      r.PreviousSummary,
		ReservationExpiresAt: nullTimeToPtr(r.ReservationExpiresAt),
		ReadonlyDirs:         nullRawToRaw(r.ReadonlyDirs),
		FullControlDirs:      nullRawToRaw(r.FullControlDirs),
		DependsOn:            uuidSliceToStringSlice(r.DependsOn),
		CreatedAt:            r.CreatedAt,
		UpdatedAt:            r.UpdatedAt,
		ProjectID:            r.ProjectID.String(),
	}, nil
}


// ToDomainGetInProgressNodesByAgentRowSlice converts []db.GetInProgressNodesByAgentRow to []types.GetInProgressNodesByAgentRow.
func ToDomainGetInProgressNodesByAgentRowSlice(rs []db.GetInProgressNodesByAgentRow) ([]types.GetInProgressNodesByAgentRow, error) {
	out := make([]types.GetInProgressNodesByAgentRow, 0, len(rs))
	for _, r := range rs {
		d, err := ToDomainGetInProgressNodesByAgentRow(r)
		if err != nil {
			return nil, fmt.Errorf("convert in-progress nodes row: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// ToDomainProjectMember converts db.ProjectMember to types.ProjectMember.
func ToDomainProjectMember(m db.ProjectMember) (types.ProjectMember, error) {
	return types.ProjectMember{
		ID:         m.ID.String(),
		ProjectID:  m.ProjectID.String(),
		MemberType: m.MemberType,
		AgentID:    nullUUIDToString(m.AgentID),
		MemberID:   nullUUIDToString(m.MemberID),
		Role:       m.Role,
		CreatedAt:  m.CreatedAt,
	}, nil
}

// ToDomainProjectMemberSlice converts []db.ProjectMember to []types.ProjectMember.
func ToDomainProjectMemberSlice(ms []db.ProjectMember) ([]types.ProjectMember, error) {
	out := make([]types.ProjectMember, 0, len(ms))
	for _, m := range ms {
		d, err := ToDomainProjectMember(m)
		if err != nil {
			return nil, fmt.Errorf("convert project member: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateProjectMemberParams converts types.CreateProjectMemberParams to db.CreateProjectMemberParams.
func FromDomainCreateProjectMemberParams(p types.CreateProjectMemberParams) (db.CreateProjectMemberParams, error) {
	pid, err := stringToUUID(p.ProjectID)
	if err != nil {
		return db.CreateProjectMemberParams{}, fmt.Errorf("convert project id: %w", err)
	}
	return db.CreateProjectMemberParams{
		ProjectID:  pid,
		MemberType: p.MemberType,
		AgentID:    stringToNullUUID(p.AgentID),
		MemberID:   stringToNullUUID(p.MemberID),
		Role:       p.Role,
	}, nil
}

// ToDomainProjectReviewer converts db.ProjectReviewer to types.ProjectReviewer.
func ToDomainProjectReviewer(r db.ProjectReviewer) (types.ProjectReviewer, error) {
	return types.ProjectReviewer{
		ID:         r.ID.String(),
		ProjectID:  r.ProjectID.String(),
		MemberType: r.MemberType,
		AgentID:    nullUUIDToString(r.AgentID),
		MemberID:   nullUUIDToString(r.MemberID),
		CreatedAt:  r.CreatedAt,
	}, nil
}

// ToDomainProjectReviewerSlice converts []db.ProjectReviewer to []types.ProjectReviewer.
func ToDomainProjectReviewerSlice(rs []db.ProjectReviewer) ([]types.ProjectReviewer, error) {
	out := make([]types.ProjectReviewer, 0, len(rs))
	for _, r := range rs {
		d, err := ToDomainProjectReviewer(r)
		if err != nil {
			return nil, fmt.Errorf("convert project reviewer: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateProjectReviewerParams converts types.CreateProjectReviewerParams to db.CreateProjectReviewerParams.
func FromDomainCreateProjectReviewerParams(p types.CreateProjectReviewerParams) (db.CreateProjectReviewerParams, error) {
	pid, err := stringToUUID(p.ProjectID)
	if err != nil {
		return db.CreateProjectReviewerParams{}, fmt.Errorf("convert project id: %w", err)
	}
	return db.CreateProjectReviewerParams{
		ProjectID:  pid,
		MemberType: p.MemberType,
		AgentID:    stringToNullUUID(p.AgentID),
		MemberID:   stringToNullUUID(p.MemberID),
	}, nil
}

// FromDomainIsAgentProjectMemberParams converts types.IsAgentProjectMemberParams to db.IsAgentProjectMemberParams.
func FromDomainIsAgentProjectMemberParams(p types.IsAgentProjectMemberParams) (db.IsAgentProjectMemberParams, error) {
	pid, err := stringToUUID(p.ProjectID)
	if err != nil {
		return db.IsAgentProjectMemberParams{}, fmt.Errorf("convert project id: %w", err)
	}
	var agentID uuid.NullUUID
	if p.AgentID != "" {
		aid, err := uuid.Parse(p.AgentID)
		if err != nil {
			return db.IsAgentProjectMemberParams{}, fmt.Errorf("convert agent id: %w", err)
		}
		agentID = uuid.NullUUID{UUID: aid, Valid: true}
	}
	return db.IsAgentProjectMemberParams{
		ProjectID: pid,
		AgentID:   agentID,
	}, nil
}

// FromDomainListProjectsByAgentMembershipParams converts types.ListProjectsByAgentMembershipParams to db.ListProjectsByAgentMembershipParams.
func FromDomainListProjectsByAgentMembershipParams(p types.ListProjectsByAgentMembershipParams) (db.ListProjectsByAgentMembershipParams, error) {
	ws, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.ListProjectsByAgentMembershipParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	return db.ListProjectsByAgentMembershipParams{
		WorkspaceID: ws,
		AgentID:     stringToNullUUID(p.AgentID),
	}, nil
}

// ===========================================================================
// Member domain helpers (Workspace subdomain)
// ===========================================================================

// ToDomainMemberSlice converts []db.Member to []types.Member.
func ToDomainMemberSlice(ms []db.Member) ([]types.Member, error) {
	out := make([]types.Member, 0, len(ms))
	for _, m := range ms {
		d, err := ToDomainMember(m)
		if err != nil {
			return nil, fmt.Errorf("convert member: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateMemberParams converts types.CreateMemberParams to db.CreateMemberParams.
func FromDomainCreateMemberParams(p types.CreateMemberParams) (db.CreateMemberParams, error) {
	return db.CreateMemberParams{
		Name:  p.Name,
		Email: p.Email,
	}, nil
}

// ToDomainWorkspaceMember converts db.WorkspaceMember to types.WorkspaceMember.
func ToDomainWorkspaceMember(wm db.WorkspaceMember) (types.WorkspaceMember, error) {
	return types.WorkspaceMember{
		ID:          wm.ID.String(),
		WorkspaceID: wm.WorkspaceID.String(),
		MemberID:    wm.MemberID.String(),
		Role:        wm.Role,
		CreatedAt:   wm.CreatedAt,
		UpdatedAt:   wm.UpdatedAt,
	}, nil
}

// ToDomainWorkspaceMemberSlice converts []db.WorkspaceMember to []types.WorkspaceMember.
func ToDomainWorkspaceMemberSlice(wms []db.WorkspaceMember) ([]types.WorkspaceMember, error) {
	out := make([]types.WorkspaceMember, 0, len(wms))
	for _, wm := range wms {
		d, err := ToDomainWorkspaceMember(wm)
		if err != nil {
			return nil, fmt.Errorf("convert workspace member: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FromDomainCreateWorkspaceMemberParams converts types.CreateWorkspaceMemberParams to db.CreateWorkspaceMemberParams.
func FromDomainCreateWorkspaceMemberParams(p types.CreateWorkspaceMemberParams) (db.CreateWorkspaceMemberParams, error) {
	wsID, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.CreateWorkspaceMemberParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	mID, err := stringToUUID(p.MemberID)
	if err != nil {
		return db.CreateWorkspaceMemberParams{}, fmt.Errorf("convert member id: %w", err)
	}
	return db.CreateWorkspaceMemberParams{
		WorkspaceID: wsID,
		MemberID:    mID,
		Role:        p.Role,
	}, nil
}

// FromDomainUpdateMemberRoleParams converts types.UpdateMemberRoleParams to db.UpdateMemberRoleParams.
func FromDomainUpdateMemberRoleParams(p types.UpdateMemberRoleParams) (db.UpdateMemberRoleParams, error) {
	wsID, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.UpdateMemberRoleParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	mID, err := stringToUUID(p.MemberID)
	if err != nil {
		return db.UpdateMemberRoleParams{}, fmt.Errorf("convert member id: %w", err)
	}
	return db.UpdateMemberRoleParams{
		WorkspaceID: wsID,
		MemberID:    mID,
		Role:        p.Role,
	}, nil
}

// FromDomainGetWorkspaceMemberParams converts types.GetWorkspaceMemberParams to db.GetWorkspaceMemberParams.
func FromDomainGetWorkspaceMemberParams(p types.GetWorkspaceMemberParams) (db.GetWorkspaceMemberParams, error) {
	wsID, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.GetWorkspaceMemberParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	mID, err := stringToUUID(p.MemberID)
	if err != nil {
		return db.GetWorkspaceMemberParams{}, fmt.Errorf("convert member id: %w", err)
	}
	return db.GetWorkspaceMemberParams{
		WorkspaceID: wsID,
		MemberID:    mID,
	}, nil
}

// FromDomainGetWorkspaceMemberRoleParams converts types.GetWorkspaceMemberRoleParams to db.GetWorkspaceMemberRoleParams.
func FromDomainGetWorkspaceMemberRoleParams(p types.GetWorkspaceMemberRoleParams) (db.GetWorkspaceMemberRoleParams, error) {
	wsID, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.GetWorkspaceMemberRoleParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	mID, err := stringToUUID(p.MemberID)
	if err != nil {
		return db.GetWorkspaceMemberRoleParams{}, fmt.Errorf("convert member id: %w", err)
	}
	return db.GetWorkspaceMemberRoleParams{
		WorkspaceID: wsID,
		MemberID:    mID,
	}, nil
}

// ToDomainListMembersByWorkspaceRow converts db.ListMembersByWorkspaceRow to types.ListMembersByWorkspaceRow.
func ToDomainListMembersByWorkspaceRow(r db.ListMembersByWorkspaceRow) (types.ListMembersByWorkspaceRow, error) {
	return types.ListMembersByWorkspaceRow{
		ID:                r.ID.String(),
		Name:              r.Name,
		Email:             r.Email,
		PasswordHash:      r.PasswordHash,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
		WorkspaceRole:     r.WorkspaceRole,
		WorkspaceJoinedAt: r.WorkspaceJoinedAt,
	}, nil
}

// ToDomainListMembersByWorkspaceRowSlice converts []db.ListMembersByWorkspaceRow to []types.ListMembersByWorkspaceRow.
func ToDomainListMembersByWorkspaceRowSlice(rs []db.ListMembersByWorkspaceRow) ([]types.ListMembersByWorkspaceRow, error) {
	out := make([]types.ListMembersByWorkspaceRow, 0, len(rs))
	for _, r := range rs {
		d, err := ToDomainListMembersByWorkspaceRow(r)
		if err != nil {
			return nil, fmt.Errorf("convert list members row: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// ===========================================================================
// Convenience conversion functions (for direct param passing in the service layer)
// ===========================================================================

// ToDBGetWorkspaceMemberParams builds db.GetWorkspaceMemberParams from uuid.UUID.
func ToDBGetWorkspaceMemberParams(workspaceID, memberID uuid.UUID) db.GetWorkspaceMemberParams {
	return db.GetWorkspaceMemberParams{
		WorkspaceID: workspaceID,
		MemberID:    memberID,
	}
}

// ToDBGetWorkspaceMemberRoleParams builds db.GetWorkspaceMemberRoleParams from uuid.UUID.
func ToDBGetWorkspaceMemberRoleParams(workspaceID, memberID uuid.UUID) db.GetWorkspaceMemberRoleParams {
	return db.GetWorkspaceMemberRoleParams{
		WorkspaceID: workspaceID,
		MemberID:    memberID,
	}
}

// FromDomainCreateMemberParamsSimple builds db.CreateMemberParams from simple params.
func FromDomainCreateMemberParamsSimple(name, email string) db.CreateMemberParams {
	return db.CreateMemberParams{
		Name:  name,
		Email: email,
	}
}

// FromDomainCreateWorkspaceParamsSimple builds db.CreateWorkspaceParams from simple params.
func FromDomainCreateWorkspaceParamsSimple(name, description, issuePrefix string) db.CreateWorkspaceParams {
	return db.CreateWorkspaceParams{
		Name:        name,
		Description: sql.NullString{String: description, Valid: true},
		IssuePrefix: issuePrefix,
	}
}

// ToDomainExecutionSession converts db.ExecutionSession to types.ExecutionSession.
func ToDomainExecutionSession(s db.ExecutionSession) (types.ExecutionSession, error) {
	taskNodeID, err := stringToUUID(s.TaskNodeID.String())
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("convert task_node_id: %w", err)
	}
	_ = taskNodeID
	return types.ExecutionSession{
		ID:              s.ID.String(),
		RuntimeID:       nullUUIDToString(s.RuntimeID),
		AgentID:         nullUUIDToString(s.AgentID),
		TaskNodeID:      s.TaskNodeID.String(),
		Attempt:         s.Attempt,
		Status:          s.Status,
		Workdir:         nullStringToPtr(s.Workdir),
		Branch:          nullStringToPtr(s.Branch),
		BaseCommit:      nullStringToPtr(s.BaseCommit),
		HeadCommit:      nullStringToPtr(s.HeadCommit),
		ClaudeSessionID: nullStringToPtr(s.ClaudeSessionID),
		StartedAt:       s.StartedAt,
		CompletedAt:     nullTimeToPtr(s.CompletedAt),
		InterruptedAt:   nullTimeToPtr(s.InterruptedAt),
		CreatedAt:       s.CreatedAt,
	}, nil
}

// FromDomainCreateExecutionSessionParams converts types.CreateExecutionSessionParams to db.CreateExecutionSessionParams.
func FromDomainCreateExecutionSessionParams(p types.CreateExecutionSessionParams) (db.CreateExecutionSessionParams, error) {
	taskNodeID, err := stringToUUID(p.TaskNodeID)
	if err != nil {
		return db.CreateExecutionSessionParams{}, fmt.Errorf("convert task_node_id: %w", err)
	}
	return db.CreateExecutionSessionParams{
		RuntimeID:       stringToNullUUID(p.RuntimeID),
		AgentID:         stringToNullUUID(p.AgentID),
		TaskNodeID:      taskNodeID,
		Attempt:         p.Attempt,
		Status:          p.Status,
		Workdir:         ptrToNullString(p.Workdir),
		Branch:          ptrToNullString(p.Branch),
		BaseCommit:      ptrToNullString(p.BaseCommit),
		ClaudeSessionID: ptrToNullString(p.ClaudeSessionID),
	}, nil
}

// FromDomainGetActiveSessionByAgentAndWorkdirParams converts types.GetActiveSessionByAgentAndWorkdirParams to db.GetActiveSessionByAgentAndWorkdirParams.
func FromDomainGetActiveSessionByAgentAndWorkdirParams(p types.GetActiveSessionByAgentAndWorkdirParams) (db.GetActiveSessionByAgentAndWorkdirParams, error) {
	return db.GetActiveSessionByAgentAndWorkdirParams{
		AgentID: stringToNullUUID(p.AgentID),
		Workdir: ptrToNullString(p.Workdir),
	}, nil
}

// FromDomainUpdateSessionClaudeIDParams converts types.UpdateSessionClaudeIDParams to db.UpdateSessionClaudeIDParams.
func FromDomainUpdateSessionClaudeIDParams(p types.UpdateSessionClaudeIDParams) (db.UpdateSessionClaudeIDParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateSessionClaudeIDParams{}, fmt.Errorf("convert session id: %w", err)
	}
	return db.UpdateSessionClaudeIDParams{
		ID:              id,
		ClaudeSessionID: ptrToNullString(p.ClaudeSessionID),
	}, nil
}

// ToDomainGetTemplateStatsRow converts db.GetTemplateStatsRow to types.GetTemplateStatsRow.
func ToDomainGetTemplateStatsRow(r db.GetTemplateStatsRow) types.GetTemplateStatsRow {
	var avgCompletion float64
	switch v := r.AvgCompletionSeconds.(type) {
	case float64:
		avgCompletion = v
	case int64:
		avgCompletion = float64(v)
	}
	var rejectRate float64
	switch v := r.RejectRate.(type) {
	case float64:
		rejectRate = v
	case int64:
		rejectRate = float64(v)
	}
	return types.GetTemplateStatsRow{
		UsageCount:           r.UsageCount,
		AvgCompletionSeconds: avgCompletion,
		RejectRate:           rejectRate,
	}
}

// ToDomainWorkflowTriggerRun converts db.WorkflowTriggerRun to types.WorkflowTriggerRun.
func ToDomainWorkflowTriggerRun(r db.WorkflowTriggerRun) (types.WorkflowTriggerRun, error) {
	return types.WorkflowTriggerRun{
		ID:                 r.ID.String(),
		WorkspaceID:        r.WorkspaceID.String(),
		ProjectID:          r.ProjectID.String(),
		WorkflowTemplateID: r.WorkflowTemplateID.String(),
		TriggerType:        string(r.TriggerType),
		ExternalKey:        r.ExternalKey,
		Status:             r.Status,
		TaskID:             nullInt32ToPtr(r.TaskID),
		Payload:            r.Payload,
		Error:              r.Error,
		CreatedAt:          r.CreatedAt,
	}, nil
}

// FromDomainCreateWorkflowTriggerRunParams converts domain params to db params.
func FromDomainCreateWorkflowTriggerRunParams(p types.CreateWorkflowTriggerRunParams) (db.CreateWorkflowTriggerRunParams, error) {
	wsID, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.CreateWorkflowTriggerRunParams{}, fmt.Errorf("convert workspace id: %w", err)
	}
	projID, err := stringToUUID(p.ProjectID)
	if err != nil {
		return db.CreateWorkflowTriggerRunParams{}, fmt.Errorf("convert project id: %w", err)
	}
	tplID, err := stringToUUID(p.WorkflowTemplateID)
	if err != nil {
		return db.CreateWorkflowTriggerRunParams{}, fmt.Errorf("convert template id: %w", err)
	}
	return db.CreateWorkflowTriggerRunParams{
		WorkspaceID:        wsID,
		ProjectID:          projID,
		WorkflowTemplateID: tplID,
		TriggerType:        db.WorkflowTriggerType(p.TriggerType),
		ExternalKey:        p.ExternalKey,
		Status:             p.Status,
		Payload:            p.Payload,
	}, nil
}

// FromDomainMarkWorkflowTriggerRunCompletedParams converts domain params to db params.
func FromDomainMarkWorkflowTriggerRunCompletedParams(p types.MarkWorkflowTriggerRunCompletedParams) (db.MarkWorkflowTriggerRunCompletedParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.MarkWorkflowTriggerRunCompletedParams{}, fmt.Errorf("convert id: %w", err)
	}
	return db.MarkWorkflowTriggerRunCompletedParams{
		ID:     id,
		TaskID: ptrToNullInt32(p.TaskID),
	}, nil
}

// FromDomainMarkWorkflowTriggerRunFailedParams converts domain params to db params.
func FromDomainMarkWorkflowTriggerRunFailedParams(p types.MarkWorkflowTriggerRunFailedParams) (db.MarkWorkflowTriggerRunFailedParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.MarkWorkflowTriggerRunFailedParams{}, fmt.Errorf("convert id: %w", err)
	}
	return db.MarkWorkflowTriggerRunFailedParams{
		ID:    id,
		Error: p.Error,
	}, nil
}

// FromDomainListDueScheduledWorkflowTemplatesParams converts domain params to db params.
func FromDomainListDueScheduledWorkflowTemplatesParams(p types.ListDueScheduledWorkflowTemplatesParams) (db.ListDueScheduledWorkflowTemplatesParams, error) {
	return db.ListDueScheduledWorkflowTemplatesParams{
		NextRunAt: ptrToNullTime(p.NextRunAt),
		Limit:     p.Limit,
	}, nil
}

// FromDomainUpdateWorkflowTemplateTriggerScheduleParams converts domain params to db params.
func FromDomainUpdateWorkflowTemplateTriggerScheduleParams(p types.UpdateWorkflowTemplateTriggerScheduleParams) (db.UpdateWorkflowTemplateTriggerScheduleParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateWorkflowTemplateTriggerScheduleParams{}, fmt.Errorf("convert id: %w", err)
	}
	return db.UpdateWorkflowTemplateTriggerScheduleParams{
		ID:              id,
		NextRunAt:       ptrToNullTime(p.NextRunAt),
		LastTriggeredAt: ptrToNullTime(p.LastTriggeredAt),
	}, nil
}
