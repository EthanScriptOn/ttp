package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const defaultCredentialKeyFile = ".runtime/credential.key"

// loadOrCreateCredentialKey returns the instance key used to encrypt project
// Git tokens. When a key file is configured, the file is authoritative: a
// shell-level CICD_GIT_CREDENTIAL_KEY must not accidentally override the
// persisted local key and make existing credentials unreadable. If the file is
// missing, an explicitly configured key initializes it (including the default
// project-root file). The JWT secret is used only as a migration fallback for
// older installations that relied on the previous implicit key.
func loadOrCreateCredentialKey(fallbackKey string) (string, error) {
	explicitKey := strings.TrimSpace(os.Getenv("CICD_GIT_CREDENTIAL_KEY"))
	configuredPath := strings.TrimSpace(os.Getenv("CICD_GIT_CREDENTIAL_KEY_FILE"))
	path, err := resolveCredentialKeyPath(configuredPath)
	if err != nil {
		return "", err
	}
	return readOrCreateCredentialKey(path, firstNonEmpty(explicitKey, fallbackKey))
}

func readOrCreateCredentialKey(path, fallbackKey string) (string, error) {
	read := func() (string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", fmt.Errorf("credential key file is empty")
		}
		return key, nil
	}

	if key, err := read(); err == nil {
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read credential key file: %w", err)
	}

	key := strings.TrimSpace(fallbackKey)
	if key == "" {
		keyBytes := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, keyBytes); err != nil {
			return "", fmt.Errorf("generate credential key: %w", err)
		}
		key = base64.RawURLEncoding.EncodeToString(keyBytes)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create credential key directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		// Another process may have initialized the file between the read and
		// create calls. Reuse that key rather than generating a second one.
		if errors.Is(err, os.ErrExist) {
			if existing, readErr := read(); readErr == nil {
				return existing, nil
			} else {
				return "", fmt.Errorf("read credential key file after concurrent initialization: %w", readErr)
			}
		}
		return "", fmt.Errorf("create credential key file: %w", err)
	}
	if _, err := file.WriteString(key + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write credential key file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close credential key file: %w", err)
	}
	return key, nil
}

func resolveCredentialKeyPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultCredentialKeyFile
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	root, err := findProjectRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, path), nil
}

func findProjectRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("TTP_PROJECT_ROOT")); root != "" {
		absoluteRoot, err := filepath.Abs(root)
		if err != nil {
			return "", fmt.Errorf("resolve TTP_PROJECT_ROOT: %w", err)
		}
		return absoluteRoot, nil
	}

	if root, ok := projectRootFromExecutable(); ok {
		return root, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	if root, ok := searchProjectRoot(cwd); ok {
		return root, nil
	}
	return cwd, nil
}

func projectRootFromExecutable() (string, bool) {
	executable, err := os.Executable()
	if err != nil {
		return "", false
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", false
	}
	binDir := filepath.Dir(executable)
	if filepath.Base(binDir) != "bin" {
		return "", false
	}
	runtimeDir := filepath.Dir(binDir)
	if filepath.Base(runtimeDir) != ".runtime" {
		return "", false
	}
	return filepath.Dir(runtimeDir), true
}

func searchProjectRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if looksLikeProjectRoot(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func looksLikeProjectRoot(dir string) bool {
	markers := []string{
		filepath.Join("backend", "go.mod"),
		filepath.Join("frontend", "package.json"),
		filepath.Join("deploy", "docker-compose.yml"),
	}
	for _, marker := range markers {
		if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
