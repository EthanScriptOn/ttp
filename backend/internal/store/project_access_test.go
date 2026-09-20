package store

import (
	"context"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
)

func TestMemoryProjectAccessFiltersProjectsAndChecksRolePermissions(t *testing.T) {
	ctx := context.Background()
	data := NewMemoryWithFixtures()
	member, err := data.CreateSpaceMember(ctx, "space-lab", CreateSpaceMemberInput{
		Username: "project-viewer",
		Password: "viewer-pass-123",
		Role:     "viewer",
	})
	if err != nil {
		t.Fatalf("create space member: %v", err)
	}
	project, err := data.CreateProject(ctx, "space-lab", CreateProjectInput{
		Name:          "Second project",
		RepositoryURL: "https://github.com/acme/second-project",
		ClusterID:     "demo-cluster",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projects, err := data.ListProjectsForUser(ctx, member.UserID, "space-lab")
	if err != nil {
		t.Fatalf("list projects before membership: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected no visible projects before membership, got %d", len(projects))
	}
	if _, err := data.CreateProjectMember(ctx, "space-lab", project.ID, CreateProjectMemberInput{UserID: member.UserID, RoleKey: "project_developer"}); err != nil {
		t.Fatalf("add project member: %v", err)
	}
	projects, err = data.ListProjectsForUser(ctx, member.UserID, "space-lab")
	if err != nil {
		t.Fatalf("list projects after membership: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("expected second project to be visible, got %#v", projects)
	}
	allowed, err := data.HasProjectPermission(ctx, member.UserID, "space-lab", project.ID, permissions.ProjectDeploymentManage)
	if err != nil || !allowed {
		t.Fatalf("expected developer deployment permission, allowed=%v err=%v", allowed, err)
	}
	allowed, err = data.HasProjectPermission(ctx, member.UserID, "space-lab", project.ID, permissions.ProjectReleasePublish)
	if err != nil {
		t.Fatalf("check publish permission: %v", err)
	}
	if allowed {
		t.Fatal("developer should not receive publish permission by default")
	}
}

func TestMemoryCustomProjectRoleBindsPermissions(t *testing.T) {
	ctx := context.Background()
	data := NewMemoryWithFixtures()
	role, err := data.CreateProjectRole(ctx, "space-lab", 1, CreateProjectRoleInput{
		Name:        "只读发布员",
		Description: "可以查看和执行发布",
		Permissions: []string{permissions.ProjectView, permissions.ProjectReleaseRead, permissions.ProjectReleasePublish},
	})
	if err != nil {
		t.Fatalf("create custom project role: %v", err)
	}
	member, err := data.CreateSpaceMember(ctx, "space-lab", CreateSpaceMemberInput{
		Username: "release-member",
		Password: "release-pass-123",
		Role:     "viewer",
	})
	if err != nil {
		t.Fatalf("create space member: %v", err)
	}
	if _, err := data.CreateProjectMember(ctx, "space-lab", "reverse-lab", CreateProjectMemberInput{UserID: member.UserID, RoleID: role.ID}); err != nil {
		t.Fatalf("bind custom role: %v", err)
	}
	access, err := data.GetProjectAccess(ctx, member.UserID, "space-lab", "reverse-lab")
	if err != nil {
		t.Fatalf("get project access: %v", err)
	}
	if access.RoleID != role.ID || access.RoleName != role.Name {
		t.Fatalf("unexpected project access: %#v", access)
	}
	if len(access.Permissions) != 3 {
		t.Fatalf("expected three permissions, got %#v", access.Permissions)
	}
}
