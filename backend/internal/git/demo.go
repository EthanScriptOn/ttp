package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

type demoRepository struct {
	repository Repository
	branches   map[string][]Commit
	tags       []Tag
}

// DemoProvider serves deterministic data and never reads credentials or
// connects to a Git server.
type DemoProvider struct {
	mu            sync.RWMutex
	repositories  map[string]demoRepository
	mergeSessions map[string]demoMergeSession
}

var _ BranchMerger = (*DemoProvider)(nil)

type demoMergeSession struct {
	request   MergeRequest
	conflicts []MergeConflict
}

func NewDemoProvider() *DemoProvider {
	base := time.Date(2026, time.January, 12, 8, 0, 0, 0, time.UTC)
	return NewDemoProviderWithRepositories([]DemoRepositoryInput{
		{
			Repository: Repository{ID: "demo-repo", Name: "Android Reverse Lab", URL: "https://example.invalid/android-reverse-lab", DefaultBranch: "main"},
			Branches: map[string][]Commit{
				"main": {
					newDemoCommit("a1b2c3d4e5f6", "Add runtime health probe", "demo", base.Add(2*time.Hour)),
					newDemoCommit("f6e5d4c3b2a1", "Document release workflow", "demo", base),
				},
				"release/2026.01": {
					newDemoCommit("112233445566", "Prepare January release", "demo", base.Add(3*time.Hour)),
				},
			},
			Tags: []Tag{
				{Name: "v1.0.0", SHA: "a1b2c3d4e5f6"},
				{Name: "release-2026.01", SHA: "112233445566"},
			},
		},
	})
}

type DemoRepositoryInput struct {
	Repository Repository
	Branches   map[string][]Commit
	Tags       []Tag
}

func NewDemoProviderWithRepositories(inputs []DemoRepositoryInput) *DemoProvider {
	provider := &DemoProvider{repositories: make(map[string]demoRepository, len(inputs)), mergeSessions: make(map[string]demoMergeSession)}
	for _, input := range inputs {
		branches := make(map[string][]Commit, len(input.Branches))
		for name, commits := range input.Branches {
			branches[name] = append([]Commit(nil), commits...)
		}
		provider.repositories[input.Repository.ID] = demoRepository{repository: input.Repository, branches: branches, tags: append([]Tag(nil), input.Tags...)}
	}
	return provider
}

// PrepareMerge creates an isolated, in-memory merge workspace. It mirrors the
// shape of the production operation while making it impossible for a demo run
// to modify a real Git server or the protected base branch.
func (p *DemoProvider) PrepareMerge(ctx context.Context, request MergeRequest) (MergeResult, error) {
	if err := ctx.Err(); err != nil {
		return MergeResult{}, err
	}
	request = normalizeMergeRequest(request)
	if request.RepositoryID == "" || request.SourceBranch == "" || request.BaseBranch == "" || request.SelectedSHA == "" {
		return MergeResult{}, ErrBranchNotFound
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	repository, ok := p.repositories[request.RepositoryID]
	if !ok {
		return MergeResult{}, ErrRepositoryNotFound
	}
	sourceCommits, sourceOK := repository.branches[request.SourceBranch]
	baseCommits, baseOK := repository.branches[request.BaseBranch]
	if !sourceOK || len(sourceCommits) == 0 {
		return MergeResult{}, ErrBranchNotFound
	}
	if !baseOK || len(baseCommits) == 0 {
		return MergeResult{}, ErrBranchNotFound
	}
	if !containsDemoCommit(sourceCommits, request.SelectedSHA) {
		return MergeResult{}, ErrCommitNotFound
	}
	if request.TemporaryBranch == "" {
		request.TemporaryBranch = "demo-release-prep-" + shortSHA(request.SelectedSHA)
	}
	if request.SourceBranch == request.BaseBranch || demoCommitSetContainsAll(sourceCommits, baseCommits) {
		return MergeResult{TemporaryBranch: request.TemporaryBranch, Status: "ready", HeadSHA: sourceCommits[0].SHA, Message: "演示模式：源分支已经包含基准分支，可以直接发布。"}, nil
	}

	// The fixture intentionally exposes one realistic file conflict when the
	// sample release branch is compared with main. This lets the browser flow be
	// exercised without fabricating a remote repository response at runtime.
	conflicts := []MergeConflict{{
		Path:            "deploy/application.yaml",
		BaseContent:     "replicas: 2\nimageTag: main\n",
		SourceContent:   "replicas: 1\nimageTag: release\n",
		ResolvedContent: "replicas: 2\nimageTag: release\n",
		Status:          "conflict",
	}}
	p.mergeSessions[mergeSessionKey(request)] = demoMergeSession{request: request, conflicts: cloneMergeConflicts(conflicts)}
	return MergeResult{
		TemporaryBranch: request.TemporaryBranch,
		Status:          "conflict",
		Conflicts:       conflicts,
		Message:         "演示模式：已创建临时发布分支，发现 1 个冲突文件；基准分支保持不变。",
	}, nil
}

// ResolveMerge completes a demo merge only after every reported file has a
// resolution. The resulting temporary branch is added to the in-memory
// fixture, so a subsequent refresh can show the prepared version.
func (p *DemoProvider) ResolveMerge(ctx context.Context, request MergeRequest, resolutions []MergeResolution) (MergeResult, error) {
	if err := ctx.Err(); err != nil {
		return MergeResult{}, err
	}
	request = normalizeMergeRequest(request)
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.mergeSessions[mergeSessionKey(request)]
	if !ok {
		return MergeResult{}, ErrBranchNotFound
	}
	resolved := make(map[string]string, len(resolutions))
	for _, item := range resolutions {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		resolved[path] = item.Content
	}
	conflicts := cloneMergeConflicts(session.conflicts)
	for index := range conflicts {
		content, exists := resolved[conflicts[index].Path]
		if !exists {
			return MergeResult{TemporaryBranch: request.TemporaryBranch, Status: "conflict", Conflicts: conflicts, Message: "还有冲突文件没有完成处理。"}, nil
		}
		conflicts[index].ResolvedContent = content
		conflicts[index].Status = "resolved"
	}

	hashInput := request.RepositoryID + "\x00" + request.SourceBranch + "\x00" + request.BaseBranch + "\x00" + request.SelectedSHA + "\x00" + request.TemporaryBranch
	for _, item := range conflicts {
		hashInput += "\x00" + item.Path + "\x00" + item.ResolvedContent
	}
	digest := sha256.Sum256([]byte(hashInput))
	mergeSHA := hex.EncodeToString(digest[:])
	commit := Commit{SHA: mergeSHA, ShortSHA: shortSHA(mergeSHA), Message: "Prepare release merge", Author: "demo", AuthoredAt: time.Now().UTC()}
	repository := p.repositories[request.RepositoryID]
	repository.branches[request.TemporaryBranch] = []Commit{commit}
	p.repositories[request.RepositoryID] = repository
	delete(p.mergeSessions, mergeSessionKey(request))
	return MergeResult{
		TemporaryBranch: request.TemporaryBranch,
		Status:          "ready",
		Conflicts:       conflicts,
		HeadSHA:         mergeSHA,
		Message:         "演示模式：冲突已解决，临时发布分支已准备好；main 未被修改，可以继续发布。",
	}, nil
}

func normalizeMergeRequest(request MergeRequest) MergeRequest {
	request.RepositoryID = strings.TrimSpace(request.RepositoryID)
	request.SourceBranch = strings.TrimSpace(request.SourceBranch)
	request.BaseBranch = strings.TrimSpace(request.BaseBranch)
	request.SelectedSHA = strings.TrimSpace(request.SelectedSHA)
	request.TemporaryBranch = strings.TrimSpace(request.TemporaryBranch)
	return request
}

func mergeSessionKey(request MergeRequest) string {
	return strings.Join([]string{request.RepositoryID, request.SourceBranch, request.BaseBranch, request.SelectedSHA, request.TemporaryBranch}, "\x00")
}

func containsDemoCommit(commits []Commit, sha string) bool {
	for _, commit := range commits {
		if strings.EqualFold(strings.TrimSpace(commit.SHA), strings.TrimSpace(sha)) {
			return true
		}
	}
	return false
}

func demoCommitSetContainsAll(source, base []Commit) bool {
	if len(base) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(source))
	for _, commit := range source {
		set[strings.ToLower(strings.TrimSpace(commit.SHA))] = struct{}{}
	}
	for _, commit := range base {
		if _, ok := set[strings.ToLower(strings.TrimSpace(commit.SHA))]; !ok {
			return false
		}
	}
	return true
}

func cloneMergeConflicts(source []MergeConflict) []MergeConflict {
	return append([]MergeConflict(nil), source...)
}

func newDemoCommit(sha, message, author string, authoredAt time.Time) Commit {
	short := sha
	if len(short) > 7 {
		short = short[:7]
	}
	return Commit{SHA: sha, ShortSHA: short, Message: message, Author: author, AuthoredAt: authoredAt}
}

func (p *DemoProvider) ListRepositories(ctx context.Context) ([]Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]Repository, 0, len(p.repositories))
	for _, repository := range p.repositories {
		result = append(result, repository.repository)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (p *DemoProvider) ListBranches(ctx context.Context, repositoryID string) ([]Branch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	repository, ok := p.repositories[repositoryID]
	if !ok {
		return nil, ErrRepositoryNotFound
	}
	branches := make([]Branch, 0, len(repository.branches))
	for name, commits := range repository.branches {
		if len(commits) == 0 {
			continue
		}
		branches = append(branches, Branch{Name: name, Head: commits[0], IsHead: name == repository.repository.DefaultBranch})
	}
	sort.Slice(branches, func(i, j int) bool { return branches[i].Name < branches[j].Name })
	return branches, nil
}

func (p *DemoProvider) ListCommits(ctx context.Context, repositoryID, branch string, limit int) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	repository, ok := p.repositories[repositoryID]
	if !ok {
		return nil, ErrRepositoryNotFound
	}
	commits, ok := repository.branches[branch]
	if !ok {
		return nil, ErrBranchNotFound
	}
	if limit <= 0 || limit > len(commits) {
		limit = len(commits)
	}
	return append([]Commit(nil), commits[:limit]...), nil
}

func (p *DemoProvider) ListTags(ctx context.Context, repositoryID string, limit int) ([]Tag, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	repository, ok := p.repositories[repositoryID]
	if !ok {
		return nil, ErrRepositoryNotFound
	}
	if limit <= 0 || limit > len(repository.tags) {
		limit = len(repository.tags)
	}
	return append([]Tag(nil), repository.tags[:limit]...), nil
}

func (p *DemoProvider) GetCommit(ctx context.Context, repositoryID, sha string) (Commit, error) {
	if err := ctx.Err(); err != nil {
		return Commit{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	repository, ok := p.repositories[repositoryID]
	if !ok {
		return Commit{}, ErrRepositoryNotFound
	}
	for _, commits := range repository.branches {
		for _, commit := range commits {
			if strings.EqualFold(commit.SHA, sha) {
				return commit, nil
			}
		}
	}
	return Commit{}, ErrCommitNotFound
}

// MergeBranch keeps the local fixture useful for exercising the same
// post-release policy as a remote provider. It creates a deterministic
// synthetic merge commit on the demo base branch and never touches a real
// repository.
func (p *DemoProvider) MergeBranch(ctx context.Context, repositoryID, sourceBranch, targetBranch string) (BranchMergeResult, error) {
	if err := ctx.Err(); err != nil {
		return BranchMergeResult{}, err
	}
	sourceBranch, targetBranch = strings.TrimSpace(sourceBranch), strings.TrimSpace(targetBranch)
	if sourceBranch == "" || targetBranch == "" {
		return BranchMergeResult{}, ErrBranchNotFound
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	repository, ok := p.repositories[repositoryID]
	if !ok {
		return BranchMergeResult{}, ErrRepositoryNotFound
	}
	source, sourceOK := repository.branches[sourceBranch]
	base, baseOK := repository.branches[targetBranch]
	if !sourceOK || len(source) == 0 || !baseOK || len(base) == 0 {
		return BranchMergeResult{}, ErrBranchNotFound
	}
	if sourceBranch == targetBranch || containsDemoCommit(base, source[0].SHA) {
		return BranchMergeResult{CommitSHA: base[0].SHA, Message: "演示模式：目标分支已经包含发布分支，跳过重复 merge。"}, nil
	}
	hashInput := repositoryID + "\x00" + sourceBranch + "\x00" + targetBranch + "\x00" + source[0].SHA
	sum := sha256.Sum256([]byte(hashInput))
	sha := hex.EncodeToString(sum[:])
	commit := Commit{SHA: sha, ShortSHA: shortSHA(sha), Message: "Merge " + sourceBranch + " into " + targetBranch, Author: "demo", AuthoredAt: time.Now().UTC()}
	repository.branches[targetBranch] = append([]Commit{commit}, base...)
	p.repositories[repositoryID] = repository
	return BranchMergeResult{CommitSHA: sha, Message: "演示模式：自动 merge 已完成。"}, nil
}
