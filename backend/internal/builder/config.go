package builder

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type RuntimeConfig struct {
	Addr                    string
	Token                   string
	WorkDir                 string
	GitPath                 string
	BuildctlPath            string
	BuildkitAddr            string
	RegistryCredentialsFile string
	AllowedSourceHosts      []string
	ImageRepositoryPrefix   string
	RegistryCredentialRef   string
	Platforms               []string
	BuildTimeout            time.Duration
	MaxConcurrentBuilds     int
}

func LoadRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		Addr:                    env("TTP_BUILDER_ADDR", "127.0.0.1:8791"),
		Token:                   strings.TrimSpace(os.Getenv("TTP_BUILDER_TOKEN")),
		WorkDir:                 env("TTP_BUILDER_WORKDIR", "/var/lib/ttp-builder"),
		GitPath:                 env("TTP_BUILDER_GIT", "git"),
		BuildctlPath:            env("TTP_BUILDER_BUILDCTL", "buildctl"),
		BuildkitAddr:            strings.TrimSpace(os.Getenv("TTP_BUILDER_BUILDKIT_ADDR")),
		RegistryCredentialsFile: strings.TrimSpace(os.Getenv("TTP_BUILDER_REGISTRY_CREDENTIALS_FILE")),
		AllowedSourceHosts:      envList("TTP_BUILDER_ALLOWED_GIT_HOSTS"),
		ImageRepositoryPrefix:   strings.TrimSpace(os.Getenv("TTP_BUILDER_IMAGE_REPOSITORY_PREFIX")),
		RegistryCredentialRef:   strings.TrimSpace(os.Getenv("TTP_BUILDER_REGISTRY_CREDENTIAL_REF")),
		Platforms:               envList("TTP_BUILDER_IMAGE_PLATFORMS"),
		BuildTimeout:            time.Duration(envInt("TTP_BUILDER_TIMEOUT_SECONDS", 900)) * time.Second,
		MaxConcurrentBuilds:     envInt("TTP_BUILDER_MAX_CONCURRENT_BUILDS", 1),
	}
}

func (c RuntimeConfig) Executor() (*BuildKitExecutor, error) {
	return NewBuildKitExecutor(BuildKitConfig{
		WorkDir: c.WorkDir, GitPath: c.GitPath, BuildctlPath: c.BuildctlPath, BuildkitAddr: c.BuildkitAddr,
		RegistryCredentialsFile: c.RegistryCredentialsFile, AllowedSourceHosts: c.AllowedSourceHosts,
		ImageRepositoryPrefix: c.ImageRepositoryPrefix, RegistryCredentialRef: c.RegistryCredentialRef, Platforms: c.Platforms, Timeout: c.BuildTimeout,
	})
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envList(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
