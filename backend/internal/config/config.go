package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr                        string
	JWTSecret                   string
	JWTMinutes                  int
	MySQLDSN                    string
	AllowedOrigin               string
	GitProvider                 string
	GitCredentialKey            string
	GitAPIBaseURL               string
	GitHosts                    []string
	GitTimeout                  time.Duration
	RuntimeProvider             string
	KubeconfigPath              string
	KubeContext                 string
	KubeClusterID               string
	KubeProjectLabel            string
	KubeServiceAccountNamespace string
	KubeServiceAccountName      string
	KubeInCluster               bool
	KubeTimeout                 time.Duration
	KubeRolloutTimeout          time.Duration
	ImageBuilderURL             string
	ImageBuilderToken           string
	ImageBuilderTimeout         time.Duration
	credentialKeyError          error
}

func Load() Config {
	minutes := envInt("CICD_JWT_MINUTES", 720)
	gitTimeout := time.Duration(envInt("CICD_GIT_TIMEOUT_SECONDS", 15)) * time.Second
	kubeTimeout := time.Duration(envInt("CICD_KUBE_TIMEOUT_SECONDS", 15)) * time.Second
	kubeRolloutTimeout := time.Duration(envInt("CICD_KUBE_ROLLOUT_TIMEOUT_SECONDS", 300)) * time.Second
	imageBuilderTimeout := time.Duration(envInt("CICD_IMAGE_BUILDER_TIMEOUT_SECONDS", 900)) * time.Second
	jwtSecret := strings.TrimSpace(os.Getenv("CICD_JWT_SECRET"))
	mysqlDSN := strings.TrimSpace(os.Getenv("CICD_MYSQL_DSN"))
	gitCredentialKey := ""
	var credentialKeyError error
	// Avoid creating local state for incomplete configurations (for example
	// package tests). A real server has both of these values before it opens
	// the database and can safely initialize its persistent key here.
	if jwtSecret != "" && mysqlDSN != "" {
		gitCredentialKey, credentialKeyError = loadOrCreateCredentialKey(jwtSecret)
	} else {
		gitCredentialKey = strings.TrimSpace(os.Getenv("CICD_GIT_CREDENTIAL_KEY"))
	}
	return Config{
		Addr:                        env("CICD_ADDR", ":8790"),
		JWTSecret:                   jwtSecret,
		JWTMinutes:                  minutes,
		MySQLDSN:                    mysqlDSN,
		AllowedOrigin:               env("CICD_ALLOWED_ORIGIN", "http://localhost:5173"),
		GitProvider:                 env("CICD_GIT_PROVIDER", "auto"),
		GitCredentialKey:            gitCredentialKey,
		GitAPIBaseURL:               strings.TrimSpace(os.Getenv("CICD_GIT_API_BASE_URL")),
		GitHosts:                    envList("CICD_GIT_ALLOWED_HOSTS"),
		GitTimeout:                  gitTimeout,
		RuntimeProvider:             env("CICD_RUNTIME_PROVIDER", "kubernetes"),
		KubeconfigPath:              strings.TrimSpace(os.Getenv("CICD_KUBECONFIG")),
		KubeContext:                 strings.TrimSpace(os.Getenv("CICD_KUBE_CONTEXT")),
		KubeClusterID:               strings.TrimSpace(os.Getenv("CICD_KUBE_CLUSTER_ID")),
		KubeProjectLabel:            env("CICD_KUBE_PROJECT_LABEL", "cicd.yuebuy.com/project"),
		KubeServiceAccountNamespace: env("CICD_KUBE_SERVICE_ACCOUNT_NAMESPACE", "ttp-system"),
		KubeServiceAccountName:      env("CICD_KUBE_SERVICE_ACCOUNT_NAME", "ttp-runtime"),
		KubeInCluster:               envBool("CICD_KUBE_IN_CLUSTER", false),
		KubeTimeout:                 kubeTimeout,
		KubeRolloutTimeout:          kubeRolloutTimeout,
		ImageBuilderURL:             strings.TrimSpace(os.Getenv("CICD_IMAGE_BUILDER_URL")),
		ImageBuilderToken:           strings.TrimSpace(os.Getenv("CICD_IMAGE_BUILDER_TOKEN")),
		ImageBuilderTimeout:         imageBuilderTimeout,
		credentialKeyError:          credentialKeyError,
	}
}

// Validate rejects incomplete production configuration before the HTTP
// listener is opened. Missing dependencies must be startup errors, never a
// reason to switch to volatile or fabricated data.
func (c Config) Validate() error {
	if strings.TrimSpace(c.MySQLDSN) == "" {
		return fmt.Errorf("CICD_MYSQL_DSN is required")
	}
	if strings.TrimSpace(c.JWTSecret) == "" {
		return fmt.Errorf("CICD_JWT_SECRET is required")
	}
	if c.credentialKeyError != nil {
		return fmt.Errorf("CICD_GIT_CREDENTIAL_KEY could not be initialized: %w", c.credentialKeyError)
	}
	if strings.TrimSpace(os.Getenv("CICD_DEMO_MODE")) != "" {
		return fmt.Errorf("CICD_DEMO_MODE is no longer supported")
	}
	if strings.EqualFold(strings.TrimSpace(c.RuntimeProvider), "demo") {
		return fmt.Errorf("CICD_RUNTIME_PROVIDER=demo is no longer supported")
	}
	if strings.TrimSpace(c.KubeClusterID) == "" {
		return fmt.Errorf("CICD_KUBE_CLUSTER_ID is required")
	}
	if strings.TrimSpace(c.ImageBuilderURL) != "" || strings.TrimSpace(c.ImageBuilderToken) != "" {
		if strings.TrimSpace(c.ImageBuilderURL) == "" || strings.TrimSpace(c.ImageBuilderToken) == "" {
			return fmt.Errorf("CICD_IMAGE_BUILDER_URL and CICD_IMAGE_BUILDER_TOKEN must be set together")
		}
	}
	return nil
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

func envBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	values := strings.Split(raw, ",")
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
