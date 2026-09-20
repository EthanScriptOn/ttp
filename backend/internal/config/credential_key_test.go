package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadOrCreateCredentialKeyPersistsAcrossLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.key")

	first, err := readOrCreateCredentialKey(path, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := readOrCreateCredentialKey(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatal("credential key changed across loads")
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential key file permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadOrCreateCredentialKeyUsesExplicitKeyToInitializeDefaultFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CICD_GIT_CREDENTIAL_KEY", "configured-key")
	t.Setenv("CICD_GIT_CREDENTIAL_KEY_FILE", "")
	t.Setenv("TTP_PROJECT_ROOT", root)

	key, err := loadOrCreateCredentialKey("jwt-fallback")
	if err != nil {
		t.Fatal(err)
	}
	if key != "configured-key" {
		t.Fatal("explicit key was not selected")
	}
	data, err := os.ReadFile(filepath.Join(root, ".runtime", "credential.key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "configured-key\n" {
		t.Fatalf("default credential key file content = %q", string(data))
	}
}

func TestLoadOrCreateCredentialKeyUsesConfiguredFileBeforeExplicitKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.key")
	if err := os.WriteFile(path, []byte("file-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CICD_GIT_CREDENTIAL_KEY", "configured-key")
	t.Setenv("CICD_GIT_CREDENTIAL_KEY_FILE", path)

	key, err := loadOrCreateCredentialKey("jwt-fallback")
	if err != nil {
		t.Fatal(err)
	}
	if key != "file-key" {
		t.Fatalf("configured key file was not selected, got %q", key)
	}
}

func TestLoadOrCreateCredentialKeyInitializesConfiguredFileFromExplicitKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.key")
	t.Setenv("CICD_GIT_CREDENTIAL_KEY", "configured-key")
	t.Setenv("CICD_GIT_CREDENTIAL_KEY_FILE", path)

	key, err := loadOrCreateCredentialKey("jwt-fallback")
	if err != nil {
		t.Fatal(err)
	}
	if key != "configured-key" {
		t.Fatal("explicit key was not used to initialize the configured key file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "configured-key\n" {
		t.Fatalf("configured key file content = %q", string(data))
	}
}

func TestLoadInitializesPersistentCredentialKeyForProductionConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.key")
	t.Setenv("CICD_GIT_CREDENTIAL_KEY", "")
	t.Setenv("CICD_GIT_CREDENTIAL_KEY_FILE", path)
	t.Setenv("CICD_MYSQL_DSN", "app:password@tcp(127.0.0.1:3306)/cicd_platform")
	t.Setenv("CICD_JWT_SECRET", "jwt-secret")
	t.Setenv("CICD_KUBE_CLUSTER_ID", "local")

	loaded := Load()
	if loaded.GitCredentialKey == "" || loaded.credentialKeyError != nil {
		t.Fatalf("Load() did not initialize a key: key=%q err=%v", loaded.GitCredentialKey, loaded.credentialKeyError)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if loaded.GitCredentialKey != "jwt-secret" {
		t.Fatal("legacy JWT fallback was not persisted as the instance key")
	}
}

func TestReadOrCreateCredentialKeyUsesLegacyFallbackAndPersistsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.key")
	first, err := readOrCreateCredentialKey(path, "legacy-jwt-secret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := readOrCreateCredentialKey(path, "different-secret")
	if err != nil {
		t.Fatal(err)
	}
	if first != "legacy-jwt-secret" || second != first {
		t.Fatal("legacy fallback was not persisted")
	}
}

func TestLoadInitializesDefaultCredentialKeyRelativeToProjectRoot(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, "backend"),
		filepath.Join(root, "frontend"),
		filepath.Join(root, "deploy"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(root, "backend", "go.mod"):            "module example.test/backend\n",
		filepath.Join(root, "frontend", "package.json"):     "{}\n",
		filepath.Join(root, "deploy", "docker-compose.yml"): "services: {}\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(root, "backend")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWorkingDirectory)
	})
	t.Setenv("CICD_GIT_CREDENTIAL_KEY", "")
	t.Setenv("CICD_GIT_CREDENTIAL_KEY_FILE", "")
	t.Setenv("CICD_MYSQL_DSN", "app:password@tcp(127.0.0.1:3306)/cicd_platform")
	t.Setenv("CICD_JWT_SECRET", "jwt-secret")
	t.Setenv("CICD_KUBE_CLUSTER_ID", "local")
	t.Setenv("TTP_PROJECT_ROOT", "")

	loaded := Load()
	if loaded.GitCredentialKey == "" || loaded.credentialKeyError != nil {
		t.Fatalf("Load() did not initialize a key: key=%q err=%v", loaded.GitCredentialKey, loaded.credentialKeyError)
	}
	if _, err := os.Stat(filepath.Join(root, ".runtime", "credential.key")); err != nil {
		t.Fatalf("credential key was not created under project root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "backend", ".runtime", "credential.key")); !os.IsNotExist(err) {
		t.Fatalf("credential key was unexpectedly created under backend/: %v", err)
	}
}
