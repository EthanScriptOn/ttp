package git

import (
	"context"
	"strings"
	"time"
)

var _ ServiceAccountProvider = (*DemoProvider)(nil)
var _ AccessChecker = (*DemoProvider)(nil)

// ServiceAccount exposes the virtual account used by the local demo. It
// keeps the demo release flow usable while retaining the same account gate as
// a real GitHub or GitLab installation.
func (p *DemoProvider) ServiceAccount() ServiceAccount {
	return ServiceAccount{
		Username:    "cicd-bot",
		DisplayName: "CI/CD 发布机器人",
		Provider:    "demo",
		AuthMethod:  "demo",
		Configured:  true,
	}
}

func (p *DemoProvider) CheckRepositoryAccess(ctx context.Context, repositoryID string) (RepositoryAccess, error) {
	if err := ctx.Err(); err != nil {
		return RepositoryAccess{}, err
	}
	repositoryID = strings.TrimSpace(repositoryID)
	report := RepositoryAccess{
		RepositoryID:            strings.TrimSpace(repositoryID),
		Provider:                "demo",
		Supported:               true,
		Account:                 p.ServiceAccount(),
		Authenticated:           true,
		AuthenticatedUsername:   "cicd-bot",
		AccountMatches:          true,
		Permission:              "Write",
		RequiredPermission:      "Write",
		RequiredMergePermission: "Write",
		CheckedAt:               time.Now().UTC(),
	}

	p.mu.RLock()
	repository, found := p.repositories[repositoryID]
	p.mu.RUnlock()
	if !found {
		report.Message = "演示仓库不存在，无法检查发布权限。"
		finalizeRepositoryAccess(&report)
		return report, nil
	}
	report.RepositoryFound = true
	report.RepositoryURL = repository.repository.URL
	report.CanRead = true
	report.CanWrite = true
	report.CanCreateTemporaryBranch = true
	report.CanMerge = true
	report.Message = "演示模式：平台 Git 服务账号授权已通过，可以创建临时发布分支并完成合并。"
	finalizeRepositoryAccess(&report)
	return report, nil
}
