package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

type credentialCipher struct {
	aead cipher.AEAD
}

func newCredentialCipher(explicitKey, fallbackKey string) *credentialCipher {
	seed := strings.TrimSpace(explicitKey)
	if seed == "" {
		seed = fallbackKey
	}
	key := sha256.Sum256([]byte("ttp-project-git-credential\x00" + seed))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return &credentialCipher{}
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return &credentialCipher{}
	}
	return &credentialCipher{aead: aead}
}

func (c *credentialCipher) seal(value string) (string, error) {
	if c == nil || c.aead == nil {
		return "", fmt.Errorf("credential encryption is unavailable")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := c.aead.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func (c *credentialCipher) open(value string) (string, error) {
	if c == nil || c.aead == nil {
		return "", fmt.Errorf("credential encryption is unavailable")
	}
	encoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(encoded) < c.aead.NonceSize() {
		return "", fmt.Errorf("credential ciphertext is invalid")
	}
	nonce, ciphertext := encoded[:c.aead.NonceSize()], encoded[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("credential ciphertext cannot be opened")
	}
	return string(plaintext), nil
}

type projectGitCredentialRequest struct {
	Provider string `json:"provider"`
	Username string `json:"username"`
	Token    string `json:"token"`
}

func (s *Server) getProjectGitCredential(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	credential, err := s.deps.Store.GetProjectGitCredential(c.Request.Context(), project.SpaceID, project.ID)
	if err != nil {
		if err == store.ErrNotFound {
			c.JSON(200, gin.H{"credential": gin.H{"project_id": project.ID, "configured": false}})
			return
		}
		writeStoreError(c, err)
		return
	}
	c.JSON(200, gin.H{"credential": safeProjectGitCredential(credential)})
}

func (s *Server) saveProjectGitCredential(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	var request projectGitCredentialRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, 400, "invalid_request", "仓库机器人配置格式不正确")
		return
	}
	credential, err := normalizeProjectGitCredential(request)
	if err != nil {
		writeError(c, 400, "invalid_request", err.Error())
		return
	}
	registry, supported := s.deps.Git.(git.RepositoryCredentialRegistry)
	if !supported {
		writeError(c, 501, "git_credential_unsupported", "当前 Git 连接不支持项目级仓库机器人")
		return
	}
	report, err := registry.CheckRepositoryCredential(c.Request.Context(), project.RepositoryID, project.RepositoryURL, credential)
	if err != nil {
		writeGitAccessError(c, err)
		return
	}
	if !report.Usable {
		c.JSON(422, gin.H{"error": gin.H{"code": "git_credential_invalid", "message": report.Message}, "access": report})
		return
	}
	ciphertext, err := s.credentialCipher.seal(credential.Token)
	if err != nil {
		writeError(c, 500, "credential_storage_failed", "仓库机器人凭证保存失败")
		return
	}
	saved, err := s.deps.Store.SaveProjectGitCredential(c.Request.Context(), project.SpaceID, project.ID, store.SaveProjectGitCredentialInput{Provider: credential.Provider, Username: credential.Username, TokenCiphertext: ciphertext})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if err := registry.ConfigureRepositoryCredential(project.RepositoryID, project.RepositoryURL, credential); err != nil {
		_ = s.deps.Store.DeleteProjectGitCredential(c.Request.Context(), project.SpaceID, project.ID)
		_ = registry.ClearRepositoryCredential(project.RepositoryID, project.RepositoryURL)
		writeProviderError(c, err)
		return
	}
	s.recordAudit(c, "配置项目 Git 机器人", project.Name+" · "+credential.Username)
	c.JSON(200, gin.H{"credential": safeProjectGitCredential(saved), "access": report})
}

func (s *Server) deleteProjectGitCredential(c *gin.Context) {
	project, ok := s.projectForRequest(c)
	if !ok {
		return
	}
	if registry, supported := s.deps.Git.(git.RepositoryCredentialRegistry); supported {
		if err := registry.ClearRepositoryCredential(project.RepositoryID, project.RepositoryURL); err != nil {
			writeProviderError(c, err)
			return
		}
	}
	if err := s.deps.Store.DeleteProjectGitCredential(c.Request.Context(), project.SpaceID, project.ID); err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "移除项目 Git 机器人", project.Name)
	c.JSON(200, gin.H{"credential": gin.H{"project_id": project.ID, "configured": false}})
}

func normalizeProjectGitCredential(request projectGitCredentialRequest) (git.RepositoryCredential, error) {
	provider := strings.ToLower(strings.TrimSpace(request.Provider))
	if provider == "" {
		provider = "auto"
	}
	if provider != "auto" && provider != "github" && provider != "gitlab" {
		return git.RepositoryCredential{}, fmt.Errorf("Git 平台必须是 GitHub 或 GitLab")
	}
	username := strings.TrimSpace(request.Username)
	if username == "" || len(username) > 120 || strings.ContainsAny(username, "\x00\r\n") {
		return git.RepositoryCredential{}, fmt.Errorf("请输入有效的机器人账号")
	}
	token := strings.TrimSpace(request.Token)
	if token == "" || len(token) > 4096 || strings.ContainsAny(token, "\x00\r\n") {
		return git.RepositoryCredential{}, fmt.Errorf("请输入有效的访问 Token")
	}
	return git.RepositoryCredential{Provider: provider, Username: username, Token: token}, nil
}

func safeProjectGitCredential(value store.ProjectGitCredential) gin.H {
	return gin.H{"project_id": value.ProjectID, "space_id": value.SpaceID, "provider": value.Provider, "username": value.Username, "configured": value.Configured, "updated_at": value.UpdatedAt.Format(time.RFC3339)}
}
