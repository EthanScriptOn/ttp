package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/release"
)

func filterReleases(items []release.Release, query string) []release.Release {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}
	filtered := make([]release.Release, 0, len(items))
	for _, item := range items {
		values := []string{item.ID, item.Branch, item.Name, item.CreatedByName, item.CreatedByUsername, string(item.Status)}
		for _, commit := range item.Commits {
			values = append(values, commit.SHA, commit.Message, commit.Author)
		}
		for _, target := range item.Targets {
			values = append(values, target.ID, target.Name, target.Environment, target.EnvironmentStage, target.ClusterID, target.Namespace, string(target.Status))
		}
		if strings.Contains(strings.ToLower(strings.Join(values, "\n")), query) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

const (
	defaultReleasePageSize = 10
	maxReleasePageSize     = 100
)

func releasePagination(c *gin.Context) (page, pageSize int, paginated, ok bool) {
	rawPage := strings.TrimSpace(c.Query("page"))
	rawPageSize := strings.TrimSpace(c.Query("page_size"))
	paginated = rawPage != "" || rawPageSize != ""
	page = 1
	pageSize = defaultReleasePageSize

	if rawPage != "" {
		parsed, err := strconv.Atoi(rawPage)
		if err != nil || parsed < 1 {
			writeError(c, http.StatusBadRequest, "invalid_request", "page 必须是大于 0 的整数")
			return 0, 0, paginated, false
		}
		page = parsed
	}
	if rawPageSize != "" {
		parsed, err := strconv.Atoi(rawPageSize)
		if err != nil || parsed < 1 || parsed > maxReleasePageSize {
			writeError(c, http.StatusBadRequest, "invalid_request", "page_size 必须在 1 到 100 之间")
			return 0, 0, paginated, false
		}
		pageSize = parsed
	}
	return page, pageSize, paginated, true
}
