package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/permissions"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func requirePermission(s store.Store, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := auth.ClaimsFrom(c)
		if !ok {
			writeError(c, http.StatusUnauthorized, "unauthorized", "请先登录")
			return
		}
		if claims.IsAdmin {
			c.Next()
			return
		}
		role, err := s.Role(c.Request.Context(), claims.UserID, claims.SpaceID)
		if err != nil {
			writeStoreError(c, err)
			return
		}
		if !permissions.Has(role, permission) {
			writeError(c, http.StatusForbidden, "forbidden", "当前空间没有执行此操作的权限")
			return
		}
		c.Next()
	}
}

func requireProjectPermission(s store.Store, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := auth.ClaimsFrom(c)
		if !ok {
			writeError(c, http.StatusUnauthorized, "unauthorized", "请先登录")
			return
		}
		projectID := c.Param("projectID")
		if projectID == "" {
			writeError(c, http.StatusBadRequest, "invalid_request", "项目编号不能为空")
			return
		}
		allowed, err := s.HasProjectPermission(c.Request.Context(), claims.UserID, claims.SpaceID, projectID, permission)
		if err != nil {
			writeStoreError(c, err)
			return
		}
		if !allowed {
			writeError(c, http.StatusForbidden, "project_forbidden", "当前账号没有该项目的操作权限")
			return
		}
		c.Next()
	}
}
