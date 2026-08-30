package config

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr                   string
	JWTSecret              string
	JWTMinutes             int
	BootstrapAdminPassword string
	DemoAdminPassword      string
	MySQLDSN               string
	DemoMode               bool
	AllowedOrigin          string
	GitProvider            string
	GitToken               string
	GitServiceUsername     string
	GitServiceDisplayName  string
	GitServiceEmail        string
	GitAPIBaseURL          string
	GitHosts               []string
	GitTimeout             time.Duration
	RuntimeProvider        string
	KubeconfigPath         string
	KubeContext            string
	KubeClusterID          string
	KubeProjectLabel       string
	KubeInCluster          bool
	KubeTimeout            time.Duration
	KubeRolloutTimeout     time.Duration
}

func Load() Config {
	minutes := envInt("CICD_JWT_MINUTES", 720)
	demo := envBool("CICD_DEMO_MODE", false)
	gitTimeout := time.Duration(envInt("CICD_GIT_TIMEOUT_SECONDS", 15)) * time.Second
	kubeTimeout := time.Duration(envInt("CICD_KUBE_TIMEOUT_SECONDS", 15)) * time.Second
	kubeRolloutTimeout := time.Duration(envInt("CICD_KUBE_ROLLOUT_TIMEOUT_SECONDS", 300)) * time.Second
	jwtSecret := strings.TrimSpace(os.Getenv("CICD_JWT_SECRET"))
	if jwtSecret == "" {
		jwtSecret = ephemeralSecret()
	}
	return Config{
		Addr:                   env("CICD_ADDR", ":8790"),
		JWTSecret:              jwtSecret,
		JWTMinutes:             minutes,
		BootstrapAdminPassword: strings.TrimSpace(os.Getenv("CICD_BOOTSTRAP_ADMIN_PASSWORD")),
		DemoAdminPassword:      strings.TrimSpace(os.Getenv("CICD_DEMO_ADMIN_PASSWORD")),
		MySQLDSN:               strings.TrimSpace(os.Getenv("CICD_MYSQL_DSN")),
		DemoMode:               demo,
		AllowedOrigin:          env("CICD_ALLOWED_ORIGIN", "http://localhost:5173"),
		GitProvider:            env("CICD_GIT_PROVIDER", "auto"),
		GitToken:               strings.TrimSpace(os.Getenv("CICD_GIT_TOKEN")),
		GitServiceUsername:     env("CICD_GIT_SERVICE_USERNAME", "cicd-bot"),
		GitServiceDisplayName:  env("CICD_GIT_SERVICE_DISPLAY_NAME", "CI/CD 发布机器人"),
		GitServiceEmail:        strings.TrimSpace(os.Getenv("CICD_GIT_SERVICE_EMAIL")),
		GitAPIBaseURL:          strings.TrimSpace(os.Getenv("CICD_GIT_API_BASE_URL")),
		GitHosts:               envList("CICD_GIT_ALLOWED_HOSTS"),
		GitTimeout:             gitTimeout,
		RuntimeProvider:        env("CICD_RUNTIME_PROVIDER", "auto"),
		KubeconfigPath:         strings.TrimSpace(os.Getenv("CICD_KUBECONFIG")),
		KubeContext:            strings.TrimSpace(os.Getenv("CICD_KUBE_CONTEXT")),
		KubeClusterID:          env("CICD_KUBE_CLUSTER_ID", "default-cluster"),
		KubeProjectLabel:       env("CICD_KUBE_PROJECT_LABEL", "cicd.yuebuy.com/project"),
		KubeInCluster:          envBool("CICD_KUBE_IN_CLUSTER", false),
		KubeTimeout:            kubeTimeout,
		KubeRolloutTimeout:     kubeRolloutTimeout,
	}
}

func ephemeralSecret() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err == nil {
		return base64.RawURLEncoding.EncodeToString(bytes)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
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
