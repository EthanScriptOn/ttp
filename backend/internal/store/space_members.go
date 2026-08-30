package store

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
)

const (
	memberPasswordMinLength = 8
	memberPasswordMaxLength = 128
)

func updateSpaceValue(current domain.Space, input UpdateSpaceInput) (domain.Space, error) {
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return domain.Space{}, fmt.Errorf("%w: space name is required", ErrInvalidInput)
		}
		if len([]rune(name)) > 120 {
			return domain.Space{}, fmt.Errorf("%w: space name is too long", ErrInvalidInput)
		}
		current.Name = name
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		if len([]rune(description)) > 255 {
			return domain.Space{}, fmt.Errorf("%w: space description is too long", ErrInvalidInput)
		}
		current.Description = description
	}
	return current, nil
}

func normalizeMemberCreateInput(input CreateSpaceMemberInput) (CreateSpaceMemberInput, error) {
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" {
		return CreateSpaceMemberInput{}, fmt.Errorf("%w: username is required", ErrInvalidInput)
	}
	if len([]rune(input.Username)) > 100 || hasSpaceOrControl(input.Username) {
		return CreateSpaceMemberInput{}, fmt.Errorf("%w: username is invalid", ErrInvalidInput)
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" {
		input.DisplayName = input.Username
	}
	if len([]rune(input.DisplayName)) > 120 || hasControl(input.DisplayName) {
		return CreateSpaceMemberInput{}, fmt.Errorf("%w: display name is invalid", ErrInvalidInput)
	}
	input.Password = strings.TrimSpace(input.Password)
	if input.Password != "" && (len([]byte(input.Password)) < memberPasswordMinLength || len([]byte(input.Password)) > memberPasswordMaxLength) {
		return CreateSpaceMemberInput{}, fmt.Errorf("%w: password must be 8 to 128 characters", ErrInvalidInput)
	}
	role, err := normalizeMemberRole(input.Role, false)
	if err != nil {
		return CreateSpaceMemberInput{}, err
	}
	input.Role = role
	return input, nil
}

func normalizeMemberUpdateInput(input UpdateSpaceMemberInput) (UpdateSpaceMemberInput, error) {
	role, err := normalizeMemberRole(input.Role, false)
	if err != nil {
		return UpdateSpaceMemberInput{}, err
	}
	input.Role = role
	return input, nil
}

func normalizeMemberRole(value string, allowOwner bool) (string, error) {
	role := permissions.NormalizeRole(value)
	if role == "" {
		return "", fmt.Errorf("%w: role is invalid", ErrInvalidInput)
	}
	if role == "owner" && !allowOwner {
		return "", fmt.Errorf("%w: owner role requires an explicit ownership transfer", ErrForbidden)
	}
	return role, nil
}

func hasSpaceOrControl(value string) bool {
	for _, char := range value {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return true
		}
	}
	return false
}

func hasControl(value string) bool {
	for _, char := range value {
		if unicode.IsControl(char) {
			return true
		}
	}
	return false
}

func memberRoleOrder(role string) int {
	switch permissions.NormalizeRole(role) {
	case "owner":
		return 0
	case "admin":
		return 1
	case "developer":
		return 2
	case "viewer":
		return 3
	default:
		return 9
	}
}

func spaceMemberView(user domain.User, role string, joinedAt time.Time) domain.SpaceMember {
	return domain.SpaceMember{
		UserID:       user.ID,
		Username:     user.Username,
		DisplayName:  user.DisplayName,
		Role:         permissions.NormalizeRole(role),
		IsSuperAdmin: user.IsSuperAdmin,
		JoinedAt:     joinedAt,
	}
}
