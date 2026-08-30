package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

func (m *Memory) UpdateSpace(ctx context.Context, spaceID string, input UpdateSpaceInput) (domain.Space, error) {
	if err := ctx.Err(); err != nil {
		return domain.Space{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.spaces[strings.TrimSpace(spaceID)]
	if !ok {
		return domain.Space{}, ErrNotFound
	}
	updated, err := updateSpaceValue(current, input)
	if err != nil {
		return domain.Space{}, err
	}
	m.spaces[updated.ID] = updated
	return updated, nil
}

func (m *Memory) ListSpaceMembers(ctx context.Context, spaceID string) ([]domain.SpaceMember, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	spaceID = strings.TrimSpace(spaceID)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return nil, ErrNotFound
	}
	result := make([]domain.SpaceMember, 0)
	for _, member := range m.memberships {
		if member.SpaceID != spaceID {
			continue
		}
		user, ok := m.users[member.UserID]
		if !ok {
			continue
		}
		joinedAt := member.JoinedAt
		if joinedAt.IsZero() {
			joinedAt = time.Time{}
		}
		result = append(result, spaceMemberView(user, member.Role, joinedAt))
	}
	sort.SliceStable(result, func(i, j int) bool {
		if memberRoleOrder(result[i].Role) != memberRoleOrder(result[j].Role) {
			return memberRoleOrder(result[i].Role) < memberRoleOrder(result[j].Role)
		}
		return strings.ToLower(result[i].Username) < strings.ToLower(result[j].Username)
	})
	return result, nil
}

func (m *Memory) CreateSpaceMember(ctx context.Context, spaceID string, input CreateSpaceMemberInput) (domain.SpaceMember, error) {
	if err := ctx.Err(); err != nil {
		return domain.SpaceMember{}, err
	}
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return domain.SpaceMember{}, ErrNotFound
	}
	var existingUser domain.User
	var found bool
	for _, user := range m.users {
		if strings.EqualFold(user.Username, input.Username) {
			existingUser = user
			found = true
			break
		}
	}
	if found {
		if _, exists := m.memberships[memberKey(existingUser.ID, spaceID)]; exists {
			return domain.SpaceMember{}, ErrConflict
		}
	} else {
		if passwordHash == "" {
			return domain.SpaceMember{}, fmt.Errorf("%w: password is required for a new user", ErrInvalidInput)
		}
		var nextID uint64
		for id := range m.users {
			if id > nextID {
				nextID = id
			}
		}
		existingUser = domain.User{ID: nextID + 1, Username: input.Username, DisplayName: input.DisplayName, PasswordHash: passwordHash}
		m.users[existingUser.ID] = existingUser
	}
	joinedAt := time.Now().UTC()
	m.memberships[memberKey(existingUser.ID, spaceID)] = membership{UserID: existingUser.ID, SpaceID: spaceID, Role: input.Role, JoinedAt: joinedAt}
	return spaceMemberView(existingUser, input.Role, joinedAt), nil
}

func (m *Memory) UpdateSpaceMember(ctx context.Context, spaceID string, userID uint64, input UpdateSpaceMemberInput) (domain.SpaceMember, error) {
	if err := ctx.Err(); err != nil {
		return domain.SpaceMember{}, err
	}
	input, err := normalizeMemberUpdateInput(input)
	if err != nil {
		return domain.SpaceMember{}, err
	}
	spaceID = strings.TrimSpace(spaceID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return domain.SpaceMember{}, ErrNotFound
	}
	key := memberKey(userID, spaceID)
	member, ok := m.memberships[key]
	if !ok {
		return domain.SpaceMember{}, ErrNotFound
	}
	if member.Role == "owner" {
		return domain.SpaceMember{}, fmt.Errorf("%w: the owner cannot be changed here", ErrForbidden)
	}
	user, ok := m.users[userID]
	if !ok {
		return domain.SpaceMember{}, ErrNotFound
	}
	member.Role = input.Role
	m.memberships[key] = member
	return spaceMemberView(user, member.Role, member.JoinedAt), nil
}

func (m *Memory) RemoveSpaceMember(ctx context.Context, spaceID string, userID uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spaceID = strings.TrimSpace(spaceID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return ErrNotFound
	}
	key := memberKey(userID, spaceID)
	member, ok := m.memberships[key]
	if !ok {
		return ErrNotFound
	}
	if member.Role == "owner" {
		return fmt.Errorf("%w: the owner cannot be removed", ErrForbidden)
	}
	delete(m.memberships, key)
	return nil
}
