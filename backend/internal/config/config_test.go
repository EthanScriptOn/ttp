package config

import (
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		MySQLDSN:         "app:password@tcp(127.0.0.1:3306)/cicd_platform?parseTime=true",
		JWTSecret:        "a-secret-that-is-long-enough-for-tests",
		GitCredentialKey: "credential-key-that-is-long-enough-for-tests",
		RuntimeProvider:  "kubernetes",
		KubeClusterID:    "local",
	}
}

func TestValidateRequiresProductionDependencies(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "mysql dsn", mutate: func(config *Config) { config.MySQLDSN = "" }, want: "CICD_MYSQL_DSN"},
		{name: "jwt secret", mutate: func(config *Config) { config.JWTSecret = "" }, want: "CICD_JWT_SECRET"},
		{name: "kubernetes cluster", mutate: func(config *Config) { config.KubeClusterID = "" }, want: "CICD_KUBE_CLUSTER_ID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig()
			test.mutate(&config)
			if err := config.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsDemoConfiguration(t *testing.T) {
	t.Setenv("CICD_DEMO_MODE", "true")
	config := validConfig()
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "CICD_DEMO_MODE") {
		t.Fatalf("CICD_DEMO_MODE validation error = %v", err)
	}

	t.Setenv("CICD_DEMO_MODE", "")
	config = validConfig()
	config.RuntimeProvider = "demo"
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "CICD_RUNTIME_PROVIDER") {
		t.Fatalf("demo runtime validation error = %v", err)
	}
}

func TestLoadUsesNonDemoProductionDefaults(t *testing.T) {
	t.Setenv("CICD_DEMO_MODE", "")
	t.Setenv("CICD_RUNTIME_PROVIDER", "")
	config := Load()
	if config.RuntimeProvider != "kubernetes" {
		t.Fatalf("RuntimeProvider = %q, want kubernetes", config.RuntimeProvider)
	}
}
