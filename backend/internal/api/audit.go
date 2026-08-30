package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

// listAuditLogs returns only entries from the space carried by the signed
// session. The store repeats that filter so a caller cannot widen the scope
// by changing a query parameter.
func (s *Server) listAuditLogs(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(c, http.StatusBadRequest, "invalid_request", "操作记录条数必须在 1 到 200 之间")
			return
		}
		limit = parsed
	}
	items, err := s.deps.Store.ListAuditLogs(c.Request.Context(), claims.SpaceID, limit)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	for index := range items {
		if items[index].UserID == 0 {
			continue
		}
		if user, userErr := s.deps.Store.User(c.Request.Context(), items[index].UserID); userErr == nil {
			items[index].UserName = user.DisplayName
			if items[index].UserName == "" {
				items[index].UserName = user.Username
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// recordAudit is intentionally best effort. An audit storage hiccup must not
// turn a successful deployment or configuration change into a false failure.
func (s *Server) recordAudit(c *gin.Context, action, target string) {
	claims, ok := auth.ClaimsFrom(c)
	if !ok || strings.TrimSpace(claims.SpaceID) == "" {
		return
	}
	s.recordAuditForSpace(c, claims.SpaceID, claims.UserID, action, target)
}

func (s *Server) recordAuditForSpace(c *gin.Context, spaceID string, userID uint64, action, target string) {
	if strings.TrimSpace(spaceID) == "" || strings.TrimSpace(action) == "" {
		return
	}
	_ = s.deps.Store.AppendAuditLog(c.Request.Context(), domain.AuditLog{
		SpaceID:   strings.TrimSpace(spaceID),
		UserID:    userID,
		Action:    strings.TrimSpace(action),
		Target:    strings.TrimSpace(target),
		CreatedAt: time.Now().UTC(),
	})
}
