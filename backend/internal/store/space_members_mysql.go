package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

type spaceMemberQueryRow struct {
	UserID       uint64    `gorm:"column:user_id"`
	Username     string    `gorm:"column:username"`
	DisplayName  string    `gorm:"column:display_name"`
	Role         string    `gorm:"column:role"`
	JoinedAt     time.Time `gorm:"column:joined_at"`
	IsSuperAdmin bool      `gorm:"column:is_super_admin"`
}

func (s *MySQL) UpdateSpace(ctx context.Context, spaceID string, input UpdateSpaceInput) (domain.Space, error) {
	spaceID = strings.TrimSpace(spaceID)
	var row spaceRow
	if err := s.db.WithContext(ctx).Where("id = ?", spaceID).First(&row).Error; err != nil {
		return domain.Space{}, mapDBError(err)
	}
	current := toSpace(row)
	updated, err := updateSpaceValue(current, input)
	if err != nil {
		return domain.Space{}, err
	}
	updates := map[string]any{"name": updated.Name, "description": updated.Description}
	if err := s.db.WithContext(ctx).Model(&spaceRow{}).Where("id = ?", spaceID).Updates(updates).Error; err != nil {
		return domain.Space{}, err
	}
	return s.SpaceByID(ctx, spaceID)
}

// SpaceByID is intentionally internal: callers that need authorization should
// use Space, which verifies the user membership first.
func (s *MySQL) SpaceByID(ctx context.Context, spaceID string) (domain.Space, error) {
	var row spaceRow
	if err := s.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(spaceID)).First(&row).Error; err != nil {
		return domain.Space{}, mapDBError(err)
	}
	return toSpace(row), nil
}

func (s *MySQL) ListSpaceMembers(ctx context.Context, spaceID string) ([]domain.SpaceMember, error) {
	spaceID = strings.TrimSpace(spaceID)
	var space spaceRow
	if err := s.db.WithContext(ctx).Where("id = ?", spaceID).First(&space).Error; err != nil {
		return nil, mapDBError(err)
	}
	rows := make([]spaceMemberQueryRow, 0)
	if err := s.db.WithContext(ctx).
		Table("space_members AS sm").
		Select("sm.user_id, u.username, u.display_name, sm.role, sm.created_at AS joined_at, u.is_super_admin").
		Joins("JOIN users AS u ON u.id = sm.user_id").
		Where("sm.space_id = ?", spaceID).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.SpaceMember, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.SpaceMember{UserID: row.UserID, Username: row.Username, DisplayName: row.DisplayName, Role: row.Role, IsSuperAdmin: row.IsSuperAdmin, JoinedAt: row.JoinedAt})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if memberRoleOrder(result[i].Role) != memberRoleOrder(result[j].Role) {
			return memberRoleOrder(result[i].Role) < memberRoleOrder(result[j].Role)
		}
		return strings.ToLower(result[i].Username) < strings.ToLower(result[j].Username)
	})
	return result, nil
}

func (s *MySQL) CreateSpaceMember(ctx context.Context, spaceID string, input CreateSpaceMemberInput) (domain.SpaceMember, error) {
	input, err := normalizeMemberCreateInput(input)
	if err != nil {
		return domain.SpaceMember{}, err
	}
	var passwordHash string
	if input.Password != "" {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			return domain.SpaceMember{}, fmt.Errorf("%w: password could not be secured", ErrInvalidInput)
		}
		passwordHash = string(hash)
	}
	spaceID = strings.TrimSpace(spaceID)
	var user userRow
	var member memberRow
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var space spaceRow
		if queryErr := tx.Where("id = ?", spaceID).First(&space).Error; queryErr != nil {
			return mapDBError(queryErr)
		}
		queryErr := tx.Where("username = ?", input.Username).First(&user).Error
		switch queryErr {
		case nil:
		case gorm.ErrRecordNotFound:
			if passwordHash == "" {
				return fmt.Errorf("%w: password is required for a new user", ErrInvalidInput)
			}
			user = userRow{Username: input.Username, DisplayName: input.DisplayName, PasswordHash: passwordHash}
			if createErr := tx.Create(&user).Error; createErr != nil {
				if strings.Contains(strings.ToLower(createErr.Error()), "duplicate") {
					return ErrConflict
				}
				return createErr
			}
		default:
			return queryErr
		}
		var existing memberRow
		if queryErr = tx.Where("user_id = ? AND space_id = ?", user.ID, spaceID).First(&existing).Error; queryErr == nil {
			return ErrConflict
		} else if queryErr != gorm.ErrRecordNotFound {
			return queryErr
		}
		member = memberRow{UserID: user.ID, SpaceID: spaceID, Role: input.Role}
		if createErr := tx.Create(&member).Error; createErr != nil {
			if strings.Contains(strings.ToLower(createErr.Error()), "duplicate") {
				return ErrConflict
			}
			return createErr
		}
		return nil
	})
	if err != nil {
		return domain.SpaceMember{}, err
	}
	return spaceMemberView(toUser(user), member.Role, member.CreatedAt), nil
}

func (s *MySQL) UpdateSpaceMember(ctx context.Context, spaceID string, userID uint64, input UpdateSpaceMemberInput) (domain.SpaceMember, error) {
	input, err := normalizeMemberUpdateInput(input)
	if err != nil {
		return domain.SpaceMember{}, err
	}
	spaceID = strings.TrimSpace(spaceID)
	var member memberRow
	if err := s.db.WithContext(ctx).Where("user_id = ? AND space_id = ?", userID, spaceID).First(&member).Error; err != nil {
		return domain.SpaceMember{}, mapDBError(err)
	}
	if member.Role == "owner" {
		return domain.SpaceMember{}, fmt.Errorf("%w: the owner cannot be changed here", ErrForbidden)
	}
	if err := s.db.WithContext(ctx).Model(&memberRow{}).Where("user_id = ? AND space_id = ?", userID, spaceID).Update("role", input.Role).Error; err != nil {
		return domain.SpaceMember{}, err
	}
	return s.findSpaceMember(ctx, spaceID, userID)
}

func (s *MySQL) RemoveSpaceMember(ctx context.Context, spaceID string, userID uint64) error {
	spaceID = strings.TrimSpace(spaceID)
	var member memberRow
	if err := s.db.WithContext(ctx).Where("user_id = ? AND space_id = ?", userID, spaceID).First(&member).Error; err != nil {
		return mapDBError(err)
	}
	if member.Role == "owner" {
		return fmt.Errorf("%w: the owner cannot be removed", ErrForbidden)
	}
	result := s.db.WithContext(ctx).Where("user_id = ? AND space_id = ?", userID, spaceID).Delete(&memberRow{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MySQL) findSpaceMember(ctx context.Context, spaceID string, userID uint64) (domain.SpaceMember, error) {
	var row spaceMemberQueryRow
	if err := s.db.WithContext(ctx).
		Table("space_members AS sm").
		Select("sm.user_id, u.username, u.display_name, sm.role, sm.created_at AS joined_at, u.is_super_admin").
		Joins("JOIN users AS u ON u.id = sm.user_id").
		Where("sm.space_id = ? AND sm.user_id = ?", spaceID, userID).
		Scan(&row).Error; err != nil {
		return domain.SpaceMember{}, err
	}
	if row.UserID == 0 {
		return domain.SpaceMember{}, ErrNotFound
	}
	return domain.SpaceMember{UserID: row.UserID, Username: row.Username, DisplayName: row.DisplayName, Role: row.Role, IsSuperAdmin: row.IsSuperAdmin, JoinedAt: row.JoinedAt}, nil
}
