package store

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

const (
	registryAuthBasic = "basic"
	registryAuthToken = "token"
)

func normalizeImageRegistryConnection(spaceID string, input CreateImageRegistryConnectionInput) (domain.ImageRegistryConnection, error) {
	if strings.TrimSpace(spaceID) == "" {
		return domain.ImageRegistryConnection{}, fmt.Errorf("%w: space id is required", ErrInvalidInput)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 120 || hasControl(name) {
		return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry connection name is invalid", ErrInvalidInput)
	}
	registry, err := normalizeRegistryHost(input.Registry)
	if err != nil {
		return domain.ImageRegistryConnection{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	authType := strings.ToLower(strings.TrimSpace(input.AuthType))
	if authType == "" {
		authType = registryAuthBasic
	}
	if authType != registryAuthBasic && authType != registryAuthToken {
		return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry auth type must be basic or token", ErrInvalidInput)
	}
	username := strings.TrimSpace(input.Username)
	if len(username) > 120 || hasControl(username) {
		return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry username is invalid", ErrInvalidInput)
	}
	if authType == registryAuthBasic && username == "" {
		return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry username is required for basic auth", ErrInvalidInput)
	}
	ciphertext := strings.TrimSpace(input.CredentialCiphertext)
	if ciphertext == "" {
		return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry credential is required", ErrInvalidInput)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "registry-" + uuid.NewString()[:8]
	}
	if !validRegistryConnectionID(id) {
		return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry connection id is invalid", ErrInvalidInput)
	}
	now := time.Now().UTC()
	return domain.ImageRegistryConnection{
		ID: id, SpaceID: spaceID, Name: name, Registry: registry, AuthType: authType,
		Username: username, PullSecretName: pullSecretName(id), Configured: true,
		Status: "unverified", CredentialCiphertext: ciphertext, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func updateImageRegistryConnection(current domain.ImageRegistryConnection, input UpdateImageRegistryConnectionInput) (domain.ImageRegistryConnection, error) {
	updated := current
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > 120 || hasControl(name) {
			return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry connection name is invalid", ErrInvalidInput)
		}
		updated.Name = name
	}
	if input.Registry != nil {
		registry, err := normalizeRegistryHost(*input.Registry)
		if err != nil {
			return domain.ImageRegistryConnection{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		updated.Registry = registry
	}
	if input.AuthType != nil {
		authType := strings.ToLower(strings.TrimSpace(*input.AuthType))
		if authType != registryAuthBasic && authType != registryAuthToken {
			return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry auth type must be basic or token", ErrInvalidInput)
		}
		updated.AuthType = authType
	}
	if input.Username != nil {
		username := strings.TrimSpace(*input.Username)
		if len(username) > 120 || hasControl(username) {
			return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry username is invalid", ErrInvalidInput)
		}
		updated.Username = username
	}
	if updated.AuthType == registryAuthBasic && updated.Username == "" {
		return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry username is required for basic auth", ErrInvalidInput)
	}
	if input.CredentialCiphertext != nil {
		ciphertext := strings.TrimSpace(*input.CredentialCiphertext)
		if ciphertext == "" {
			return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry credential cannot be empty", ErrInvalidInput)
		}
		updated.CredentialCiphertext = ciphertext
		updated.Configured = true
		updated.Status = "unverified"
	}
	if input.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*input.Status))
		if status != "unverified" && status != "active" && status != "invalid" {
			return domain.ImageRegistryConnection{}, fmt.Errorf("%w: registry connection status is invalid", ErrInvalidInput)
		}
		updated.Status = status
	}
	if input.LastCheckedAt != nil {
		value := input.LastCheckedAt.UTC()
		updated.LastCheckedAt = &value
	}
	updated.PullSecretName = pullSecretName(updated.ID)
	updated.UpdatedAt = time.Now().UTC()
	return updated, nil
}

func normalizeRegistryHost(value string) (string, error) {
	value = strings.TrimSpace(strings.TrimSuffix(value, "/"))
	if value == "" || len(value) > 255 || strings.ContainsAny(value, "\x00\r\n/@ ") {
		return "", fmt.Errorf("registry host is invalid")
	}
	parsed, err := url.Parse("https://" + value)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("registry host must be a hostname with optional port")
	}
	host := strings.ToLower(parsed.Hostname())
	if net.ParseIP(host) == nil && !strings.Contains(host, ".") && host != "localhost" {
		return "", fmt.Errorf("registry host must be a DNS name or IP")
	}
	if parsed.Port() != "" {
		port := parsed.Port()
		if parsed.Hostname() == "" || port == "" {
			return "", fmt.Errorf("registry port is invalid")
		}
	}
	if parsed.Port() != "" {
		return host + ":" + parsed.Port(), nil
	}
	return host, nil
}

func validRegistryConnectionID(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}

func pullSecretName(connectionID string) string {
	connectionID = strings.ToLower(strings.TrimSpace(connectionID))
	connectionID = strings.ReplaceAll(connectionID, "_", "-")
	if len(connectionID) > 45 {
		connectionID = connectionID[:45]
	}
	return "ttp-registry-" + connectionID
}
