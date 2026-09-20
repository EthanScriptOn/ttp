package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

// imageRegistryCredentialPayload is encrypted as one value. Keeping the
// username and secret together means changing the auth mode can never leave
// an unencrypted credential column behind.
type imageRegistryCredentialPayload struct {
	Username string `json:"username,omitempty"`
	Secret   string `json:"secret"`
}

type imageRegistryConnectionRequest struct {
	Name       string `json:"name"`
	Registry   string `json:"registry"`
	AuthType   string `json:"auth_type"`
	Username   string `json:"username"`
	Secret     string `json:"secret"`
	Password   string `json:"password"`
	Token      string `json:"token"`
	Credential string `json:"credential"`
}

type imageRegistryConnectionPatchRequest struct {
	Name       *string `json:"name"`
	Registry   *string `json:"registry"`
	AuthType   *string `json:"auth_type"`
	Username   *string `json:"username"`
	Secret     *string `json:"secret"`
	Password   *string `json:"password"`
	Token      *string `json:"token"`
	Credential *string `json:"credential"`
}

func (r imageRegistryConnectionRequest) secret() string {
	if strings.TrimSpace(r.Secret) != "" {
		return r.Secret
	}
	if strings.TrimSpace(r.Password) != "" {
		return r.Password
	}
	if strings.TrimSpace(r.Token) != "" {
		return r.Token
	}
	return r.Credential
}

func (r imageRegistryConnectionPatchRequest) secret() (*string, bool) {
	if r.Secret != nil {
		return r.Secret, true
	}
	if r.Password != nil {
		return r.Password, true
	}
	if r.Token != nil {
		return r.Token, true
	}
	if r.Credential != nil {
		return r.Credential, true
	}
	return nil, false
}

func (s *Server) listImageRegistryConnections(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	items, err := s.deps.Store.ListImageRegistryConnections(c.Request.Context(), claims.SpaceID)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	for index := range items {
		items[index].CredentialCiphertext = ""
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (s *Server) createImageRegistryConnection(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	var request imageRegistryConnectionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "镜像仓库连接配置格式不正确")
		return
	}
	request.AuthType = normalizeRegistryAuthType(request.AuthType)
	secret := strings.TrimSpace(request.secret())
	if secret == "" {
		writeError(c, http.StatusBadRequest, "invalid_request", "请输入镜像仓库凭证")
		return
	}
	ciphertext, err := s.sealImageRegistryCredential(request.Username, secret)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "credential_storage_failed", "镜像仓库凭证保存失败")
		return
	}
	item, err := s.deps.Store.CreateImageRegistryConnection(c.Request.Context(), claims.SpaceID, store.CreateImageRegistryConnectionInput{
		Name: request.Name, Registry: request.Registry, AuthType: normalizeRegistryAuthType(request.AuthType),
		Username: request.Username, CredentialCiphertext: ciphertext,
	})
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "添加镜像仓库连接", item.Name)
	c.JSON(http.StatusCreated, gin.H{"connection": safeImageRegistryConnection(item)})
}

func (s *Server) updateImageRegistryConnection(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	current, err := s.deps.Store.GetImageRegistryConnection(c.Request.Context(), claims.SpaceID, c.Param("connectionID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	var request imageRegistryConnectionPatchRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "镜像仓库连接配置格式不正确")
		return
	}
	username := current.Username
	if request.Username != nil {
		username = *request.Username
	}
	secret, hasSecret := request.secret()
	credentialChanged := hasSecret || username != current.Username
	if request.AuthType != nil && normalizeRegistryAuthType(*request.AuthType) != current.AuthType {
		credentialChanged = true
	}
	if request.Registry != nil && strings.ToLower(strings.TrimSpace(*request.Registry)) != current.Registry {
		credentialChanged = true
	}
	if credentialChanged && !hasSecret {
		decoded, decodeErr := s.openImageRegistryCredential(current)
		if decodeErr != nil {
			writeError(c, http.StatusInternalServerError, "credential_storage_failed", "镜像仓库凭证不可用")
			return
		}
		secret = &decoded.Secret
	}
	input := store.UpdateImageRegistryConnectionInput{
		Name: request.Name, Registry: request.Registry, AuthType: normalizeRegistryAuthTypePointer(request.AuthType),
		Username: request.Username,
	}
	if credentialChanged {
		ciphertext, sealErr := s.sealImageRegistryCredential(username, strings.TrimSpace(valueOrString(secret)))
		if sealErr != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "镜像仓库凭证不能为空且不能包含控制字符")
			return
		}
		input.CredentialCiphertext = &ciphertext
	}
	item, err := s.deps.Store.UpdateImageRegistryConnection(c.Request.Context(), claims.SpaceID, c.Param("connectionID"), input)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "更新镜像仓库连接", item.Name)
	c.JSON(http.StatusOK, gin.H{"connection": safeImageRegistryConnection(item)})
}

func normalizeRegistryAuthType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "password" || value == "username_password" || value == "username-password" {
		return "basic"
	}
	return value
}

func normalizeRegistryAuthTypePointer(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := normalizeRegistryAuthType(*value)
	return &normalized
}

func (s *Server) testImageRegistryConnection(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	item, err := s.deps.Store.GetImageRegistryConnection(c.Request.Context(), claims.SpaceID, c.Param("connectionID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	credential, err := s.openImageRegistryCredential(item)
	if err != nil {
		s.markImageRegistryConnectionTest(c, item, false)
		s.recordAudit(c, "测试镜像仓库连接", item.Name)
		writeRegistryTestError(c, fmt.Errorf("镜像仓库凭证不可用"))
		return
	}
	testErr := checkImageRegistryConnection(c.Request.Context(), item, credential)
	updated := s.markImageRegistryConnectionTest(c, item, testErr == nil)
	s.recordAudit(c, "测试镜像仓库连接", item.Name)
	if testErr != nil {
		writeRegistryTestError(c, testErr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"connection": safeImageRegistryConnection(updated), "message": "连接成功"})
}

func (s *Server) deleteImageRegistryConnection(c *gin.Context) {
	claims, ok := s.requireSpace(c)
	if !ok {
		return
	}
	item, err := s.deps.Store.GetImageRegistryConnection(c.Request.Context(), claims.SpaceID, c.Param("connectionID"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	if err := s.deps.Store.DeleteImageRegistryConnection(c.Request.Context(), claims.SpaceID, item.ID); err != nil {
		writeStoreError(c, err)
		return
	}
	s.recordAudit(c, "删除镜像仓库连接", item.Name)
	c.JSON(http.StatusOK, gin.H{"deleted": true, "connection_id": item.ID})
}

func (s *Server) sealImageRegistryCredential(username, secret string) (string, error) {
	username = strings.TrimSpace(username)
	secret = strings.TrimSpace(secret)
	if secret == "" || strings.ContainsAny(username+secret, "\x00\r\n") || len(secret) > 4096 {
		return "", errors.New("invalid registry credential")
	}
	payload, err := json.Marshal(imageRegistryCredentialPayload{Username: username, Secret: secret})
	if err != nil {
		return "", err
	}
	return s.credentialCipher.seal(string(payload))
}

func (s *Server) openImageRegistryCredential(item domain.ImageRegistryConnection) (imageRegistryCredentialPayload, error) {
	plaintext, err := s.credentialCipher.open(item.CredentialCiphertext)
	if err != nil {
		return imageRegistryCredentialPayload{}, err
	}
	var payload imageRegistryCredentialPayload
	if err := json.Unmarshal([]byte(plaintext), &payload); err != nil || strings.TrimSpace(payload.Secret) == "" || strings.ContainsAny(payload.Username+payload.Secret, "\x00\r\n") {
		return imageRegistryCredentialPayload{}, errors.New("invalid registry credential")
	}
	if payload.Username == "" {
		payload.Username = item.Username
	}
	return payload, nil
}

func checkImageRegistryConnection(ctx context.Context, item domain.ImageRegistryConnection, credential imageRegistryCredentialPayload) error {
	host := strings.TrimSpace(item.Registry)
	if host == "" {
		return errors.New("镜像仓库地址无效")
	}
	scheme := "https"
	if isLocalRegistryHost(host) {
		scheme = "http"
	}
	requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, scheme+"://"+host+"/v2/", nil)
	if err != nil {
		return errors.New("镜像仓库地址无效")
	}
	if item.AuthType == "token" {
		req.Header.Set("Authorization", "Bearer "+credential.Secret)
	} else {
		req.SetBasicAuth(credential.Username, credential.Secret)
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("镜像仓库不可访问：%v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("镜像仓库拒绝了凭证（HTTP %d）", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("镜像仓库检查失败（HTTP %d）", response.StatusCode)
	}
	return nil
}

func isLocalRegistryHost(value string) bool {
	host := strings.ToLower(strings.TrimSpace(value))
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = strings.Trim(parsedHost, "[]")
	} else {
		host = strings.Trim(host, "[]")
	}
	if host == "localhost" || host == "docker.for.mac.localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) markImageRegistryConnectionTest(c *gin.Context, item domain.ImageRegistryConnection, success bool) domain.ImageRegistryConnection {
	now := time.Now().UTC()
	status := "invalid"
	if success {
		status = "active"
	}
	updated, err := s.deps.Store.UpdateImageRegistryConnection(c.Request.Context(), item.SpaceID, item.ID, store.UpdateImageRegistryConnectionInput{Status: &status, LastCheckedAt: &now})
	if err != nil {
		return item
	}
	return updated
}

func safeImageRegistryConnection(item domain.ImageRegistryConnection) domain.ImageRegistryConnection {
	item.CredentialCiphertext = ""
	return item
}

func writeRegistryTestError(c *gin.Context, err error) {
	message := "镜像仓库连接测试失败"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	writeError(c, http.StatusUnprocessableEntity, "registry_connection_test_failed", message)
}
