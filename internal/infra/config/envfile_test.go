package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvSetsUnsetVariables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	key := "TEST_DOTENV_KEY_" + filepath.Base(dir)
	if err := os.WriteFile(path, []byte(key+"=from-file\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}

	if got := os.Getenv(key); got != "from-file" {
		t.Fatalf("%s = %q", key, got)
	}
}

func TestLoadDotEnvSkipsExistingVariables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	key := "TEST_DOTENV_EXISTING_" + filepath.Base(dir)

	if err := os.WriteFile(path, []byte(key+"=from-file\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv(key, "from-shell")

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}

	if got := os.Getenv(key); got != "from-shell" {
		t.Fatalf("%s = %q", key, got)
	}
}
