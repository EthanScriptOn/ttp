package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
)

func projectMemberKey(projectID string, userID uint64) string {
	return strings.TrimSpace(projectID) + "\x00" + strconv.FormatUint(userID, 10)
}

func (m *Memory) projectRoleLocked(spaceID, roleID, roleKey string) (domain.ProjectRole, error) {
	roleID, roleKey, err := normalizeProjectRoleReference(roleID, roleKey)
	if err != nil {
		return domain.ProjectRole{}, err
	}
	if roleID != "" {
		if strings.HasPrefix(roleID, projectSystemRoleID) {
			roleKey = strings.TrimPrefix(roleID, projectSystemRoleID)
		} else if role, ok := m.projectRoles[roleID]; ok && role.SpaceID == spaceID {
			role.Permissions = append([]string(nil), role.Permissions...)
			return role, nil
		} else {
			return domain.ProjectRole{}, ErrNotFound
		}
	}
	if role, ok := projectRoleByKey(spaceID, roleKey); ok {
		return role, nil
	}
	for _, role := range m.projectRoles {
		if role.SpaceID == spaceID && role.Key == roleKey {
			role.Permissions = append([]string(nil), role.Permissions...)
			return role, nil
		}
	}
	return domain.ProjectRole{}, ErrNotFound
}

func (m *Memory) projectAccessLocked(userID uint64, spaceID, projectID string) (domain.ProjectAccess, error) {
	user, ok := m.users[userID]
	if !ok {
		return domain.ProjectAccess{}, ErrNotFound
	}
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.ProjectAccess{}, ErrNotFound
	}
	if user.IsSuperAdmin {
		return domain.ProjectAccess{
			ProjectID: projectID, SpaceID: spaceID, RoleID: projectSystemRoleID + "project_maintainer",
			RoleKey: "project_maintainer", RoleName: "项目维护者", Permissions: allProjectPermissions(),
			IsSpaceAdmin: true, IsSuperAdmin: true,
		}, nil
	}
	spaceMember, ok := m.memberships[memberKey(userID, spaceID)]
	if !ok {
		return domain.ProjectAccess{}, ErrForbidden
	}
	if role := permissions.NormalizeRole(spaceMember.Role); role == "owner" || role == "admin" {
		definition, _ := projectRoleByKey(spaceID, "project_maintainer")
		return domain.ProjectAccess{
			ProjectID: projectID, SpaceID: spaceID, RoleID: projectSystemRoleID + definition.Key,
			RoleKey: definition.Key, RoleName: definition.Name, Permissions: allProjectPermissions(),
			IsSpaceAdmin: true,
		}, nil
	}
	member, ok := m.projectMembers[projectMemberKey(projectID, userID)]
	if !ok || member.SpaceID != spaceID {
		return domain.ProjectAccess{}, ErrForbidden
	}
	role, err := m.projectRoleLocked(spaceID, member.RoleID, member.RoleKey)
	if err != nil {
		return domain.ProjectAccess{}, err
	}
	return domain.ProjectAccess{
		ProjectID: projectID, SpaceID: spaceID, RoleID: role.ID, RoleKey: role.Key,
		RoleName: role.Name, Permissions: append([]string(nil), role.Permissions...),
	}, nil
}

func (m *Memory) ListProjectsForUser(ctx context.Context, userID uint64, spaceID string) ([]domain.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return nil, ErrNotFound
	}
	if _, ok := m.users[userID]; !ok {
		return nil, ErrNotFound
	}
	result := make([]domain.Project, 0)
	for _, project := range m.projects {
		if project.SpaceID != spaceID {
			continue
		}
		if _, err := m.projectAccessLocked(userID, spaceID, project.ID); err == nil {
			result = append(result, project)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (m *Memory) GetProjectAccess(ctx context.Context, userID uint64, spaceID, projectID string) (domain.ProjectAccess, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProjectAccess{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.projectAccessLocked(userID, strings.TrimSpace(spaceID), strings.TrimSpace(projectID))
}

func (m *Memory) HasProjectPermission(ctx context.Context, userID uint64, spaceID, projectID, permission string) (bool, error) {
	access, err := m.GetProjectAccess(ctx, userID, spaceID, projectID)
	if err != nil {
		return false, err
	}
	for _, candidate := range access.Permissions {
		if candidate == permission || candidate == "*" {
			return true, nil
		}
	}
	return false, nil
}

func (m *Memory) EnsureProjectMember(ctx context.Context, spaceID, projectID string, userID uint64, roleKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	roleKey = strings.ToLower(strings.TrimSpace(roleKey))
	role, ok := projectRoleByKey(spaceID, roleKey)
	if !ok {
		return ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return ErrNotFound
	}
	if _, ok := m.users[userID]; !ok {
		return ErrNotFound
	}
	if !m.users[userID].IsSuperAdmin {
		if _, ok := m.memberships[memberKey(userID, spaceID)]; !ok {
			return ErrForbidden
		}
	}
	key := projectMemberKey(projectID, userID)
	if _, exists := m.projectMembers[key]; exists {
		return nil
	}
	now := time.Now().UTC()
	m.projectMembers[key] = projectMembership{ProjectID: projectID, SpaceID: spaceID, UserID: userID, RoleID: role.ID, RoleKey: role.Key, JoinedAt: now, UpdatedAt: now}
	return nil
}

func (m *Memory) ListProjectMembers(ctx context.Context, spaceID, projectID string) ([]domain.ProjectMember, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return nil, ErrNotFound
	}
	result := make([]domain.ProjectMember, 0)
	for _, member := range m.projectMembers {
		if member.ProjectID != projectID || member.SpaceID != spaceID {
			continue
		}
		user, ok := m.users[member.UserID]
		if !ok {
			continue
		}
		role, err := m.projectRoleLocked(spaceID, member.RoleID, member.RoleKey)
		if err != nil {
			continue
		}
		result = append(result, domain.ProjectMember{
			ProjectID: projectID, SpaceID: spaceID, UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName,
			RoleID: role.ID, RoleKey: role.Key, RoleName: role.Name, Permissions: append([]string(nil), role.Permissions...),
			IsSuperAdmin: user.IsSuperAdmin, JoinedAt: member.JoinedAt, UpdatedAt: member.UpdatedAt,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].RoleName == result[j].RoleName {
			return strings.ToLower(result[i].Username) < strings.ToLower(result[j].Username)
		}
		return result[i].RoleName < result[j].RoleName
	})
	return result, nil
}

func (m *Memory) CreateProjectMember(ctx context.Context, spaceID, projectID string, input CreateProjectMemberInput) (domain.ProjectMember, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProjectMember{}, err
	}
	roleID, roleKey, err := normalizeProjectRoleReference(input.RoleID, input.RoleKey)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.ProjectMember{}, ErrNotFound
	}
	user, ok := m.users[input.UserID]
	if !ok {
		return domain.ProjectMember{}, ErrNotFound
	}
	if !user.IsSuperAdmin {
		if _, ok := m.memberships[memberKey(input.UserID, spaceID)]; !ok {
			return domain.ProjectMember{}, ErrForbidden
		}
	}
	role, err := m.projectRoleLocked(spaceID, roleID, roleKey)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	key := projectMemberKey(projectID, input.UserID)
	if _, exists := m.projectMembers[key]; exists {
		return domain.ProjectMember{}, ErrConflict
	}
	now := time.Now().UTC()
	m.projectMembers[key] = projectMembership{ProjectID: projectID, SpaceID: spaceID, UserID: input.UserID, RoleID: role.ID, RoleKey: role.Key, JoinedAt: now, UpdatedAt: now}
	return domain.ProjectMember{
		ProjectID: projectID, SpaceID: spaceID, UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName,
		RoleID: role.ID, RoleKey: role.Key, RoleName: role.Name, Permissions: append([]string(nil), role.Permissions...),
		IsSuperAdmin: user.IsSuperAdmin, JoinedAt: now, UpdatedAt: now,
	}, nil
}

func (m *Memory) UpdateProjectMember(ctx context.Context, spaceID, projectID string, userID uint64, input UpdateProjectMemberInput) (domain.ProjectMember, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProjectMember{}, err
	}
	roleID, roleKey, err := normalizeProjectRoleReference(input.RoleID, input.RoleKey)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	memberKeyValue := projectMemberKey(projectID, userID)
	member, ok := m.projectMembers[memberKeyValue]
	if !ok || member.SpaceID != spaceID {
		return domain.ProjectMember{}, ErrNotFound
	}
	user, ok := m.users[userID]
	if !ok {
		return domain.ProjectMember{}, ErrNotFound
	}
	role, err := m.projectRoleLocked(spaceID, roleID, roleKey)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	member.RoleID, member.RoleKey, member.UpdatedAt = role.ID, role.Key, time.Now().UTC()
	m.projectMembers[memberKeyValue] = member
	return domain.ProjectMember{
		ProjectID: projectID, SpaceID: spaceID, UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName,
		RoleID: role.ID, RoleKey: role.Key, RoleName: role.Name, Permissions: append([]string(nil), role.Permissions...),
		IsSuperAdmin: user.IsSuperAdmin, JoinedAt: member.JoinedAt, UpdatedAt: member.UpdatedAt,
	}, nil
}

func (m *Memory) RemoveProjectMember(ctx context.Context, spaceID, projectID string, userID uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := projectMemberKey(projectID, userID)
	member, ok := m.projectMembers[key]
	if !ok || member.SpaceID != spaceID {
		return ErrNotFound
	}
	if member.RoleKey == "project_maintainer" {
		maintainers := 0
		for _, candidate := range m.projectMembers {
			if candidate.ProjectID == projectID && candidate.SpaceID == spaceID && candidate.RoleKey == "project_maintainer" {
				maintainers++
			}
		}
		if maintainers <= 1 {
			return fmt.Errorf("%w: project must keep at least one maintainer", ErrForbidden)
		}
	}
	delete(m.projectMembers, key)
	return nil
}

func (m *Memory) ListProjectRoles(ctx context.Context, spaceID string) ([]domain.ProjectRole, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return nil, ErrNotFound
	}
	result := systemProjectRoles(spaceID)
	for _, role := range m.projectRoles {
		if role.SpaceID == spaceID {
			role.Permissions = append([]string(nil), role.Permissions...)
			result = append(result, role)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].IsSystem != result[j].IsSystem {
			return result[i].IsSystem
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func (m *Memory) CreateProjectRole(ctx context.Context, spaceID string, createdBy uint64, input CreateProjectRoleInput) (domain.ProjectRole, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProjectRole{}, err
	}
	input, err := normalizeProjectRoleInput(input)
	if err != nil {
		return domain.ProjectRole{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return domain.ProjectRole{}, ErrNotFound
	}
	if _, ok := m.users[createdBy]; !ok {
		return domain.ProjectRole{}, ErrNotFound
	}
	for _, role := range m.projectRoles {
		if role.SpaceID == spaceID && strings.EqualFold(role.Name, input.Name) {
			return domain.ProjectRole{}, ErrConflict
		}
	}
	now := time.Now().UTC()
	role := projectRoleFromCustom(newProjectRoleID(), spaceID, customProjectRoleKey(), input.Name, input.Description, createdBy, now, now, input.Permissions)
	m.projectRoles[role.ID] = role
	return role, nil
}

func (m *Memory) UpdateProjectRole(ctx context.Context, spaceID, roleID string, input UpdateProjectRoleInput) (domain.ProjectRole, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProjectRole{}, err
	}
	input, err := normalizeProjectRoleUpdateInput(input)
	if err != nil {
		return domain.ProjectRole{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	role, ok := m.projectRoles[strings.TrimSpace(roleID)]
	if !ok || role.SpaceID != spaceID {
		return domain.ProjectRole{}, ErrNotFound
	}
	if input.Name != nil {
		for id, existing := range m.projectRoles {
			if id != role.ID && existing.SpaceID == spaceID && strings.EqualFold(existing.Name, *input.Name) {
				return domain.ProjectRole{}, ErrConflict
			}
		}
		role.Name = *input.Name
	}
	if input.Description != nil {
		role.Description = *input.Description
	}
	if input.Permissions != nil {
		role.Permissions = append([]string(nil), (*input.Permissions)...)
	}
	role.UpdatedAt = time.Now().UTC()
	m.projectRoles[role.ID] = role
	return role, nil
}

func (m *Memory) DeleteProjectRole(ctx context.Context, spaceID, roleID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	role, ok := m.projectRoles[strings.TrimSpace(roleID)]
	if !ok || role.SpaceID != spaceID {
		return ErrNotFound
	}
	for _, member := range m.projectMembers {
		if member.SpaceID == spaceID && member.RoleID == role.ID {
			return ErrConflict
		}
	}
	delete(m.projectRoles, role.ID)
	return nil
}
