package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func (s *Server) getSpaceSettings(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	space, err := s.deps.Store.Space(c.Request.Context(), claims.UserID, claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	role, err := s.deps.Store.Role(c.Request.Context(), claims.UserID, claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"space":     space,
		"role":      role,
		"role_name": permissions.RoleName(role),
	})
}

func (s *Server) updateSpaceSettings(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	var input store.UpdateSpaceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "空间配置格式不正确")
		return
	}
	space, err := s.deps.Store.UpdateSpace(c.Request.Context(), claims.SpaceID, input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	role, err := s.deps.Store.Role(c.Request.Context(), claims.UserID, claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	space.Role = role
	s.recordAudit(c, "更新空间设置", space.Name)
	c.JSON(http.StatusOK, gin.H{"space": space, "role": role, "role_name": permissions.RoleName(role)})
}

func (s *Server) getSpaceMembers(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	items, err := s.deps.Store.ListSpaceMembers(c.Request.Context(), claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	for index := range items {
		items[index].IsCurrentUser = items[index].UserID == claims.UserID
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items), "current_user_id": claims.UserID})
}

func (s *Server) createSpaceMember(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	actorRole, ok := s.spaceActorRole(c, claims)
	if !ok {
		return
	}
	var input store.CreateSpaceMemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "成员配置格式不正确")
		return
	}
	if !permissions.CanAssignRole(actorRole, input.Role) {
		writeError(c, http.StatusForbidden, "forbidden", "不能分配这个空间角色")
		return
	}
	member, err := s.deps.Store.CreateSpaceMember(c.Request.Context(), claims.SpaceID, input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "添加空间成员", member.Username)
	c.JSON(http.StatusCreated, gin.H{"member": member})
}

func (s *Server) updateSpaceMember(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	actorRole, ok := s.spaceActorRole(c, claims)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(strings.TrimSpace(c.Param("userID")), 10, 64)
	if err != nil || userID == 0 {
		writeError(c, http.StatusBadRequest, "invalid_request", "成员编号不正确")
		return
	}
	current, err := s.findSpaceMember(c, claims.SpaceID, userID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !permissions.CanManageTarget(actorRole, current.Role) {
		writeError(c, http.StatusForbidden, "forbidden", "所有者不能在这里被修改")
		return
	}
	var input store.UpdateSpaceMemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "角色配置格式不正确")
		return
	}
	if !permissions.CanAssignRole(actorRole, input.Role) {
		writeError(c, http.StatusForbidden, "forbidden", "不能分配这个空间角色")
		return
	}
	if userID == claims.UserID && permissions.NormalizeRole(input.Role) != permissions.NormalizeRole(current.Role) {
		writeError(c, http.StatusForbidden, "forbidden", "不能直接修改自己的空间角色")
		return
	}
	member, err := s.deps.Store.UpdateSpaceMember(c.Request.Context(), claims.SpaceID, userID, input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "调整空间成员角色", member.Username+" · "+permissions.RoleName(member.Role))
	c.JSON(http.StatusOK, gin.H{"member": member})
}

func (s *Server) removeSpaceMember(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	actorRole, ok := s.spaceActorRole(c, claims)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(strings.TrimSpace(c.Param("userID")), 10, 64)
	if err != nil || userID == 0 {
		writeError(c, http.StatusBadRequest, "invalid_request", "成员编号不正确")
		return
	}
	if userID == claims.UserID {
		writeError(c, http.StatusForbidden, "forbidden", "不能移除自己，请让其他管理员操作")
		return
	}
	member, err := s.findSpaceMember(c, claims.SpaceID, userID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if !permissions.CanManageTarget(actorRole, member.Role) {
		writeError(c, http.StatusForbidden, "forbidden", "所有者不能被移除")
		return
	}
	if err := s.deps.Store.RemoveSpaceMember(c.Request.Context(), claims.SpaceID, userID); err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "移除空间成员", member.Username)
	c.JSON(http.StatusOK, gin.H{"removed": true, "user_id": userID})
}

func (s *Server) getSpacePermissions(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	role, err := s.deps.Store.Role(c.Request.Context(), claims.UserID, claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"role":        role,
		"role_name":   permissions.RoleName(role),
		"roles":       permissions.RoleDefinitions(),
		"permissions": permissions.PermissionDefinitions(),
	})
}

func (s *Server) spaceActorRole(c *gin.Context, claims auth.Claims) (string, bool) {
	role, err := s.deps.Store.Role(c.Request.Context(), claims.UserID, claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return "", false
	}
	return role, true
}

func (s *Server) findSpaceMember(c *gin.Context, spaceID string, userID uint64) (domain.SpaceMember, error) {
	items, err := s.deps.Store.ListSpaceMembers(c.Request.Context(), spaceID)
	if err != nil {
		return domain.SpaceMember{}, err
	}
	for _, item := range items {
		if item.UserID == userID {
			return item, nil
		}
	}
	return domain.SpaceMember{}, store.ErrNotFound
}
