package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func (s *Server) getProjectAccess(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	access, err := s.deps.Store.GetProjectAccess(c.Request.Context(), claims.UserID, claims.SpaceID, c.Param("projectID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"access": access})
}

func (s *Server) listProjectMembers(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	items, err := s.deps.Store.ListProjectMembers(c.Request.Context(), claims.SpaceID, c.Param("projectID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	for index := range items {
		items[index].IsCurrentUser = items[index].UserID == claims.UserID
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items), "current_user_id": claims.UserID})
}

func (s *Server) createProjectMember(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	var input store.CreateProjectMemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "项目成员配置格式不正确")
		return
	}
	if input.UserID == 0 {
		writeError(c, http.StatusBadRequest, "invalid_request", "请选择空间成员")
		return
	}
	member, err := s.deps.Store.CreateProjectMember(c.Request.Context(), claims.SpaceID, c.Param("projectID"), input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "添加项目成员", member.Username+" · "+member.RoleName)
	c.JSON(http.StatusCreated, gin.H{"member": member})
}

func (s *Server) updateProjectMember(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(strings.TrimSpace(c.Param("userID")), 10, 64)
	if err != nil || userID == 0 {
		writeError(c, http.StatusBadRequest, "invalid_request", "成员编号不正确")
		return
	}
	var input store.UpdateProjectMemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "项目角色配置格式不正确")
		return
	}
	member, err := s.deps.Store.UpdateProjectMember(c.Request.Context(), claims.SpaceID, c.Param("projectID"), userID, input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "调整项目成员角色", member.Username+" · "+member.RoleName)
	c.JSON(http.StatusOK, gin.H{"member": member})
}

func (s *Server) removeProjectMember(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(strings.TrimSpace(c.Param("userID")), 10, 64)
	if err != nil || userID == 0 {
		writeError(c, http.StatusBadRequest, "invalid_request", "成员编号不正确")
		return
	}
	if userID == claims.UserID {
		writeError(c, http.StatusForbidden, "forbidden", "不能移除自己，请让其他项目维护者操作")
		return
	}
	if err := s.deps.Store.RemoveProjectMember(c.Request.Context(), claims.SpaceID, c.Param("projectID"), userID); err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "移除项目成员", strconv.FormatUint(userID, 10))
	c.JSON(http.StatusOK, gin.H{"removed": true, "user_id": userID})
}

func (s *Server) listProjectRoles(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	items, err := s.deps.Store.ListProjectRoles(c.Request.Context(), claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":        items,
		"total":        len(items),
		"permissions":  permissions.ProjectPermissionDefinitions(),
		"system_roles": permissions.ProjectRoleDefinitions(),
	})
}

func (s *Server) createProjectRole(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	var input store.CreateProjectRoleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "项目角色配置格式不正确")
		return
	}
	role, err := s.deps.Store.CreateProjectRole(c.Request.Context(), claims.SpaceID, claims.UserID, input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "创建项目自定义角色", role.Name)
	c.JSON(http.StatusCreated, gin.H{"role": role})
}

func (s *Server) updateProjectRole(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	var input store.UpdateProjectRoleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "项目角色配置格式不正确")
		return
	}
	role, err := s.deps.Store.UpdateProjectRole(c.Request.Context(), claims.SpaceID, c.Param("roleID"), input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "更新项目自定义角色", role.Name)
	c.JSON(http.StatusOK, gin.H{"role": role})
}

func (s *Server) deleteProjectRole(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	if err := s.deps.Store.DeleteProjectRole(c.Request.Context(), claims.SpaceID, c.Param("roleID")); err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "删除项目自定义角色", c.Param("roleID"))
	c.JSON(http.StatusOK, gin.H{"deleted": true, "role_id": c.Param("roleID")})
}
