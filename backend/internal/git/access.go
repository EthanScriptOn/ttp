package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrGitWriteAccessDenied is returned when the platform account cannot
	// perform a Git operation that changes repository refs.
	ErrGitWriteAccessDenied = errors.New("git service account does not have write access")
	// ErrGitAccessCheckFailed indicates that the platform account could not be
	// verified because the provider or network was unavailable. It is distinct
	// from ErrGitWriteAccessDenied so callers do not tell users to change repo
	// permissions when the real problem is a transient check failure.
	ErrGitAccessCheckFailed = errors.New("git service account access check failed")
	// ErrGitAccessCheckUnsupported indicates that a provider cannot verify the
	// identity and repository permissions of its configured account.
	ErrGitAccessCheckUnsupported = errors.New("git service account access checks are not supported")
)

// ServiceAccount is the public identity of the account configured for Git
// operations. Credentials are intentionally not represented here.
type ServiceAccount struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Provider    string `json:"provider"`
	AuthMethod  string `json:"auth_method"`
	Configured  bool   `json:"configured"`
}

// ServiceAccountProvider exposes only the non-secret identity used by a Git
// provider. Implementations must never return a token, password, or private
// key through this interface.
type ServiceAccountProvider interface {
	ServiceAccount() ServiceAccount
}

// RepositoryAccess is a safe-to-display result of checking the configured
// service account against one repository. The capability flags describe what
// the provider reports; protected-branch rules can still impose a stricter
// merge policy.
type RepositoryAccess struct {
	RepositoryID             string         `json:"repository_id"`
	RepositoryURL            string         `json:"repository_url"`
	Provider                 string         `json:"provider"`
	Supported                bool           `json:"supported"`
	Account                  ServiceAccount `json:"account"`
	Authenticated            bool           `json:"authenticated"`
	AuthenticatedUsername    string         `json:"authenticated_username,omitempty"`
	RepositoryFound          bool           `json:"repository_found"`
	AccountMatches           bool           `json:"account_matches"`
	Permission               string         `json:"permission"`
	CanRead                  bool           `json:"can_read"`
	CanWrite                 bool           `json:"can_write"`
	CanCreateTemporaryBranch bool           `json:"can_create_temporary_branch"`
	CanMerge                 bool           `json:"can_merge"`
	Usable                   bool           `json:"usable"`
	RequiredPermission       string         `json:"required_permission"`
	RequiredMergePermission  string         `json:"required_merge_permission"`
	Message                  string         `json:"message"`
	CheckedAt                time.Time      `json:"checked_at"`
}

// AccessChecker is optional so custom providers can adopt the check without
// breaking the existing read-only Provider contract.
type AccessChecker interface {
	CheckRepositoryAccess(ctx context.Context, repositoryID string) (RepositoryAccess, error)
}

// AccessDeniedError is returned by RequireRepositoryWriteAccess. Its report
// is safe to pass to an API response and deliberately contains no credential.
type AccessDeniedError struct {
	Report RepositoryAccess
	Reason string
}

func (e *AccessDeniedError) Error() string {
	if e == nil || strings.TrimSpace(e.Reason) == "" {
		return ErrGitWriteAccessDenied.Error()
	}
	return fmt.Sprintf("%s: %s", ErrGitWriteAccessDenied, strings.TrimSpace(e.Reason))
}

func (e *AccessDeniedError) Unwrap() error { return ErrGitWriteAccessDenied }

// AccessCheckError preserves the underlying cancellation/timeout signal for
// HTTP status mapping while exposing a stable sentinel to API callers. The
// provider error is never returned directly to the browser.
type AccessCheckError struct {
	Report RepositoryAccess
	Cause  error
}

func (e *AccessCheckError) Error() string { return ErrGitAccessCheckFailed.Error() }

func (e *AccessCheckError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *AccessCheckError) Is(target error) bool {
	if target == ErrGitAccessCheckFailed {
		return true
	}
	return e != nil && errors.Is(e.Cause, target)
}

// ServiceAccountFor returns the configured public identity without exposing
// provider credentials. Providers that do not implement the optional
// interface are represented as an unconfigured account.
func ServiceAccountFor(provider Provider) ServiceAccount {
	if accountProvider, ok := provider.(ServiceAccountProvider); ok {
		return normalizeServiceAccount(accountProvider.ServiceAccount(), "")
	}
	return ServiceAccount{
		DisplayName: "项目仓库机器人",
		Provider:    "unknown",
		AuthMethod:  "unknown",
		Configured:  false,
	}
}

// CheckRepositoryAccess asks the configured provider to verify its own
// service account. A report is returned for expected authentication or
// permission failures so the UI can explain how to fix the repository; an
// error is reserved for transport and malformed-provider failures.
func CheckRepositoryAccess(ctx context.Context, provider Provider, repositoryID string) (RepositoryAccess, error) {
	if provider == nil {
		return RepositoryAccess{}, fmt.Errorf("%w: git provider is not configured", ErrGitAccessCheckUnsupported)
	}
	checker, ok := provider.(AccessChecker)
	if !ok {
		report := RepositoryAccess{
			RepositoryID: repositoryID,
			Account:      ServiceAccountFor(provider),
			Supported:    false,
			Message:      "当前 Git 连接不支持验证项目仓库机器人，发布操作已被阻止。",
			CheckedAt:    time.Now().UTC(),
		}
		return report, nil
	}
	report, err := checker.CheckRepositoryAccess(ctx, repositoryID)
	if err != nil {
		return report, err
	}
	finalizeRepositoryAccess(&report)
	return report, nil
}

// RequireRepositoryWriteAccess is the server-side gate for every Git write
// operation. It intentionally checks the platform account, never the logged
// in user's personal Git credentials.
func RequireRepositoryWriteAccess(ctx context.Context, provider Provider, repositoryID string) error {
	report, err := CheckRepositoryAccess(ctx, provider, repositoryID)
	if err != nil {
		return &AccessCheckError{
			Report: report,
			Cause:  err,
		}
	}
	if report.Usable && report.CanWrite {
		return nil
	}
	reason := strings.TrimSpace(report.Message)
	if reason == "" {
		reason = "项目仓库机器人没有目标仓库的写权限"
	}
	return &AccessDeniedError{Report: report, Reason: reason}
}

// RequireRepositoryMergeAccess is the stricter gate used by the isolated
// branch preparation flow. Write access alone is not enough to claim that the
// configured service account can merge into a protected base branch.
func RequireRepositoryMergeAccess(ctx context.Context, provider Provider, repositoryID string) error {
	report, err := CheckRepositoryAccess(ctx, provider, repositoryID)
	if err != nil {
		return &AccessCheckError{
			Report: report,
			Cause:  err,
		}
	}

	reason := strings.TrimSpace(report.Message)
	switch {
	case !report.Supported:
		reason = "当前 Git 连接不支持验证项目仓库机器人的合并权限"
	case !report.Authenticated:
		reason = "项目仓库机器人认证未通过，无法执行合并"
	case !report.RepositoryFound:
		reason = "项目仓库机器人无法访问目标仓库，无法执行合并"
	case !report.AccountMatches:
		reason = "当前 Token 与填写的项目仓库机器人不一致，无法执行合并"
	case !report.CanWrite:
		reason = "项目仓库机器人没有目标仓库的写权限，无法执行合并"
	case !report.CanCreateTemporaryBranch:
		reason = "项目仓库机器人没有创建临时发布分支的权限，无法执行合并"
	case !report.CanMerge:
		required := strings.TrimSpace(report.RequiredMergePermission)
		if required == "" {
			required = "目标仓库要求的合并权限"
		}
		reason = fmt.Sprintf("项目仓库机器人没有合并权限，至少需要 %s", required)
	default:
		return nil
	}
	if reason == "" {
		reason = "项目仓库机器人不满足合并发布要求"
	}
	return &AccessDeniedError{Report: report, Reason: reason}
}

func defaultServiceAccount(kind remoteKind) ServiceAccount {
	provider := "github"
	if kind == remoteGitLab {
		provider = "gitlab"
	}
	return ServiceAccount{
		DisplayName: "项目仓库机器人",
		Provider:    provider,
		AuthMethod:  "token",
		Configured:  false,
	}
}

func normalizeServiceAccount(account ServiceAccount, provider string) ServiceAccount {
	account.Username = strings.TrimSpace(account.Username)
	account.DisplayName = strings.TrimSpace(account.DisplayName)
	if account.DisplayName == "" {
		account.DisplayName = "项目仓库机器人"
	}
	account.Email = strings.TrimSpace(account.Email)
	if strings.TrimSpace(provider) != "" {
		account.Provider = strings.ToLower(strings.TrimSpace(provider))
	} else {
		account.Provider = strings.ToLower(strings.TrimSpace(account.Provider))
	}
	account.AuthMethod = strings.ToLower(strings.TrimSpace(account.AuthMethod))
	if account.AuthMethod == "" {
		account.AuthMethod = "token"
	}
	return account
}

func newRepositoryAccess(remote *remoteProvider, provider string) RepositoryAccess {
	account := ServiceAccount{DisplayName: "项目仓库机器人", Provider: provider, AuthMethod: "token"}
	if remote != nil {
		account = normalizeServiceAccount(remote.serviceAccount, provider)
		account.Configured = strings.TrimSpace(remote.token) != ""
	}
	requiredPermission := "Write"
	requiredMergePermission := "Write（受保护分支可能需要更高权限）"
	if provider == "gitlab" {
		requiredPermission = "Developer"
		requiredMergePermission = "Developer（受保护分支通常需要 Maintainer）"
	}
	repository := Repository{}
	if remote != nil {
		repository = remote.repositorySnapshot()
	}
	return RepositoryAccess{
		RepositoryID:            repository.ID,
		RepositoryURL:           repository.URL,
		Provider:                provider,
		Supported:               true,
		Account:                 account,
		AccountMatches:          false,
		Permission:              "none",
		RequiredPermission:      requiredPermission,
		RequiredMergePermission: requiredMergePermission,
		CheckedAt:               time.Now().UTC(),
	}
}

func accessHTTPStatus(err error) int {
	var statusErr *ProviderHTTPError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode
	}
	return 0
}

func isAccessDeniedStatus(status int) bool {
	return status == 401 || status == 403 || status == 404
}

func finalizeRepositoryAccess(report *RepositoryAccess) {
	if report == nil {
		return
	}
	report.Usable = report.Supported &&
		report.Authenticated &&
		report.RepositoryFound &&
		report.AccountMatches &&
		report.CanWrite &&
		report.CanCreateTemporaryBranch
	if report.CheckedAt.IsZero() {
		report.CheckedAt = time.Now().UTC()
	}
}
