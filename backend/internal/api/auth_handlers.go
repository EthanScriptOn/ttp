package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	SpaceID  string `json:"space_id"`
}

func (s *Server) login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "用户名和密码格式不正确")
		return
	}
	user, err := s.deps.Store.Authenticate(c.Request.Context(), request.Username, request.Password)
	if err != nil {
		if err == store.ErrInvalidCredentials {
			writeError(c, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		} else {
			writeStoreError(c, err)
		}
		return
	}
	spaces, err := s.deps.Store.ListSpaces(c.Request.Context(), user.ID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	spaceID := strings.TrimSpace(request.SpaceID)
	if spaceID == "" && len(spaces) > 0 {
		spaceID = spaces[0].ID
	}
	role := ""
	if spaceID != "" {
		role, err = s.deps.Store.Role(c.Request.Context(), user.ID, spaceID)
		if err != nil {
			writeStoreError(c, err)
			return
		}
	}
	token, expires, err := s.deps.Auth.Issue(user, spaceID, role)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "token_error", "创建登录会话失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": expires, "user": userView(user), "spaces": spaces, "current_space_id": spaceID})
}

func (s *Server) me(c *gin.Context) {
	claims, ok := auth.ClaimsFrom(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	user, err := s.deps.Store.User(c.Request.Context(), claims.UserID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	spaces, err := s.deps.Store.ListSpaces(c.Request.Context(), user.ID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	var current *domain.Space
	if claims.SpaceID != "" {
		item, err := s.deps.Store.Space(c.Request.Context(), user.ID, claims.SpaceID)
		if err == nil {
			current = &item
		}
	}
	c.JSON(http.StatusOK, gin.H{"user": userView(user), "spaces": spaces, "current_space": current, "current_space_id": claims.SpaceID})
}

func (s *Server) selectSpace(c *gin.Context) {
	claims, ok := auth.ClaimsFrom(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var request struct {
		SpaceID string `json:"space_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.SpaceID) == "" {
		writeError(c, http.StatusBadRequest, "invalid_request", "请选择空间")
		return
	}
	spaceID := strings.TrimSpace(request.SpaceID)
	space, err := s.deps.Store.Space(c.Request.Context(), claims.UserID, spaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	user, err := s.deps.Store.User(c.Request.Context(), claims.UserID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	role, err := s.deps.Store.Role(c.Request.Context(), user.ID, spaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	token, expires, err := s.deps.Auth.Issue(user, spaceID, role)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "token_error", "切换空间失败")
		return
	}
	s.recordAuditForSpace(c, space.ID, claims.UserID, "切换空间", space.Name)
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": expires, "current_space": space})
}

func (s *Server) listSpaces(c *gin.Context) {
	claims, ok := auth.ClaimsFrom(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	items, err := s.deps.Store.ListSpaces(c.Request.Context(), claims.UserID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items), "current_space_id": claims.SpaceID})
}

func (s *Server) createSpace(c *gin.Context) {
	claims, ok := auth.ClaimsFrom(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var input store.CreateSpaceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "空间配置格式不正确")
		return
	}
	space, err := s.deps.Store.CreateSpace(c.Request.Context(), claims.UserID, input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAuditForSpace(c, space.ID, claims.UserID, "创建空间", space.Name)
	c.JSON(http.StatusCreated, space)
}

func userView(user domain.User) gin.H {
	return gin.H{"id": user.ID, "username": user.Username, "display_name": user.DisplayName, "is_super_admin": user.IsSuperAdmin}
}
