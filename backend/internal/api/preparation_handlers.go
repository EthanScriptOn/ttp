package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
)

type prepareReleaseRequest struct {
	Action          string                   `json:"action"`
	SourceBranch    string                   `json:"source_branch"`
	BaseBranch      string                   `json:"base_branch"`
	SelectedSHA     string                   `json:"selected_sha"`
	TemporaryBranch string                   `json:"temporary_branch"`
	Resolutions     []prepareMergeResolution `json:"resolutions"`
}

type prepareMergeResolution struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s *Server) prepareRelease(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	var request prepareReleaseRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "版本确认参数格式不正确")
		return
	}
	sourceBranch := strings.TrimSpace(request.SourceBranch)
	if sourceBranch == "" {
		sourceBranch = project.DefaultBranch
	}
	baseBranch := strings.TrimSpace(request.BaseBranch)
	if baseBranch == "" {
		baseBranch = project.DefaultBranch
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "merge" || action == "open" || action == "prepare_merge" || action == "resolve" || action == "continue" || action == "resolve_merge" {
		if err := git.RequireRepositoryMergeAccess(c.Request.Context(), s.deps.Git, project.RepositoryID); err != nil {
			writePreparationError(c, err)
			return
		}
	}
	input := release.CreateInput{
		ProjectID:    project.ID,
		RepositoryID: project.RepositoryID,
		Branch:       sourceBranch,
	}
	selectedSHA := strings.TrimSpace(request.SelectedSHA)
	var result release.Preparation
	var err error
	switch action {
	case "merge", "open", "prepare_merge":
		result, err = s.deps.Release.PrepareMerge(c.Request.Context(), input, baseBranch, selectedSHA, request.TemporaryBranch)
	case "resolve", "continue", "resolve_merge":
		resolutions := make([]git.MergeResolution, 0, len(request.Resolutions))
		for _, item := range request.Resolutions {
			resolutions = append(resolutions, git.MergeResolution{Path: item.Path, Content: item.Content})
		}
		result, err = s.deps.Release.ResolveMerge(c.Request.Context(), input, baseBranch, selectedSHA, request.TemporaryBranch, resolutions)
	default:
		result, err = s.deps.Release.Prepare(c.Request.Context(), input, baseBranch, selectedSHA)
	}
	if err != nil {
		writePreparationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"preparation": result})
}

func writePreparationError(c *gin.Context, err error) {
	if errors.Is(err, git.ErrGitWriteAccessDenied) {
		writeGitWriteAccessDenied(c, err)
		return
	}
	if errors.Is(err, git.ErrGitAccessCheckFailed) || errors.Is(err, git.ErrGitAccessCheckUnsupported) {
		writeGitAccessError(c, err)
		return
	}
	if errors.Is(err, git.ErrWriteUnsupported) {
		writeError(c, http.StatusNotImplemented, "write_unsupported", "当前 Git 连接只支持读取，暂时不能在平台内完成合并")
		return
	}
	writeReleaseError(c, err)
}
