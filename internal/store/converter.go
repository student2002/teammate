// converter.go 实现 sqlc 生成类型（db 包）与 domain 类型（types 包）之间的双向转换。
//
// 本文件是四层架构中 Store 层的"类型隔离边界"：
//   - Store 的公共方法签名只暴露 types.* domain 类型
//   - Store 内部继续使用 sqlc 生成的 db.* 类型与底层查询交互
//   - 本转换层在 Store 方法体首尾做 db.* ↔ types.* 的转换
//
// 字段映射规则（domain 风格，不依赖 database/sql）：
//   - uuid.UUID → string（.String() / uuid.Parse）
//   - uuid.NullUUID → *string（nil 表示 NULL）
//   - []uuid.UUID → []string
//   - sql.NullString → *string
//   - sql.NullTime → *time.Time
//   - sql.NullInt32 → *int32
//   - pqtype.NullRawMessage → json.RawMessage（nil 表示 NULL）
//   - pqtype.Inet → string（原始 CIDR/IP）
//   - 枚举类型（TaskStatus 等 string 别名）→ string，零成本透传
//
// 错误约定：
//   - 所有 toDomainXxx 和 FromDomainXxxParams 都返回 error（uuid.Parse 可能失败）
//   - 错误用 fmt.Errorf 包装，不裸返回
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
// 基础转换辅助函数
// ---------------------------------------------------------------------------

// uuidToString 将 uuid.UUID 转换为 string。
func uuidToString(u uuid.UUID) string { return u.String() }

// stringToUUID 将 string 转换为 uuid.UUID，解析失败时返回错误。
func stringToUUID(s string) (uuid.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse uuid %q: %w", s, err)
	}
	return u, nil
}

// nullUUIDToString 将 uuid.NullUUID 转换为 *string，nil 表示 NULL。
func nullUUIDToString(nu uuid.NullUUID) *string {
	if !nu.Valid {
		return nil
	}
	s := nu.UUID.String()
	return &s
}

// stringToNullUUID 将 *string 转换为 uuid.NullUUID，nil 表示 NULL。
// 解析失败时返回零值 NullUUID（Valid=false）。
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

// nullUUIDToStringRequired 与 nullUUIDToString 相同，保留以备语义区分。
func nullUUIDToStringRequired(nu uuid.NullUUID) (string, error) {
	if !nu.Valid {
		return "", fmt.Errorf("null uuid where required expected")
	}
	return nu.UUID.String(), nil
}

// uuidSliceToStringSlice 将 []uuid.UUID 转换为 []string。
func uuidSliceToStringSlice(us []uuid.UUID) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, u.String())
	}
	return out
}

// stringSliceToUUIDSlice 将 []string 转换为 []uuid.UUID，解析失败时返回错误。
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

// nullTimeToPtr 将 sql.NullTime 转换为 *time.Time，nil 表示 NULL。
func nullTimeToPtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	t := nt.Time
	return &t
}

// ptrToNullTime 将 *time.Time 转换为 sql.NullTime，nil 表示 NULL。
func ptrToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// nullStringToPtr 将 sql.NullString 转换为 *string，nil 表示 NULL。
func nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

// nullStringToValue 将 sql.NullString 转换为 string，NULL 退化为空串。
// 用于 domain 结构体里 Type 字段是 string（非 *string）的情况。
func nullStringToValue(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}

// ptrToNullString 将 *string 转换为 sql.NullString，nil 表示 NULL。
func ptrToNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// nullInt32ToPtr 将 sql.NullInt32 转换为 *int32，nil 表示 NULL。
func nullInt32ToPtr(ni sql.NullInt32) *int32 {
	if !ni.Valid {
		return nil
	}
	v := ni.Int32
	return &v
}

// ptrToNullInt32 将 *int32 转换为 sql.NullInt32，nil 表示 NULL。
func ptrToNullInt32(i *int32) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *i, Valid: true}
}

// nullRawToRaw 将 pqtype.NullRawMessage 转换为 json.RawMessage，nil 表示 NULL。
func nullRawToRaw(nrm pqtype.NullRawMessage) json.RawMessage {
	if !nrm.Valid {
		return nil
	}
	return nrm.RawMessage
}

// rawToNullRaw 将 json.RawMessage 转换为 pqtype.NullRawMessage，nil 或空表示 NULL。
// 重要：空 json.RawMessage("") 必须返回 Valid:false，否则 pgx 会把空 []byte 当 binary
// 发送给 jsonb 字段，触发 PG 报错 "invalid input syntax for type json"。
func rawToNullRaw(rm json.RawMessage) pqtype.NullRawMessage {
	if len(rm) == 0 {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: rm, Valid: true}
}

// inetToString 将 pqtype.Inet 转换为 string（原始 CIDR/IP）。
func inetToString(inet pqtype.Inet) string {
	return inet.IPNet.String()
}

// stringToInet 将 string 转换为 pqtype.Inet，解析失败时返回零值。
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
// Task 领域
// ===========================================================================

// ToDomainTask 将 db.Task 转换为 types.Task。
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

// toDomainTaskSlice 将 []db.Task 转换为 []types.Task。
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

// FromDomainCreateTaskParams 将 types.CreateTaskParams 转换为 db.CreateTaskParams。
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

// FromDomainUpdateTaskParams 将 types.UpdateTaskParams 转换为 db.UpdateTaskParams。
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

// FromDomainUpdateTaskStatusParams 将 types.UpdateTaskStatusParams 转换为 db.UpdateTaskStatusParams。
func FromDomainUpdateTaskStatusParams(p types.UpdateTaskStatusParams) (db.UpdateTaskStatusParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateTaskStatusParams{}, fmt.Errorf("convert task id: %w", err)
	}
	return db.UpdateTaskStatusParams{ID: int32(id.ID()), Status: db.TaskStatus(p.Status)}, nil
}

// FromDomainUpdateTaskGitBranchParams 将 types.UpdateTaskGitBranchParams 转换为 db.UpdateTaskGitBranchParams。
func FromDomainUpdateTaskGitBranchParams(p types.UpdateTaskGitBranchParams) (db.UpdateTaskGitBranchParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateTaskGitBranchParams{}, fmt.Errorf("convert task id: %w", err)
	}
	return db.UpdateTaskGitBranchParams{ID: int32(id.ID()), GitBranch: ptrToNullString(p.GitBranch)}, nil
}

// ===========================================================================

// ===========================================================================
// TaskNode 领域
// ===========================================================================

// ToDomainTaskNode 将 db.TaskNode 转换为 types.TaskNode。
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

// toDomainTaskNodeSlice 将 []db.TaskNode 转换为 []types.TaskNode。
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

// FromDomainCreateTaskNodeParams 将 types.CreateTaskNodeParams 转换为 db.CreateTaskNodeParams。
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

// FromDomainClaimTaskNodeParams 将 types.ClaimTaskNodeParams 转换为 db.ClaimTaskNodeParams。
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

// FromDomainClaimTaskNodeByHumanParams 将 types.ClaimTaskNodeByHumanParams 转换为 db.ClaimTaskNodeByHumanParams。
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

// FromDomainReclaimTaskNodeParams 将 types.ReclaimTaskNodeParams 转换为 db.ReclaimTaskNodeParams。
func FromDomainReclaimTaskNodeParams(p types.ReclaimTaskNodeParams) (db.ReclaimTaskNodeParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.ReclaimTaskNodeParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.ReclaimTaskNodeParams{ID: id, Version: p.Version}, nil
}

// FromDomainResetRejectCountParams 将 types.ResetRejectCountParams 转换为 db.ResetRejectCountParams。
func FromDomainResetRejectCountParams(p types.ResetRejectCountParams) (db.ResetRejectCountParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.ResetRejectCountParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.ResetRejectCountParams{ID: id}, nil
}

// FromDomainUpdateNodeSummaryParams 将 types.UpdateNodeSummaryParams 转换为 db.UpdateNodeSummaryParams。
func FromDomainUpdateNodeSummaryParams(p types.UpdateNodeSummaryParams) (db.UpdateNodeSummaryParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateNodeSummaryParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.UpdateNodeSummaryParams{ID: id, Summary: p.Summary}, nil
}

// FromDomainUpdateTaskNodeStatusParams 将 types.UpdateTaskNodeStatusParams 转换为 db.UpdateTaskNodeStatusParams。
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

// FromDomainGetNextTaskNodeParams 将 types.GetNextTaskNodeParams 转换为 db.GetNextTaskNodeParams。
func FromDomainGetNextTaskNodeParams(p types.GetNextTaskNodeParams) (db.GetNextTaskNodeParams, error) {
	id, err := stringToUUID(p.NodeID)
	if err != nil {
		return db.GetNextTaskNodeParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.GetNextTaskNodeParams{TaskID: p.TaskID, ID: id}, nil
}

// FromDomainGetPrevTaskNodeParams 将 types.GetPrevTaskNodeParams 转换为 db.GetPrevTaskNodeParams。
func FromDomainGetPrevTaskNodeParams(p types.GetPrevTaskNodeParams) (db.GetPrevTaskNodeParams, error) {
	id, err := stringToUUID(p.NodeID)
	if err != nil {
		return db.GetPrevTaskNodeParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.GetPrevTaskNodeParams{TaskID: p.TaskID, ID: id}, nil
}

// FromDomainGetPrevStandardNodeAssigneeParams 将 types.GetPrevStandardNodeAssigneeParams 转换为 db.GetPrevStandardNodeAssigneeParams。
func FromDomainGetPrevStandardNodeAssigneeParams(p types.GetPrevStandardNodeAssigneeParams) (db.GetPrevStandardNodeAssigneeParams, error) {
	id, err := stringToUUID(p.NodeID)
	if err != nil {
		return db.GetPrevStandardNodeAssigneeParams{}, fmt.Errorf("convert node id: %w", err)
	}
	return db.GetPrevStandardNodeAssigneeParams{TaskID: p.TaskID, ID: id}, nil
}

// ===========================================================================
// Comment 领域
// ===========================================================================

// ToDomainComment 将 db.Comment 转换为 types.Comment。
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

// toDomainCommentSlice 将 []db.Comment 转换为 []types.Comment。
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

// FromDomainCreateCommentParams 将 types.CreateCommentParams 转换为 db.CreateCommentParams。
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

// FromDomainUpdateCommentParams 将 types.UpdateCommentParams 转换为 db.UpdateCommentParams。
func FromDomainUpdateCommentParams(p types.UpdateCommentParams) (db.UpdateCommentParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateCommentParams{}, fmt.Errorf("convert comment id: %w", err)
	}
	return db.UpdateCommentParams{ID: id, Content: p.Content}, nil
}

// ===========================================================================
// NodeTransition 领域
// ===========================================================================

// ToDomainNodeTransition 将 db.NodeTransition 转换为 types.NodeTransition。
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

// toDomainNodeTransitionSlice 将 []db.NodeTransition 转换为 []types.NodeTransition。
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

// FromDomainCreateNodeTransitionParams 将 types.CreateNodeTransitionParams 转换为 db.CreateNodeTransitionParams。
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
// TokenUsage 领域
// ===========================================================================

// toDomainTokenUsage 将 db.TokenUsage 转换为 types.TokenUsage。
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

// FromDomainCreateTokenUsageParams 将 types.CreateTokenUsageParams 转换为 db.CreateTokenUsageParams。
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
// Agent 领域
// ===========================================================================

// ToDomainAgent 将 db.Agent 转换为 types.Agent。
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

// toDomainAgentSlice 将 []db.Agent 转换为 []types.Agent。
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

// FromDomainCreateAgentParams 将 types.CreateAgentParams 转换为 db.CreateAgentParams。
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

// FromDomainUpdateAgentParams 将 types.UpdateAgentParams 转换为 db.UpdateAgentParams。
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

// FromDomainUpdateAgentStatusParams 将 types.UpdateAgentStatusParams 转换为 db.UpdateAgentStatusParams。
func FromDomainUpdateAgentStatusParams(p types.UpdateAgentStatusParams) (db.UpdateAgentStatusParams, error) {
	id, err := stringToUUID(p.ID)
	if err != nil {
		return db.UpdateAgentStatusParams{}, fmt.Errorf("convert agent id: %w", err)
	}
	return db.UpdateAgentStatusParams{ID: id, Status: db.AgentStatus(p.Status)}, nil
}

// ===========================================================================
// Workspace 领域
// ===========================================================================

// ToDomainWorkspace 将 db.Workspace 转换为 types.Workspace。
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

// toDomainWorkspaceSlice 将 []db.Workspace 转换为 []types.Workspace。
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

// FromDomainCreateWorkspaceParams 将 types.CreateWorkspaceParams 转换为 db.CreateWorkspaceParams。
func FromDomainCreateWorkspaceParams(p types.CreateWorkspaceParams) (db.CreateWorkspaceParams, error) {
	return db.CreateWorkspaceParams{
		Name:        p.Name,
		Description: ptrToNullString(p.Description),
		IssuePrefix: p.IssuePrefix,
		IsDefault:   p.IsDefault,
	}, nil
}

// FromDomainUpdateWorkspaceParams 将 types.UpdateWorkspaceParams 转换为 db.UpdateWorkspaceParams。
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
// Project 领域
// ===========================================================================

// ToDomainProject 将 db.Project 转换为 types.Project。
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

// toDomainProjectSlice 将 []db.Project 转换为 []types.Project。
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

// FromDomainCreateProjectParams 将 types.CreateProjectParams 转换为 db.CreateProjectParams。
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

// FromDomainUpdateProjectParams 将 types.UpdateProjectParams 转换为 db.UpdateProjectParams。
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
// ListTasks / Subtask / CountTasksByStatus 参数补充（Task 3 试点所需）
// ===========================================================================

// FromDomainListTasksParams 将 types.ListTasksParams 转换为 db.ListTasksParams。
//
// 注意：types.ListTasksParams 当前仅有 WorkspaceID 字段，db 版本需要 ProjectID 与 Status。
// 调用方需在 service 层补齐 Status；此处按零值透传。
func FromDomainListTasksParams(p types.ListTasksParams) (db.ListTasksParams, error) {
	pid, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.ListTasksParams{}, fmt.Errorf("convert project id: %w", err)
	}
	// types.ListTasksParams 无 status 字段，传 NULL（SQL 的 @status IS NULL 分支匹配所有状态）
	return db.ListTasksParams{ProjectID: pid, Status: db.NullTaskStatus{}}, nil
}

// FromDomainListTasksPaginatedParams 将 types.ListTasksPaginatedParams 转换为 db.ListTasksPaginatedParams。
func FromDomainListTasksPaginatedParams(p types.ListTasksPaginatedParams) (db.ListTasksPaginatedParams, error) {
	pid, err := stringToUUID(p.WorkspaceID)
	if err != nil {
		return db.ListTasksPaginatedParams{}, fmt.Errorf("convert project id: %w", err)
	}
	var statusVal db.NullTaskStatus
	if len(p.Statuses) > 0 {
		// 仅取首个状态过滤；多状态过滤需在 types 包扩展参数语义
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

// FromDomainCreateSubtaskParams 将 types.CreateSubtaskParams 转换为 db.CreateSubtaskParams。
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

// FromDomainCountTasksByStatusParams 将 types.CountTasksByStatusParams 转换为 db.CountTasksByStatusParams。
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
// Invitation 领域
// ===========================================================================

// ToDomainInvitation 将 db.Invitation 转换为 types.Invitation。
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

// ToDomainInvitationSlice 将 []db.Invitation 转换为 []types.Invitation。
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

// FromDomainCreateInvitationParams 将 domain 风格的参数组装为 db.CreateInvitationParams。
//
// 注意：types.CreateInvitationParams 与 db.CreateInvitationParams 字段一一对应但类型不同，
// 本函数接受 uuid.UUID/uuid.NullUUID 等 db 风格入参，与 store/invitation.go 的方法签名保持一致。
// 未来若 store/invitation.go 统一改为 string，本函数入参可同步改为 string。
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
// AgentPermission 领域
// ===========================================================================

// ToDomainAgentPermission 将 db.AgentPermission 转换为 types.AgentPermission。
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

// ToDomainAgentPermissionSlice 将 []db.AgentPermission 转换为 []types.AgentPermission。
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
// Memory 领域
// ===========================================================================

// ToDomainMemory 将 db.Memory 转换为 types.Memory。
//
// 注意：db.Memory 有 Embedding interface{} 字段，types.Memory 故意不含 Embedding
// （app 代码从不读 embedding，只写入）。本函数丢弃 Embedding。
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

// ToDomainMemorySlice 将 []db.Memory 转换为 []types.Memory。
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

// FromDomainCreateMemoryParams 将 types.CreateMemoryParams 转换为 db.CreateMemoryParams。
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

// FromDomainListMemoriesByWorkspaceParams 将 domain 风格的参数组装为 db.ListMemoriesByWorkspaceParams。
//
// 注意：db 版本用 sql.NullBool/sql.NullFloat64/sql.NullInt32 包装 nullable 过滤条件，
// domain 飝格用 *bool/*float32/*int32 指针。本函数做指针→NullXX 的转换。
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
// Search 领域
// ===========================================================================

// FromDomainSearchTasksByWorkspaceParams 将 domain 风格的参数组装为 db.SearchTasksByWorkspaceParams。
func FromDomainSearchTasksByWorkspaceParams(workspaceID uuid.UUID, pattern string) db.SearchTasksByWorkspaceParams {
	return db.SearchTasksByWorkspaceParams{
		WorkspaceID: workspaceID,
		Title:       pattern,
	}
}

// FromDomainSearchTasksByWorkspaceAndProjectParams 将 domain 风格的参数组装为 db.SearchTasksByWorkspaceAndProjectParams。
func FromDomainSearchTasksByWorkspaceAndProjectParams(workspaceID, projectID uuid.UUID, pattern string) db.SearchTasksByWorkspaceAndProjectParams {
	return db.SearchTasksByWorkspaceAndProjectParams{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		Title:       pattern,
	}
}

// FromDomainSearchAgentsByWorkspaceParams 将 domain 风格的参数组装为 db.SearchAgentsByWorkspaceParams。
func FromDomainSearchAgentsByWorkspaceParams(workspaceID uuid.UUID, pattern string) db.SearchAgentsByWorkspaceParams {
	return db.SearchAgentsByWorkspaceParams{
		WorkspaceID: workspaceID,
		Name:        pattern,
	}
}

// ===========================================================================
// Community 领域
// ===========================================================================

// ToDomainCommunityWorkflow 将 db.CommunityWorkflow 转换为 types.CommunityWorkflow。
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

// ToDomainCommunityWorkflowSlice 将 []db.CommunityWorkflow 转换为 []types.CommunityWorkflow。
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

// FromDomainCreateCommunityWorkflowParams 将 types.CreateCommunityWorkflowParams 转换为 db.CreateCommunityWorkflowParams。
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
// Workflow 领域
// ===========================================================================

// ToDomainWorkflowTemplate 将 db.WorkflowTemplate 转换为 types.WorkflowTemplate。
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

// ToDomainWorkflowTemplateSlice 将 []db.WorkflowTemplate 转换为 []types.WorkflowTemplate。
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

// ToDomainWorkflowTemplateNode 将 db.WorkflowTemplateNode 转换为 types.WorkflowTemplateNode。
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

// ToDomainWorkflowTemplateNodeSlice 将 []db.WorkflowTemplateNode 转换为 []types.WorkflowTemplateNode。
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

// FromDomainCreateWorkflowTemplateParams 将 types.CreateWorkflowTemplateParams 转换为 db.CreateWorkflowTemplateParams。
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

// FromDomainUpdateWorkflowTemplateParams 将 types.UpdateWorkflowTemplateParams 转换为 db.UpdateWorkflowTemplateParams。
//
// 注意：db 版本 ID 是 uuid.UUID，domain 版本 ID 是 string。
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

// FromDomainCreateTemplateNodeParams 将 types.CreateTemplateNodeParams 转换为 db.CreateTemplateNodeParams。
func FromDomainCreateTemplateNodeParams(p types.CreateTemplateNodeParams) (db.CreateTemplateNodeParams, error) {
	// TemplateID 允许为空：CreateWorkflowTemplate 会在事务内先建 template 再回填 nodes 的 TemplateID
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

// FromDomainCreateTemplateNodeParamsSlice 批量转换 []types.CreateTemplateNodeParams 为 []db.CreateTemplateNodeParams。
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
// Skill 领域
// ===========================================================================

// ToDomainSkill 将 db.Skill 转换为 types.Skill。
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

// ToDomainSkillSlice 将 []db.Skill 转换为 []types.Skill。
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

// FromDomainCreateSkillParams 将 types.CreateSkillParams 转换为 db.CreateSkillParams。
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

// FromDomainUpdateSkillParams 将 types.UpdateSkillParams 转换为 db.UpdateSkillParams。
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
// Runtime 领域
// ===========================================================================

// ToDomainRuntime 将 db.Runtime 转换为 types.Runtime。
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

// ToDomainRuntimeSlice 将 []db.Runtime 转换为 []types.Runtime。
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

// FromDomainCreateRuntimeParams 将 types.CreateRuntimeParams 转换为 db.CreateRuntimeParams。
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
// Member 领域（Auth 子域）
// ===========================================================================

// ToDomainMember 将 db.Member 转换为 types.Member。
//
// 注意：db.Member 含 PasswordHash 敏感字段，types.Member 故意不含（domain 层不暴露密码哈希）。
// 本函数丢弃 PasswordHash 字段。
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
// GitCredential 领域（Auth 子域）
// ===========================================================================

// ToDomainGitCredential 将 db.GitCredential 转换为 types.GitCredential。
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

// ToDomainGitCredentialSlice 将 []db.GitCredential 转换为 []types.GitCredential。
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
// Agent 领域转换器（AgentSkill、AgentMcpServer、McpServer、Rows）
// ===========================================================================

// FromDomainAddAgentSkillParams 将 types.AddAgentSkillParams 转换为 db.AddAgentSkillParams。
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

// FromDomainRemoveAgentSkillParams 将 types.RemoveAgentSkillParams 转换为 db.RemoveAgentSkillParams。
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

// FromDomainAddAgentMcpServerParams 将 types.AddAgentMcpServerParams 转换为 db.AddAgentMcpServerParams。
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

// FromDomainRemoveAgentMcpServerParams 将 types.RemoveAgentMcpServerParams 转换为 db.RemoveAgentMcpServerParams。
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

// ToDomainAgentSkill 将 db.AgentSkill 转换为 types.AgentSkill。
func ToDomainAgentSkill(as db.AgentSkill) (types.AgentSkill, error) {
	return types.AgentSkill{
		AgentID:   as.AgentID.String(),
		SkillID:   as.SkillID.String(),
		Enabled:   as.Enabled,
		CreatedAt: as.CreatedAt,
	}, nil
}

// ToDomainAgentMcpServer 将 db.AgentMcpServer 转换为 types.AgentMcpServer。
func ToDomainAgentMcpServer(ams db.AgentMcpServer) (types.AgentMcpServer, error) {
	return types.AgentMcpServer{
		AgentID:     ams.AgentID.String(),
		McpServerID: ams.McpServerID.String(),
		Enabled:     ams.Enabled,
		CreatedAt:   ams.CreatedAt,
	}, nil
}

// ToDomainListAgentSkillsRow 将 db.ListAgentSkillsRow 转换为 types.ListAgentSkillsRow。
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

// ToDomainListAgentSkillsRowSlice 将 []db.ListAgentSkillsRow 转换为 []types.ListAgentSkillsRow。
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

// ToDomainListAgentMcpServersRow 将 db.ListAgentMcpServersRow 转换为 types.ListAgentMcpServersRow。
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

// ToDomainListAgentMcpServersRowSlice 将 []db.ListAgentMcpServersRow 转换为 []types.ListAgentMcpServersRow。
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

// FromDomainUpdateMcpServerParams 将 types.UpdateMcpServerParams 转换为 db.UpdateMcpServerParams。
//
// 注意：types.UpdateMcpServerParams 不含 Status/Url 字段（更新仅改 name/url/type/auth/env），
// 这里仅映射存在的字段。如需更新 status 请用 UpdateMcpServerStatus。
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
		Status:   "active", // 默认状态，更新时不改 status
	}, nil
}

// ToDomainMcpServer 将 db.McpServer 转换为 types.McpServer。
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

// fromDomainCreateMcpServerParams 将 types.CreateMcpServerParams 转换为 db.CreateMcpServerParams。
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

// fromDomainUpdateMcpServerStatusParams 将 types.UpdateMcpServerStatusParams 转换为 db.UpdateMcpServerStatusParams。
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

// ToDomainGetInProgressNodesByAgentRow 将 db.GetInProgressNodesByAgentRow 转换为 types.GetInProgressNodesByAgentRow。
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


// ToDomainGetInProgressNodesByAgentRowSlice 将 []db.GetInProgressNodesByAgentRow 转换为 []types.GetInProgressNodesByAgentRow。
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

// ToDomainProjectMember 将 db.ProjectMember 转换为 types.ProjectMember。
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

// ToDomainProjectMemberSlice 将 []db.ProjectMember 转换为 []types.ProjectMember。
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

// FromDomainCreateProjectMemberParams 将 types.CreateProjectMemberParams 转换为 db.CreateProjectMemberParams。
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

// ToDomainProjectReviewer 将 db.ProjectReviewer 转换为 types.ProjectReviewer。
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

// ToDomainProjectReviewerSlice 将 []db.ProjectReviewer 转换为 []types.ProjectReviewer。
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

// FromDomainCreateProjectReviewerParams 将 types.CreateProjectReviewerParams 转换为 db.CreateProjectReviewerParams。
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

// FromDomainIsAgentProjectMemberParams 将 types.IsAgentProjectMemberParams 转换为 db.IsAgentProjectMemberParams。
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

// FromDomainListProjectsByAgentMembershipParams 将 types.ListProjectsByAgentMembershipParams 转换为 db.ListProjectsByAgentMembershipParams。
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
// Member 领域辅助（Workspace 子域）
// ===========================================================================

// ToDomainMemberSlice 将 []db.Member 转换为 []types.Member。
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

// FromDomainCreateMemberParams 将 types.CreateMemberParams 转换为 db.CreateMemberParams。
func FromDomainCreateMemberParams(p types.CreateMemberParams) (db.CreateMemberParams, error) {
	return db.CreateMemberParams{
		Name:  p.Name,
		Email: p.Email,
	}, nil
}

// ToDomainWorkspaceMember 将 db.WorkspaceMember 转换为 types.WorkspaceMember。
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

// ToDomainWorkspaceMemberSlice 将 []db.WorkspaceMember 转换为 []types.WorkspaceMember。
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

// FromDomainCreateWorkspaceMemberParams 将 types.CreateWorkspaceMemberParams 转换为 db.CreateWorkspaceMemberParams。
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

// FromDomainUpdateMemberRoleParams 将 types.UpdateMemberRoleParams 转换为 db.UpdateMemberRoleParams。
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

// FromDomainGetWorkspaceMemberParams 将 types.GetWorkspaceMemberParams 转换为 db.GetWorkspaceMemberParams。
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

// FromDomainGetWorkspaceMemberRoleParams 将 types.GetWorkspaceMemberRoleParams 转换为 db.GetWorkspaceMemberRoleParams。
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

// ToDomainListMembersByWorkspaceRow 将 db.ListMembersByWorkspaceRow 转换为 types.ListMembersByWorkspaceRow。
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

// ToDomainListMembersByWorkspaceRowSlice 将 []db.ListMembersByWorkspaceRow 转换为 []types.ListMembersByWorkspaceRow。
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
// 便捷转换函数（用于 Service 层直接传参）
// ===========================================================================

// ToDBGetWorkspaceMemberParams 从 uuid.UUID 构建 db.GetWorkspaceMemberParams。
func ToDBGetWorkspaceMemberParams(workspaceID, memberID uuid.UUID) db.GetWorkspaceMemberParams {
	return db.GetWorkspaceMemberParams{
		WorkspaceID: workspaceID,
		MemberID:    memberID,
	}
}

// ToDBGetWorkspaceMemberRoleParams 从 uuid.UUID 构建 db.GetWorkspaceMemberRoleParams。
func ToDBGetWorkspaceMemberRoleParams(workspaceID, memberID uuid.UUID) db.GetWorkspaceMemberRoleParams {
	return db.GetWorkspaceMemberRoleParams{
		WorkspaceID: workspaceID,
		MemberID:    memberID,
	}
}

// FromDomainCreateMemberParamsSimple 从简单参数构建 db.CreateMemberParams。
func FromDomainCreateMemberParamsSimple(name, email string) db.CreateMemberParams {
	return db.CreateMemberParams{
		Name:  name,
		Email: email,
	}
}

// FromDomainCreateWorkspaceParamsSimple 从简单参数构建 db.CreateWorkspaceParams。
func FromDomainCreateWorkspaceParamsSimple(name, description, issuePrefix string) db.CreateWorkspaceParams {
	return db.CreateWorkspaceParams{
		Name:        name,
		Description: sql.NullString{String: description, Valid: true},
		IssuePrefix: issuePrefix,
	}
}

// ToDomainExecutionSession 将 db.ExecutionSession 转换为 types.ExecutionSession。
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

// FromDomainCreateExecutionSessionParams 将 types.CreateExecutionSessionParams 转换为 db.CreateExecutionSessionParams。
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

// FromDomainGetActiveSessionByAgentAndWorkdirParams 将 types.GetActiveSessionByAgentAndWorkdirParams 转换为 db.GetActiveSessionByAgentAndWorkdirParams。
func FromDomainGetActiveSessionByAgentAndWorkdirParams(p types.GetActiveSessionByAgentAndWorkdirParams) (db.GetActiveSessionByAgentAndWorkdirParams, error) {
	return db.GetActiveSessionByAgentAndWorkdirParams{
		AgentID: stringToNullUUID(p.AgentID),
		Workdir: ptrToNullString(p.Workdir),
	}, nil
}

// FromDomainUpdateSessionClaudeIDParams 将 types.UpdateSessionClaudeIDParams 转换为 db.UpdateSessionClaudeIDParams。
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

// ToDomainGetTemplateStatsRow 将 db.GetTemplateStatsRow 转换为 types.GetTemplateStatsRow。
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

// ToDomainWorkflowTriggerRun 将 db.WorkflowTriggerRun 转换为 types.WorkflowTriggerRun。
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

// FromDomainCreateWorkflowTriggerRunParams 将 domain 参数转换为 db 参数。
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

// FromDomainMarkWorkflowTriggerRunCompletedParams 将 domain 参数转换为 db 参数。
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

// FromDomainMarkWorkflowTriggerRunFailedParams 将 domain 参数转换为 db 参数。
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

// FromDomainListDueScheduledWorkflowTemplatesParams 将 domain 参数转换为 db 参数。
func FromDomainListDueScheduledWorkflowTemplatesParams(p types.ListDueScheduledWorkflowTemplatesParams) (db.ListDueScheduledWorkflowTemplatesParams, error) {
	return db.ListDueScheduledWorkflowTemplatesParams{
		NextRunAt: ptrToNullTime(p.NextRunAt),
		Limit:     p.Limit,
	}, nil
}

// FromDomainUpdateWorkflowTemplateTriggerScheduleParams 将 domain 参数转换为 db 参数。
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
