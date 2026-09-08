package git

import (
	"context"
	"fmt"
	"strings"
)

// ServiceAccount returns only the configured identity. The token remains
// private to remoteProvider and is never part of this response.
func (p *GitHubProvider) ServiceAccount() ServiceAccount {
	if p == nil || p.remote == nil {
		return normalizeServiceAccount(ServiceAccount{Provider: "github"}, "github")
	}
	return p.remote.serviceAccountSnapshot("github")
}

// CheckRepositoryAccess verifies the platform token's GitHub identity and
// repository permission. A 401/403/404 is represented in the report so the
// caller can explain the fix without exposing the provider response body.
func (p *GitHubProvider) CheckRepositoryAccess(ctx context.Context, repositoryID string) (RepositoryAccess, error) {
	if err := p.ensure(); err != nil {
		return RepositoryAccess{}, err
	}
	if err := p.remote.checkRepository(repositoryID); err != nil {
		return RepositoryAccess{}, err
	}
	report := newRepositoryAccess(p.remote, "github")
	report.Account = p.ServiceAccount()
	if !report.Account.Configured {
		report.Message = "尚未配置项目仓库机器人，请在项目设置中填写机器人用户名和 Token。"
		finalizeRepositoryAccess(&report)
		return report, nil
	}

	var user gitHubAccessUserResponse
	_, err := p.remote.getJSON(ctx, "verify GitHub service account", p.remote.apiURL("user"), &user)
	if err != nil {
		if isAccessDeniedStatus(accessHTTPStatus(err)) {
			report.Message = "项目仓库机器人认证失败，请检查用户名和 Token 是否匹配。"
			finalizeRepositoryAccess(&report)
			return report, nil
		}
		return report, err
	}
	login := strings.TrimSpace(user.Login)
	if login == "" {
		return report, fmt.Errorf("verify GitHub service account: %w", ErrProviderResponse)
	}
	report.Authenticated = true
	report.AuthenticatedUsername = login
	report.AccountMatches = strings.EqualFold(login, report.Account.Username)
	if !report.AccountMatches {
		report.Message = fmt.Sprintf("当前 Token 属于 %s，但填写的机器人账号是 %s。", serviceAccountLabel(ServiceAccount{Username: login}), serviceAccountLabel(report.Account))
		finalizeRepositoryAccess(&report)
		return report, nil
	}

	var repository gitHubAccessRepositoryResponse
	_, err = p.remote.getJSON(ctx, "check GitHub repository permission", p.remote.apiURL(gitHubRepositorySegments(p.remote.repository)...), &repository)
	if err != nil {
		switch accessHTTPStatus(err) {
		case 404:
			report.Message = fmt.Sprintf("项目仓库机器人 %s 无法访问目标仓库，请先把它加入仓库。", serviceAccountLabel(report.Account))
		case 401, 403:
			report.Message = "项目仓库机器人已认证，但 GitHub 拒绝访问目标仓库，请检查仓库授权或组织 SSO。"
		default:
			return report, err
		}
		finalizeRepositoryAccess(&report)
		return report, nil
	}

	report.RepositoryFound = true
	report.CanRead = true
	report.Permission = githubPermission(repository.Permissions)
	report.CanWrite = repository.Permissions.Push || repository.Permissions.Maintain || repository.Permissions.Admin
	report.CanCreateTemporaryBranch = report.CanWrite
	// GitHub's repository permission does not reveal branch-protection rules;
	// conservatively require Maintain/Admin for an unconditional merge claim.
	report.CanMerge = repository.Permissions.Maintain || repository.Permissions.Admin
	if report.CanWrite {
		report.Message = fmt.Sprintf("授权已通过：项目仓库机器人 %s 对目标仓库具有 %s 权限，可以创建临时发布分支。", serviceAccountLabel(report.Account), report.Permission)
		if !report.CanMerge {
			report.Message += "受保护分支的合并可能还需要 Maintain 或 Admin。"
		}
	} else {
		report.Message = fmt.Sprintf("项目仓库机器人 %s 当前只有 %s 权限，GitHub 至少需要 Write 才能发布。", serviceAccountLabel(report.Account), report.Permission)
	}
	finalizeRepositoryAccess(&report)
	return report, nil
}

// ServiceAccount returns only the configured identity. The token remains
// private to remoteProvider and is never part of this response.
func (p *GitLabProvider) ServiceAccount() ServiceAccount {
	if p == nil || p.remote == nil {
		return normalizeServiceAccount(ServiceAccount{Provider: "gitlab"}, "gitlab")
	}
	return p.remote.serviceAccountSnapshot("gitlab")
}

// CheckRepositoryAccess verifies the platform token's GitLab identity and
// project access level. Developer (30) is the minimum write permission.
func (p *GitLabProvider) CheckRepositoryAccess(ctx context.Context, repositoryID string) (RepositoryAccess, error) {
	if err := p.ensure(); err != nil {
		return RepositoryAccess{}, err
	}
	if err := p.remote.checkRepository(repositoryID); err != nil {
		return RepositoryAccess{}, err
	}
	report := newRepositoryAccess(p.remote, "gitlab")
	report.Account = p.ServiceAccount()
	if !report.Account.Configured {
		report.Message = "尚未配置项目仓库机器人，请在项目设置中填写机器人用户名和 Token。"
		finalizeRepositoryAccess(&report)
		return report, nil
	}

	var user gitLabAccessUserResponse
	_, err := p.remote.getJSON(ctx, "verify GitLab service account", p.remote.apiURL("user"), &user)
	if err != nil {
		if isAccessDeniedStatus(accessHTTPStatus(err)) {
			report.Message = "项目仓库机器人认证失败，请检查用户名和 Token 是否匹配。"
			finalizeRepositoryAccess(&report)
			return report, nil
		}
		return report, err
	}
	username := strings.TrimSpace(user.Username)
	if username == "" {
		return report, fmt.Errorf("verify GitLab service account: %w", ErrProviderResponse)
	}
	report.Authenticated = true
	report.AuthenticatedUsername = username
	report.AccountMatches = strings.EqualFold(username, report.Account.Username)
	if !report.AccountMatches {
		report.Message = fmt.Sprintf("当前 Token 属于 %s，但填写的机器人账号是 %s。", serviceAccountLabel(ServiceAccount{Username: username}), serviceAccountLabel(report.Account))
		finalizeRepositoryAccess(&report)
		return report, nil
	}

	var repository gitLabAccessRepositoryResponse
	_, err = p.remote.getJSON(ctx, "check GitLab project permission", p.remote.apiURL("projects", p.remote.repository.projectPath), &repository)
	if err != nil {
		switch accessHTTPStatus(err) {
		case 404:
			report.Message = fmt.Sprintf("项目仓库机器人 %s 无法访问目标项目，请先把它加入项目。", serviceAccountLabel(report.Account))
		case 401, 403:
			report.Message = "项目仓库机器人已认证，但 GitLab 拒绝访问目标项目，请检查项目成员权限。"
		default:
			return report, err
		}
		finalizeRepositoryAccess(&report)
		return report, nil
	}

	level := repository.Permissions.accessLevel()
	report.RepositoryFound = true
	report.CanRead = level > 0
	report.Permission = gitLabPermission(level)
	report.CanWrite = level >= 30
	report.CanCreateTemporaryBranch = report.CanWrite
	// Protected-branch merge policies are controlled separately by GitLab;
	// Maintainer is the conservative unconditional merge capability.
	report.CanMerge = level >= 40
	if report.CanWrite {
		report.Message = fmt.Sprintf("授权已通过：项目仓库机器人 %s 对目标项目具有 %s 权限，可以创建临时发布分支。", serviceAccountLabel(report.Account), report.Permission)
		if !report.CanMerge {
			report.Message += "受保护分支的合并通常还需要 Maintainer。"
		}
	} else {
		report.Message = fmt.Sprintf("项目仓库机器人 %s 当前只有 %s 权限，GitLab 至少需要 Developer 才能发布。", serviceAccountLabel(report.Account), report.Permission)
	}
	finalizeRepositoryAccess(&report)
	return report, nil
}

func (p *remoteProvider) serviceAccountSnapshot(provider string) ServiceAccount {
	if p == nil {
		return normalizeServiceAccount(ServiceAccount{Provider: provider}, provider)
	}
	account := normalizeServiceAccount(p.serviceAccount, provider)
	account.Configured = strings.TrimSpace(p.token) != ""
	return account
}

func serviceAccountLabel(account ServiceAccount) string {
	username := strings.TrimSpace(account.Username)
	if username == "" {
		return "项目仓库机器人"
	}
	if strings.HasPrefix(username, "@") {
		return username
	}
	return "@" + username
}

type gitHubAccessUserResponse struct {
	Login string `json:"login"`
}

type gitHubAccessRepositoryResponse struct {
	Permissions gitHubRepositoryPermissions `json:"permissions"`
}

type gitHubRepositoryPermissions struct {
	Admin    bool `json:"admin"`
	Maintain bool `json:"maintain"`
	Push     bool `json:"push"`
	Triage   bool `json:"triage"`
	Pull     bool `json:"pull"`
}

func githubPermission(permissions gitHubRepositoryPermissions) string {
	switch {
	case permissions.Admin:
		return "Admin"
	case permissions.Maintain:
		return "Maintain"
	case permissions.Push:
		return "Write"
	case permissions.Triage:
		return "Triage"
	case permissions.Pull:
		return "Read"
	default:
		return "None"
	}
}

type gitLabAccessUserResponse struct {
	Username string `json:"username"`
}

type gitLabAccessRepositoryResponse struct {
	Permissions gitLabProjectPermissions `json:"permissions"`
}

type gitLabProjectPermissions struct {
	ProjectAccess gitLabAccessLevel `json:"project_access"`
	GroupAccess   gitLabAccessLevel `json:"group_access"`
}

type gitLabAccessLevel struct {
	AccessLevel int `json:"access_level"`
}

func (permissions gitLabProjectPermissions) accessLevel() int {
	if permissions.GroupAccess.AccessLevel > permissions.ProjectAccess.AccessLevel {
		return permissions.GroupAccess.AccessLevel
	}
	return permissions.ProjectAccess.AccessLevel
}

func gitLabPermission(level int) string {
	switch {
	case level >= 50:
		return "Owner"
	case level >= 40:
		return "Maintainer"
	case level >= 30:
		return "Developer"
	case level >= 20:
		return "Reporter"
	case level >= 10:
		return "Guest"
	default:
		return "None"
	}
}
