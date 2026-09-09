// project_test.go covers project data access tests.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestProjectCRUD tests project CRUD operations:
// 1. Create a project, then query via GetProject and verify the name matches
// 2. List all projects under the workspace via ListProjects
// 3. Update the project name via UpdateProject and verify
func TestProjectCRUD(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)

	fetched, err := s.GetProject(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if fetched.Name != proj.Name {
		t.Fatalf("expected name %s, got %s", proj.Name, fetched.Name)
	}

	projs, err := s.ListProjects(ctx, uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projs) == 0 {
		t.Fatal("expected at least 1 project")
	}

	updated, err := s.UpdateProject(ctx, types.UpdateProjectParams{
		ID:          proj.ID,
		Name:        "updated-proj",
		Description: &proj.Description,
		Status:      proj.Status,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Name != "updated-proj" {
		t.Fatalf("expected updated name, got %s", updated.Name)
	}
}

// TestProjectMemberCRUD tests project member CRUD operations:
// 1. Create a project member (agent type) and verify the member is associated with the correct project
// 2. List project members via ListProjectMembers and verify the count
// 3. Verify whether an agent is a project member via IsAgentProjectMember
// 4. Delete the member via DeleteProjectMember and verify
func TestProjectMemberCRUD(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)

	pm, err := s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    &agent.ID,
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("CreateProjectMember: %v", err)
	}
	if pm.ProjectID != proj.ID {
		t.Fatalf("expected project ID match")
	}

	members, err := s.ListProjectMembers(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}

	isMember, err := s.IsAgentProjectMember(ctx, types.IsAgentProjectMemberParams{ProjectID: proj.ID, AgentID: agent.ID})
	if err != nil {
		t.Fatalf("IsAgentProjectMember: %v", err)
	}
	if !isMember {
		t.Fatal("expected agent to be project member")
	}

	err = s.DeleteProjectMember(ctx, uuid.MustParse(pm.ID))
	if err != nil {
		t.Fatalf("DeleteProjectMember: %v", err)
	}
}

// TestProjectReviewerCRUD tests project reviewer CRUD operations:
// 1. Create a project reviewer (member type) and verify creation succeeds
// 2. List project reviewers via ListProjectReviewers and verify the count
// 3. Delete the reviewer via DeleteProjectReviewer and verify
func TestProjectReviewerCRUD(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	member := createTestMember(t, s, ws.ID)

	_, err := s.CreateProjectReviewer(ctx, types.CreateProjectReviewerParams{
		ProjectID:  proj.ID,
		MemberType: "member",
		MemberID:   &member.ID,
	})
	if err != nil {
		t.Fatalf("CreateProjectReviewer: %v", err)
	}

	reviewers, err := s.ListProjectReviewers(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("ListProjectReviewers: %v", err)
	}
	if len(reviewers) != 1 {
		t.Fatalf("expected 1 reviewer, got %d", len(reviewers))
	}

	err = s.DeleteProjectReviewer(ctx, uuid.MustParse(reviewers[0].ID))
	if err != nil {
		t.Fatalf("DeleteProjectReviewer: %v", err)
	}
}
