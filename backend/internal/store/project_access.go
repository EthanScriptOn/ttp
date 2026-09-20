package store

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
)

const (
	projectRoleIDPrefix = "prole-"
	projectSystemRoleID = "system:"
)

func systemProjectRoles(spaceID string) []domain.ProjectRole {
	definitions := permissions.ProjectRoleDefinitions()
	result := make([]domain.ProjectRole, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, domain.ProjectRole{
			ID:          projectSystemRoleID + definition.Key,
			SpaceID:     spaceID,
			Key:         definition.Key,
			Name:        definition.Name,
			Description: definition.Description,
			IsSystem:    true,
			Permissions: append([]string(nil), definition.Permissions...),
		})
	}
	return result
}

func projectRoleFromCustom(rowID, spaceID, key, name, description string, createdBy uint64, createdAt, updatedAt time.Time, permissionKeys []string) domain.ProjectRole {
	return domain.ProjectRole{
		ID:          rowID,
		SpaceID:     spaceID,
		Key:         key,
		Name:        name,
		Description: description,
		IsSystem:    false,
		Permissions: append([]string(nil), permissionKeys...),
		CreatedBy:   createdBy,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

func normalizeProjectRoleInput(input CreateProjectRoleInput) (CreateProjectRoleInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || len([]rune(input.Name)) > 120 {
		return CreateProjectRoleInput{}, fmt.Errorf("%w: project role name is invalid", ErrInvalidInput)
	}
	if len([]rune(input.Description)) > 255 {
		return CreateProjectRoleInput{}, fmt.Errorf("%w: project role description is too long", ErrInvalidInput)
	}
	permissionsList, err := normalizeProjectRolePermissions(input.Permissions)
	if err != nil {
		return CreateProjectRoleInput{}, err
	}
	input.Permissions = permissionsList
	return input, nil
}

func normalizeProjectRoleUpdateInput(input UpdateProjectRoleInput) (UpdateProjectRoleInput, error) {
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if value == "" || len([]rune(value)) > 120 {
			return UpdateProjectRoleInput{}, fmt.Errorf("%w: project role name is invalid", ErrInvalidInput)
		}
		*input.Name = value
	}
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		if len([]rune(value)) > 255 {
			return UpdateProjectRoleInput{}, fmt.Errorf("%w: project role description is too long", ErrInvalidInput)
		}
		*input.Description = value
	}
	if input.Permissions != nil {
		permissionsList, err := normalizeProjectRolePermissions(*input.Permissions)
		if err != nil {
			return UpdateProjectRoleInput{}, err
		}
		*input.Permissions = permissionsList
	}
	return input, nil
}

func normalizeProjectRolePermissions(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if !permissions.IsProjectPermission(key) {
			return nil, fmt.Errorf("%w: unknown project permission %q", ErrInvalidInput, key)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: project role must contain at least one permission", ErrInvalidInput)
	}
	return result, nil
}

func normalizeProjectRoleReference(roleID, roleKey string) (string, string, error) {
	roleID = strings.TrimSpace(roleID)
	roleKey = strings.ToLower(strings.TrimSpace(roleKey))
	if roleID == "" && roleKey == "" {
		return "", "", fmt.Errorf("%w: project role is required", ErrInvalidInput)
	}
	return roleID, roleKey, nil
}

func projectRoleByKey(spaceID, key string) (domain.ProjectRole, bool) {
	for _, role := range systemProjectRoles(spaceID) {
		if role.Key == strings.ToLower(strings.TrimSpace(key)) {
			return role, true
		}
	}
	return domain.ProjectRole{}, false
}

func projectRolePermissionSet(role domain.ProjectRole) map[string]struct{} {
	result := make(map[string]struct{}, len(role.Permissions))
	for _, permission := range role.Permissions {
		result[permission] = struct{}{}
	}
	return result
}

func roleHasProjectPermission(role domain.ProjectRole, permission string) bool {
	_, ok := projectRolePermissionSet(role)[strings.TrimSpace(permission)]
	return ok
}

func newProjectRoleID() string {
	return projectRoleIDPrefix + uuid.NewString()[:16]
}

func customProjectRoleKey() string {
	return "custom_" + uuid.NewString()[:12]
}

func allProjectPermissions() []string {
	result := make([]string, 0)
	for _, definition := range permissions.ProjectPermissionDefinitions() {
		result = append(result, definition.Key)
	}
	sort.Strings(result)
	return result
}
