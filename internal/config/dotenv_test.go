package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvSetsValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "MAX_BOT_TOKEN=dotenv_token\nMAX_RUN_MODE=webhook\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("MAX_BOT_TOKEN", "")
	t.Setenv("MAX_RUN_MODE", "")

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}

	if got := os.Getenv("MAX_BOT_TOKEN"); got != "dotenv_token" {
		t.Fatalf("MAX_BOT_TOKEN = %q, want %q", got, "dotenv_token")
	}
	if got := os.Getenv("MAX_RUN_MODE"); got != "webhook" {
		t.Fatalf("MAX_RUN_MODE = %q, want %q", got, "webhook")
	}
}

func TestLoadDotEnvDoesNotOverrideExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "MAX_BOT_TOKEN=dotenv_token\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("MAX_BOT_TOKEN", "existing_token")

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}

	if got := os.Getenv("MAX_BOT_TOKEN"); got != "existing_token" {
		t.Fatalf("MAX_BOT_TOKEN = %q, want %q", got, "existing_token")
	}
}

func TestLoadDotEnvMissingFileNoError(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}
}

