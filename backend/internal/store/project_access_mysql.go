package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
)

type projectMemberQueryRow struct {
	ProjectID    string    `gorm:"column:project_id"`
	SpaceID      string    `gorm:"column:space_id"`
	UserID       uint64    `gorm:"column:user_id"`
	Username     string    `gorm:"column:username"`
	DisplayName  string    `gorm:"column:display_name"`
	RoleID       string    `gorm:"column:role_id"`
	RoleKey      string    `gorm:"column:role_key"`
	IsSuperAdmin bool      `gorm:"column:is_super_admin"`
	JoinedAt     time.Time `gorm:"column:joined_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

func (s *MySQL) customProjectRole(ctx context.Context, spaceID, roleID, roleKey string) (domain.ProjectRole, error) {
	roleID, roleKey, err := normalizeProjectRoleReference(roleID, roleKey)
	if err != nil {
		return domain.ProjectRole{}, err
	}
	if strings.HasPrefix(roleID, projectSystemRoleID) {
		roleKey = strings.TrimPrefix(roleID, projectSystemRoleID)
	}
	if roleKey != "" {
		if role, ok := projectRoleByKey(spaceID, roleKey); ok {
			return role, nil
		}
	}
	query := s.db.WithContext(ctx).Where("space_id = ?", spaceID)
	if roleID != "" {
		query = query.Where("id = ?", roleID)
	} else {
		query = query.Where("`key` = ?", roleKey)
	}
	var row projectRoleRow
	if err := query.First(&row).Error; err != nil {
		return domain.ProjectRole{}, mapDBError(err)
	}
	permissionsList, err := s.projectRolePermissions(ctx, row.ID)
	if err != nil {
		return domain.ProjectRole{}, err
	}
	return projectRoleFromCustom(row.ID, row.SpaceID, row.Key, row.Name, row.Description, row.CreatedBy, row.CreatedAt, row.UpdatedAt, permissionsList), nil
}

func (s *MySQL) projectRolePermissions(ctx context.Context, roleID string) ([]string, error) {
	var rows []projectRolePermissionRow
	if err := s.db.WithContext(ctx).Where("role_id = ?", roleID).Order("permission_key ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.PermissionKey)
	}
	return result, nil
}

func (s *MySQL) projectRole(ctx context.Context, spaceID, roleID, roleKey string) (domain.ProjectRole, error) {
	roleID = strings.TrimSpace(roleID)
	roleKey = strings.ToLower(strings.TrimSpace(roleKey))
	if strings.HasPrefix(roleID, projectSystemRoleID) {
		roleKey = strings.TrimPrefix(roleID, projectSystemRoleID)
	}
	if roleKey != "" {
		if role, ok := projectRoleByKey(spaceID, roleKey); ok {
			return role, nil
		}
	}
	return s.customProjectRole(ctx, spaceID, roleID, roleKey)
}

func (s *MySQL) ListProjectsForUser(ctx context.Context, userID uint64, spaceID string) ([]domain.Project, error) {
	projects, err := s.ListProjects(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.Project, 0, len(projects))
	for _, project := range projects {
		if _, accessErr := s.GetProjectAccess(ctx, userID, spaceID, project.ID); accessErr == nil {
			result = append(result, project)
		}
	}
	return result, nil
}

func (s *MySQL) GetProjectAccess(ctx context.Context, userID uint64, spaceID, projectID string) (domain.ProjectAccess, error) {
	user, err := s.User(ctx, userID)
	if err != nil {
		return domain.ProjectAccess{}, err
	}
	project, err := s.GetProject(ctx, spaceID, projectID)
	if err != nil {
		return domain.ProjectAccess{}, err
	}
	if user.IsSuperAdmin {
		return domain.ProjectAccess{
			ProjectID: project.ID, SpaceID: spaceID, RoleID: projectSystemRoleID + "project_maintainer",
			RoleKey: "project_maintainer", RoleName: "项目维护者", Permissions: allProjectPermissions(),
			IsSpaceAdmin: true, IsSuperAdmin: true,
		}, nil
	}
	spaceRole, err := s.Role(ctx, userID, spaceID)
	if err != nil {
		return domain.ProjectAccess{}, err
	}
	if normalized := permissions.NormalizeRole(spaceRole); normalized == "owner" || normalized == "admin" {
		definition, _ := projectRoleByKey(spaceID, "project_maintainer")
		return domain.ProjectAccess{
			ProjectID: project.ID, SpaceID: spaceID, RoleID: projectSystemRoleID + definition.Key,
			RoleKey: definition.Key, RoleName: definition.Name, Permissions: allProjectPermissions(),
			IsSpaceAdmin: true,
		}, nil
	}
	var row projectMemberRow
	if err := s.db.WithContext(ctx).Where("project_id = ? AND user_id = ? AND space_id = ?", projectID, userID, spaceID).First(&row).Error; err != nil {
		return domain.ProjectAccess{}, mapDBError(err)
	}
	role, err := s.projectRole(ctx, spaceID, row.RoleID, row.RoleKey)
	if err != nil {
		return domain.ProjectAccess{}, err
	}
	return domain.ProjectAccess{
		ProjectID: project.ID, SpaceID: spaceID, RoleID: role.ID, RoleKey: role.Key,
		RoleName: role.Name, Permissions: append([]string(nil), role.Permissions...),
	}, nil
}

func (s *MySQL) HasProjectPermission(ctx context.Context, userID uint64, spaceID, projectID, permission string) (bool, error) {
	access, err := s.GetProjectAccess(ctx, userID, spaceID, projectID)
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

func (s *MySQL) EnsureProjectMember(ctx context.Context, spaceID, projectID string, userID uint64, roleKey string) error {
	roleKey = strings.ToLower(strings.TrimSpace(roleKey))
	role, ok := projectRoleByKey(spaceID, roleKey)
	if !ok {
		return ErrNotFound
	}
	if _, err := s.GetProject(ctx, spaceID, projectID); err != nil {
		return err
	}
	if _, err := s.User(ctx, userID); err != nil {
		return err
	}
	if _, err := s.Role(ctx, userID, spaceID); err != nil {
		return err
	}
	var existing projectMemberRow
	err := s.db.WithContext(ctx).Where("project_id = ? AND user_id = ? AND space_id = ?", projectID, userID, spaceID).First(&existing).Error
	switch err {
	case nil:
		return nil
	case gorm.ErrRecordNotFound:
	default:
		return err
	}
	now := time.Now().UTC()
	row := projectMemberRow{ProjectID: projectID, UserID: userID, SpaceID: spaceID, RoleID: role.ID, RoleKey: role.Key, CreatedAt: now, UpdatedAt: now}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		return err
	}
	return nil
}

func (s *MySQL) ListProjectMembers(ctx context.Context, spaceID, projectID string) ([]domain.ProjectMember, error) {
	if _, err := s.GetProject(ctx, spaceID, projectID); err != nil {
		return nil, err
	}
	var rows []projectMemberQueryRow
	if err := s.db.WithContext(ctx).
		Table("project_members AS pm").
		Select("pm.project_id, pm.space_id, pm.user_id, u.username, u.display_name, pm.role_id, pm.role_key, u.is_super_admin, pm.created_at AS joined_at, pm.updated_at").
		Joins("JOIN users AS u ON u.id = pm.user_id").
		Where("pm.project_id = ? AND pm.space_id = ?", projectID, spaceID).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.ProjectMember, 0, len(rows))
	for _, row := range rows {
		role, err := s.projectRole(ctx, spaceID, row.RoleID, row.RoleKey)
		if err != nil {
			continue
		}
		result = append(result, domain.ProjectMember{
			ProjectID: row.ProjectID, SpaceID: row.SpaceID, UserID: row.UserID, Username: row.Username, DisplayName: row.DisplayName,
			RoleID: role.ID, RoleKey: role.Key, RoleName: role.Name, Permissions: append([]string(nil), role.Permissions...),
			IsSuperAdmin: row.IsSuperAdmin, JoinedAt: row.JoinedAt, UpdatedAt: row.UpdatedAt,
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

func (s *MySQL) CreateProjectMember(ctx context.Context, spaceID, projectID string, input CreateProjectMemberInput) (domain.ProjectMember, error) {
	roleID, roleKey, err := normalizeProjectRoleReference(input.RoleID, input.RoleKey)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	if _, err := s.GetProject(ctx, spaceID, projectID); err != nil {
		return domain.ProjectMember{}, err
	}
	user, err := s.User(ctx, input.UserID)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	if !user.IsSuperAdmin {
		if _, err := s.Role(ctx, input.UserID, spaceID); err != nil {
			return domain.ProjectMember{}, err
		}
	}
	role, err := s.projectRole(ctx, spaceID, roleID, roleKey)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	now := time.Now().UTC()
	row := projectMemberRow{ProjectID: projectID, UserID: input.UserID, SpaceID: spaceID, RoleID: role.ID, RoleKey: role.Key, CreatedAt: now, UpdatedAt: now}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return domain.ProjectMember{}, ErrConflict
		}
		return domain.ProjectMember{}, err
	}
	return domain.ProjectMember{
		ProjectID: projectID, SpaceID: spaceID, UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName,
		RoleID: role.ID, RoleKey: role.Key, RoleName: role.Name, Permissions: append([]string(nil), role.Permissions...),
		IsSuperAdmin: user.IsSuperAdmin, JoinedAt: now, UpdatedAt: now,
	}, nil
}

func (s *MySQL) UpdateProjectMember(ctx context.Context, spaceID, projectID string, userID uint64, input UpdateProjectMemberInput) (domain.ProjectMember, error) {
	roleID, roleKey, err := normalizeProjectRoleReference(input.RoleID, input.RoleKey)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	var current projectMemberRow
	if err := s.db.WithContext(ctx).Where("project_id = ? AND user_id = ? AND space_id = ?", projectID, userID, spaceID).First(&current).Error; err != nil {
		return domain.ProjectMember{}, mapDBError(err)
	}
	role, err := s.projectRole(ctx, spaceID, roleID, roleKey)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	now := time.Now().UTC()
	if err := s.db.WithContext(ctx).Model(&projectMemberRow{}).Where("project_id = ? AND user_id = ? AND space_id = ?", projectID, userID, spaceID).Updates(map[string]any{"role_id": role.ID, "role_key": role.Key, "updated_at": now}).Error; err != nil {
		return domain.ProjectMember{}, err
	}
	user, err := s.User(ctx, userID)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	return domain.ProjectMember{
		ProjectID: projectID, SpaceID: spaceID, UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName,
		RoleID: role.ID, RoleKey: role.Key, RoleName: role.Name, Permissions: append([]string(nil), role.Permissions...),
		IsSuperAdmin: user.IsSuperAdmin, JoinedAt: current.CreatedAt, UpdatedAt: now,
	}, nil
}

func (s *MySQL) RemoveProjectMember(ctx context.Context, spaceID, projectID string, userID uint64) error {
	var current projectMemberRow
	if err := s.db.WithContext(ctx).Where("project_id = ? AND user_id = ? AND space_id = ?", projectID, userID, spaceID).First(&current).Error; err != nil {
		return mapDBError(err)
	}
	if current.RoleKey == "project_maintainer" {
		var maintainers int64
		if err := s.db.WithContext(ctx).Model(&projectMemberRow{}).Where("project_id = ? AND space_id = ? AND role_key = ?", projectID, spaceID, "project_maintainer").Count(&maintainers).Error; err != nil {
			return err
		}
		if maintainers <= 1 {
			return fmt.Errorf("%w: project must keep at least one maintainer", ErrForbidden)
		}
	}
	result := s.db.WithContext(ctx).Where("project_id = ? AND user_id = ? AND space_id = ?", projectID, userID, spaceID).Delete(&projectMemberRow{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MySQL) ListProjectRoles(ctx context.Context, spaceID string) ([]domain.ProjectRole, error) {
	if _, err := s.SpaceByID(ctx, spaceID); err != nil {
		return nil, err
	}
	result := systemProjectRoles(spaceID)
	var rows []projectRoleRow
	if err := s.db.WithContext(ctx).Where("space_id = ?", spaceID).Order("name ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		permissionsList, err := s.projectRolePermissions(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, projectRoleFromCustom(row.ID, row.SpaceID, row.Key, row.Name, row.Description, row.CreatedBy, row.CreatedAt, row.UpdatedAt, permissionsList))
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].IsSystem != result[j].IsSystem {
			return result[i].IsSystem
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func (s *MySQL) CreateProjectRole(ctx context.Context, spaceID string, createdBy uint64, input CreateProjectRoleInput) (domain.ProjectRole, error) {
	input, err := normalizeProjectRoleInput(input)
	if err != nil {
		return domain.ProjectRole{}, err
	}
	if _, err := s.SpaceByID(ctx, spaceID); err != nil {
		return domain.ProjectRole{}, err
	}
	if _, err := s.User(ctx, createdBy); err != nil {
		return domain.ProjectRole{}, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&projectRoleRow{}).Where("space_id = ? AND LOWER(name) = LOWER(?)", spaceID, input.Name).Count(&count).Error; err != nil {
		return domain.ProjectRole{}, err
	}
	if count > 0 {
		return domain.ProjectRole{}, ErrConflict
	}
	now := time.Now().UTC()
	role := projectRoleFromCustom(newProjectRoleID(), spaceID, customProjectRoleKey(), input.Name, input.Description, createdBy, now, now, input.Permissions)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&projectRoleRow{ID: role.ID, SpaceID: spaceID, Key: role.Key, Name: role.Name, Description: role.Description, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return ErrConflict
			}
			return err
		}
		for _, permission := range role.Permissions {
			if err := tx.Create(&projectRolePermissionRow{RoleID: role.ID, PermissionKey: permission}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domain.ProjectRole{}, err
	}
	return role, nil
}

func (s *MySQL) UpdateProjectRole(ctx context.Context, spaceID, roleID string, input UpdateProjectRoleInput) (domain.ProjectRole, error) {
	input, err := normalizeProjectRoleUpdateInput(input)
	if err != nil {
		return domain.ProjectRole{}, err
	}
	var row projectRoleRow
	if err := s.db.WithContext(ctx).Where("id = ? AND space_id = ?", strings.TrimSpace(roleID), spaceID).First(&row).Error; err != nil {
		return domain.ProjectRole{}, mapDBError(err)
	}
	if input.Name != nil {
		var count int64
		if err := s.db.WithContext(ctx).Model(&projectRoleRow{}).Where("space_id = ? AND id <> ? AND LOWER(name) = LOWER(?)", spaceID, row.ID, *input.Name).Count(&count).Error; err != nil {
			return domain.ProjectRole{}, err
		}
		if count > 0 {
			return domain.ProjectRole{}, ErrConflict
		}
		row.Name = *input.Name
	}
	if input.Description != nil {
		row.Description = *input.Description
	}
	row.UpdatedAt = time.Now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&projectRoleRow{}).Where("id = ? AND space_id = ?", row.ID, spaceID).Updates(map[string]any{"name": row.Name, "description": row.Description, "updated_at": row.UpdatedAt}).Error; err != nil {
			return err
		}
		if input.Permissions != nil {
			if err := tx.Where("role_id = ?", row.ID).Delete(&projectRolePermissionRow{}).Error; err != nil {
				return err
			}
			for _, permission := range *input.Permissions {
				if err := tx.Create(&projectRolePermissionRow{RoleID: row.ID, PermissionKey: permission}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return domain.ProjectRole{}, err
	}
	permissionsList, err := s.projectRolePermissions(ctx, row.ID)
	if err != nil {
		return domain.ProjectRole{}, err
	}
	return projectRoleFromCustom(row.ID, row.SpaceID, row.Key, row.Name, row.Description, row.CreatedBy, row.CreatedAt, row.UpdatedAt, permissionsList), nil
}

func (s *MySQL) DeleteProjectRole(ctx context.Context, spaceID, roleID string) error {
	var row projectRoleRow
	if err := s.db.WithContext(ctx).Where("id = ? AND space_id = ?", strings.TrimSpace(roleID), spaceID).First(&row).Error; err != nil {
		return mapDBError(err)
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&projectMemberRow{}).Where("role_id = ? AND space_id = ?", row.ID, spaceID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrConflict
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", row.ID).Delete(&projectRolePermissionRow{}).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND space_id = ?", row.ID, spaceID).Delete(&projectRoleRow{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}
