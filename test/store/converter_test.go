// converter_test.go 覆盖 domain/db 类型转换函数的测试。
package store_test

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// fixedTime 在所有 round-trip 测试中用作可预测的时间戳。
var fixedTime = time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

// fixedUUID 在所有 round-trip 测试中用作可预测的 UUID。
var fixedUUID = uuid.MustParse("12345678-1234-5678-1234-567812345678")

// newUUIDString 返回一个 domain 风格的 UUID 字符串。
func newUUIDString() string { return fixedUUID.String() }

// TestToDomainTask_RoundTrip 验证 db.Task → types.Task → db.CreateTaskParams 的字段保真度。
func TestToDomainTask_RoundTrip(t *testing.T) {
	dbTask := db.Task{
		ID:           42,
		ProjectID:    fixedUUID,
		WorkflowName: "default",
		Title:        "Build feature X",
		Description:  sql.NullString{String: "Detailed task description", Valid: true},
		Constraints:  sql.NullString{String: "No external deps", Valid: true},
		Type:         db.TaskTypeStory,
		Priority:     db.TaskPriorityHigh,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     fixedUUID,
		DueDate:      sql.NullTime{Time: fixedTime, Valid: true},
		Labels:       []string{"backend", "urgent"},
		Sequence:     7,
		ParentTaskID: sql.NullInt32{Int32: 5, Valid: true},
		GitBranch:    sql.NullString{String: "feature/x", Valid: true},
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}

	domainTask, err := store.ToDomainTask(dbTask)
	if err != nil {
		t.Fatalf("ToDomainTask: %v", err)
	}

	// 逐字段验证
	checks := []struct{ name, got, want string }{
		{"ID", numToStr(domainTask.ID), "42"},
		{"ProjectID", domainTask.ProjectID, newUUIDString()},
		{"WorkflowName", domainTask.WorkflowName, "default"},
		{"Title", domainTask.Title, "Build feature X"},
		{"Type", domainTask.Type, types.TaskTypeStory},
		{"Priority", domainTask.Priority, types.TaskPriorityHigh},
		{"Status", domainTask.Status, types.TaskStatusActive},
		{"AuthorType", domainTask.AuthorType, "agent"},
		{"AuthorID", domainTask.AuthorID, newUUIDString()},
		{"Sequence", numToStr(domainTask.Sequence), "7"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}

	if domainTask.Description != dbTask.Description.String {
		t.Errorf("Description = %q, want %q", domainTask.Description, dbTask.Description.String)
	}
	if domainTask.Constraints != dbTask.Constraints.String {
		t.Errorf("Constraints = %q, want %q", domainTask.Constraints, dbTask.Constraints.String)
	}
	if domainTask.DueDate == nil || !domainTask.DueDate.Equal(fixedTime) {
		t.Errorf("DueDate = %v, want %v", domainTask.DueDate, fixedTime)
	}
	if len(domainTask.Labels) != 2 ||
		domainTask.Labels[0] != "backend" ||
		domainTask.Labels[1] != "urgent" {
		t.Errorf("Labels = %v, want [backend urgent]", domainTask.Labels)
	}

	// 反向校验：toDomainTask(dbTask) 的字段能被 fromDomainXxxParams 还原
	createParams := types.CreateTaskParams{
		ProjectID:    domainTask.ProjectID,
		Title:        domainTask.Title,
		Description:  &dbTask.Description.String,
		Constraints:  &dbTask.Constraints.String,
		Type:         domainTask.Type,
		Priority:     domainTask.Priority,
		Status:       domainTask.Status,
		AuthorType:   domainTask.AuthorType,
		AuthorID:     domainTask.AuthorID,
		DueDate:      domainTask.DueDate,
		Labels:       domainTask.Labels,
		Sequence:     int32(domainTask.Sequence),
		WorkflowName: domainTask.WorkflowName,
	}
	dbCreateParams, err := store.FromDomainCreateTaskParams(createParams)
	if err != nil {
		t.Fatalf("FromDomainCreateTaskParams: %v", err)
	}
	if dbCreateParams.ProjectID != dbTask.ProjectID {
		t.Errorf("ProjectID round-trip failed: %v vs %v",
			dbCreateParams.ProjectID, dbTask.ProjectID)
	}
	if dbCreateParams.AuthorID != dbTask.AuthorID {
		t.Errorf("AuthorID round-trip failed: %v vs %v",
			dbCreateParams.AuthorID, dbTask.AuthorID)
	}
	if dbCreateParams.Title != dbTask.Title {
		t.Errorf("Title round-trip failed: %q vs %q",
			dbCreateParams.Title, dbTask.Title)
	}
}

// TestToDomainTaskNode_RoundTrip 验证 db.TaskNode → types.TaskNode 的字段保真度。
func TestToDomainTaskNode_RoundTrip(t *testing.T) {
	completedAt := fixedTime
	completedBy := uuid.NullUUID{UUID: fixedUUID, Valid: true}
	assigneeID := uuid.NullUUID{UUID: fixedUUID, Valid: true}
	reserved := uuid.NullUUID{UUID: fixedUUID, Valid: true}

	dbNode := db.TaskNode{
		ID:                   fixedUUID,
		TaskID:               42,
		Name:                 "Implement API",
		Description:          sql.NullString{String: "Build REST endpoint", Valid: true},
		SortOrder:            3,
		NodeType:             db.NodeTypeStandard,
		Status:               db.TaskNodeStatusInProgress,
		AssigneeType:         db.AssigneeTypeSpecificAgent,
		AssigneeID:           assigneeID,
		ReservedForAgentID:   reserved,
		RejectCount:          2,
		MaxRejectCycles:      5,
		TimeoutMinutes:       30,
		Version:              4,
		CompletedAt:          sql.NullTime{Time: completedAt, Valid: true},
		CompletedBy:          completedBy,
		Summary:              "Done implementing",
		PreviousSummary:      "Was in progress",
		ReservationExpiresAt: sql.NullTime{Time: completedAt.Add(5 * time.Minute), Valid: true},
		ReadonlyDirs:         pqtype.NullRawMessage{RawMessage: json.RawMessage(`["/docs","/README.md"]`), Valid: true},
		FullControlDirs:      pqtype.NullRawMessage{RawMessage: json.RawMessage(`["/src"]`), Valid: true},
		CreatedAt:            fixedTime,
		UpdatedAt:            fixedTime,
	}

	domainNode, err := store.ToDomainTaskNode(dbNode)
	if err != nil {
		t.Fatalf("ToDomainTaskNode: %v", err)
	}

	if domainNode.ID != newUUIDString() {
		t.Errorf("ID = %q, want %q", domainNode.ID, newUUIDString())
	}
	if domainNode.TaskID != 42 {
		t.Errorf("TaskID = %d, want 42", domainNode.TaskID)
	}
	if domainNode.Name != "Implement API" {
		t.Errorf("Name = %q", domainNode.Name)
	}
	if domainNode.Description != dbNode.Description.String {
		t.Errorf("Description = %q, want %q",
			domainNode.Description, dbNode.Description.String)
	}
	if domainNode.SortOrder != 3 {
		t.Errorf("SortOrder = %d, want 3", domainNode.SortOrder)
	}
	if domainNode.Status != types.TaskNodeStatusInProgress {
		t.Errorf("Status = %q", domainNode.Status)
	}
	if domainNode.Version != 4 {
		t.Errorf("Version = %d, want 4", domainNode.Version)
	}
	if domainNode.CompletedAt == nil || !domainNode.CompletedAt.Equal(completedAt) {
		t.Errorf("CompletedAt = %v, want %v", domainNode.CompletedAt, completedAt)
	}
	if string(domainNode.ReadonlyDirs) != `["/docs","/README.md"]` {
		t.Errorf("ReadonlyDirs = %s, want [\"/docs\",\"/README.md\"]", domainNode.ReadonlyDirs)
	}
	if string(domainNode.FullControlDirs) != `["/src"]` {
		t.Errorf("FullControlDirs = %s, want [\"/src\"]", domainNode.FullControlDirs)
	}
}

// TestToDomainComment_RoundTrip 验证 db.Comment → types.Comment 的字段保真度。
func TestToDomainComment_RoundTrip(t *testing.T) {
	nodeID := uuid.NullUUID{UUID: fixedUUID, Valid: true}
	parentID := uuid.NullUUID{UUID: fixedUUID, Valid: true}
	authorID := fixedUUID
	mentions := []uuid.UUID{fixedUUID}
	metadata := json.RawMessage(`{"key":"value"}`)

	dbComment := db.Comment{
		ID:           fixedUUID,
		TaskID:       42,
		NodeID:       nodeID,
		SourceNodeID: nodeID,
		ParentID:     parentID,
		AuthorType:   "agent",
		AuthorID:     authorID,
		Content:      "Need review for this node",
		CommentType:  "node_comment",
		Metadata:     pqtype.NullRawMessage{RawMessage: metadata, Valid: true},
		Mentions:     mentions,
		EditedAt:     sql.NullTime{Time: fixedTime, Valid: true},
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}

	domainComment, err := store.ToDomainComment(dbComment)
	if err != nil {
		t.Fatalf("ToDomainComment: %v", err)
	}

	if domainComment.ID != newUUIDString() {
		t.Errorf("ID = %q, want %q", domainComment.ID, newUUIDString())
	}
	if domainComment.TaskID != 42 {
		t.Errorf("TaskID = %d", domainComment.TaskID)
	}
	if domainComment.Content != "Need review for this node" {
		t.Errorf("Content = %q", domainComment.Content)
	}
	if domainComment.CommentType != "node_comment" {
		t.Errorf("CommentType = %q", domainComment.CommentType)
	}
	if string(domainComment.Metadata) != `{"key":"value"}` {
		t.Errorf("Metadata = %s", string(domainComment.Metadata))
	}
	if len(domainComment.Mentions) != 1 || domainComment.Mentions[0] != newUUIDString() {
		t.Errorf("Mentions = %v", domainComment.Mentions)
	}
	if domainComment.EditedAt == nil || !domainComment.EditedAt.Equal(fixedTime) {
		t.Errorf("EditedAt = %v", domainComment.EditedAt)
	}
}

// TestToDomainAgent_RoundTrip 验证 db.Agent → types.Agent 的字段保真度。
func TestToDomainAgent_RoundTrip(t *testing.T) {
	wsID := fixedUUID
	model := "claude-sonnet-4"
	customEnv := json.RawMessage(`{"API_KEY":"xxx"}`)

	dbAgent := db.Agent{
		ID:           fixedUUID,
		WorkspaceID:  wsID,
		Name:         "Builder Agent",
		Provider:     db.AgentProviderClaude,
		Instructions: "You are a builder agent.",
		Model:        sql.NullString{String: model, Valid: true},
		Status:       db.AgentStatusOnline,
		CustomEnv:    pqtype.NullRawMessage{RawMessage: customEnv, Valid: true},
		ExtraArgs:    []string{"--verbose", "--json"},
		GitName:      sql.NullString{String: "builder-bot", Valid: true},
		GitEmail:     sql.NullString{String: "builder@example.com", Valid: true},
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}

	domainAgent, err := store.ToDomainAgent(dbAgent)
	if err != nil {
		t.Fatalf("ToDomainAgent: %v", err)
	}

	if domainAgent.ID != newUUIDString() {
		t.Errorf("ID = %q", domainAgent.ID)
	}
	if domainAgent.WorkspaceID != newUUIDString() {
		t.Errorf("WorkspaceID = %q", domainAgent.WorkspaceID)
	}
	if domainAgent.Provider != types.AgentProviderClaude {
		t.Errorf("Provider = %q", domainAgent.Provider)
	}
	if domainAgent.Model != model {
		t.Errorf("Model = %q, want %q", domainAgent.Model, model)
	}
	if domainAgent.Status != types.AgentStatusOnline {
		t.Errorf("Status = %q", domainAgent.Status)
	}
	if string(domainAgent.CustomEnv) != `{"API_KEY":"xxx"}` {
		t.Errorf("CustomEnv = %s", string(domainAgent.CustomEnv))
	}
	if len(domainAgent.ExtraArgs) != 2 || domainAgent.ExtraArgs[0] != "--verbose" {
		t.Errorf("ExtraArgs = %v", domainAgent.ExtraArgs)
	}
	if domainAgent.GitName != "builder-bot" {
		t.Errorf("GitName = %q", domainAgent.GitName)
	}
	if domainAgent.GitEmail != "builder@example.com" {
		t.Errorf("GitEmail = %q", domainAgent.GitEmail)
	}
}

// TestToDomainWorkspace_RoundTrip 验证 db.Workspace → types.Workspace 的字段保真度。
func TestToDomainWorkspace_RoundTrip(t *testing.T) {
	desc := "Primary workspace for team X"
	dbWS := db.Workspace{
		ID:          fixedUUID,
		Name:        "Team X",
		Description: sql.NullString{String: desc, Valid: true},
		IssuePrefix: "TM",
		IsDefault:   true,
		CreatedAt:   fixedTime,
		UpdatedAt:   fixedTime,
	}

	domainWS, err := store.ToDomainWorkspace(dbWS)
	if err != nil {
		t.Fatalf("ToDomainWorkspace: %v", err)
	}

	if domainWS.ID != newUUIDString() {
		t.Errorf("ID = %q", domainWS.ID)
	}
	if domainWS.Name != "Team X" {
		t.Errorf("Name = %q", domainWS.Name)
	}
	if domainWS.Description != desc {
		t.Errorf("Description = %q, want %q", domainWS.Description, desc)
	}
	if domainWS.IssuePrefix != "TM" {
		t.Errorf("IssuePrefix = %q", domainWS.IssuePrefix)
	}
	if !domainWS.IsDefault {
		t.Errorf("IsDefault = false, want true")
	}
}

// TestToDomainProject_RoundTrip 验证 db.Project → types.Project 的字段保真度。
func TestToDomainProject_RoundTrip(t *testing.T) {
	wsID := fixedUUID
	desc := "Project description"
	icon := "🚀"
	repoURL := "https://github.com/org/repo.git"
	context := "Project context"
	defaultWorkflowID := fixedUUID

	dbProject := db.Project{
		ID:                fixedUUID,
		WorkspaceID:       wsID,
		Name:              "Project X",
		Description:       sql.NullString{String: desc, Valid: true},
		Icon:              sql.NullString{String: icon, Valid: true},
		Status:            db.ProjectStatusActive,
		RepoUrl:           sql.NullString{String: repoURL, Valid: true},
		Context:           sql.NullString{String: context, Valid: true},
		DefaultWorkflowID: uuid.NullUUID{UUID: defaultWorkflowID, Valid: true},
		MaxReviewCycles:   3,
		CreatedAt:         fixedTime,
		UpdatedAt:         fixedTime,
	}

	domainProject, err := store.ToDomainProject(dbProject)
	if err != nil {
		t.Fatalf("ToDomainProject: %v", err)
	}

	if domainProject.ID != newUUIDString() {
		t.Errorf("ID = %q", domainProject.ID)
	}
	if domainProject.WorkspaceID != newUUIDString() {
		t.Errorf("WorkspaceID = %q", domainProject.WorkspaceID)
	}
	if domainProject.Name != "Project X" {
		t.Errorf("Name = %q", domainProject.Name)
	}
	if domainProject.Description != desc {
		t.Errorf("Description = %q, want %q", domainProject.Description, desc)
	}
	if domainProject.Icon != icon {
		t.Errorf("Icon = %q, want %q", domainProject.Icon, icon)
	}
	if domainProject.Status != types.ProjectStatusActive {
		t.Errorf("Status = %q", domainProject.Status)
	}
	if domainProject.RepoURL != repoURL {
		t.Errorf("RepoURL = %q, want %q", domainProject.RepoURL, repoURL)
	}
	if domainProject.Context != context {
		t.Errorf("Context = %q, want %q", domainProject.Context, context)
	}
	if domainProject.DefaultWorkflowID == nil ||
		*domainProject.DefaultWorkflowID != newUUIDString() {
		t.Errorf("DefaultWorkflowID = %v, want %q",
			domainProject.DefaultWorkflowID, newUUIDString())
	}
}

// TestFromDomainCreateTaskParams_InvalidUUID 验证无效 UUID 时返回错误。
func TestFromDomainCreateTaskParams_InvalidUUID(t *testing.T) {
	params := types.CreateTaskParams{
		ProjectID: "not-a-uuid",
		Title:     "Test",
	}
	_, err := store.FromDomainCreateTaskParams(params)
	if err == nil {
		t.Error("Expected error for invalid UUID, got nil")
	}
}

// TestToDomainTask_NullFields 验证 db.Task 的 NULL 字段正确映射到零值。
func TestToDomainTask_NullFields(t *testing.T) {
	dbTask := db.Task{
		ID:           1,
		ProjectID:    fixedUUID,
		Title:        "Test task",
		Description:  sql.NullString{}, // NULL
		Constraints:  sql.NullString{}, // NULL
		AuthorID:     fixedUUID,
		DueDate:      sql.NullTime{}, // NULL
		ParentTaskID: sql.NullInt32{}, // NULL
		GitBranch:    sql.NullString{}, // NULL
	}

	domainTask, err := store.ToDomainTask(dbTask)
	if err != nil {
		t.Fatalf("ToDomainTask: %v", err)
	}

	if domainTask.Description != "" {
		t.Errorf("NULL Description should map to empty string, got %q",
			domainTask.Description)
	}
	if domainTask.DueDate != nil {
		t.Errorf("NULL DueDate should map to nil, got %v", domainTask.DueDate)
	}
	if domainTask.ParentTaskID != nil {
		t.Errorf("NULL ParentTaskID should map to nil, got %v",
			domainTask.ParentTaskID)
	}
	if domainTask.GitBranch != nil {
		t.Errorf("NULL GitBranch should map to nil, got %v", domainTask.GitBranch)
	}
}

// numToStr 是一个简单的整数 → string 转换辅助，避免引入 strconv。
func numToStr(n interface{}) string {
	switch v := n.(type) {
	case int:
		if v == 0 {
			return "0"
		}
		var buf [20]byte
		pos := len(buf)
		neg := v < 0
		if neg {
			v = -v
		}
		for v > 0 {
			pos--
			buf[pos] = byte('0' + v%10)
			v /= 10
		}
		if neg {
			pos--
			buf[pos] = '-'
		}
		return string(buf[pos:])
	case int32:
		return numToStr(int(v))
	case int64:
		return numToStr(int(v))
	default:
		return ""
	}
}
