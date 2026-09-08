package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
)

func (s *Server) gitRepositoryAccess(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	report, err := git.CheckRepositoryAccess(c.Request.Context(), s.deps.Git, project.RepositoryID)
	if err != nil {
		writeGitAccessError(c, err)
		return
	}
	if report.RepositoryURL == "" {
		report.RepositoryURL = project.RepositoryURL
	}
	c.JSON(http.StatusOK, gin.H{"access": report, "account": report.Account})
}

func writeGitAccessError(c *gin.Context, err error) {
	status, code, message := http.StatusBadGateway, "git_access_check_failed", "暂时无法检查项目仓库机器人，请稍后重试"
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusGatewayTimeout, "git_access_check_timeout", "检查项目仓库机器人超时，请稍后重试"
	case errors.Is(err, git.ErrInvalidProviderConfig):
		status, code, message = http.StatusBadRequest, "invalid_provider_config", "平台 Git 连接配置不正确，请检查服务端配置"
	case errors.Is(err, git.ErrRepositoryNotFound):
		status, code, message = http.StatusNotFound, "not_found", "找不到目标 Git 仓库"
	case errors.Is(err, git.ErrGitAccessCheckUnsupported):
		status, code, message = http.StatusNotImplemented, "git_access_check_unsupported", "当前 Git 连接不支持验证项目仓库机器人"
	}
	writeError(c, status, code, message)
}

func writeGitWriteAccessDenied(c *gin.Context, err error) {
	var denied *git.AccessDeniedError
	if errors.As(err, &denied) {
		message := denied.Reason
		if message == "" {
			message = "项目仓库机器人没有目标仓库的写权限"
		}
		writeError(c, http.StatusForbidden, "git_write_access_denied", message)
		return
	}
	writeError(c, http.StatusForbidden, "git_write_access_denied", "项目仓库机器人没有目标仓库的写权限")
}
