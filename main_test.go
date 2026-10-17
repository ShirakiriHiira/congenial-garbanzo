package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadToken(t *testing.T) {
	t.Run("prefers .env", func(t *testing.T) {
		t.Chdir(writeEnvFile(t, "from-dotenv"))
		t.Setenv(tokenEnv, "from-env")

		if got := loadToken(); got != "from-dotenv" {
			t.Fatalf("loadToken() = %q, want %q", got, "from-dotenv")
		}
	})

	t.Run("falls back to environment", func(t *testing.T) {
		t.Chdir(t.TempDir())
		t.Setenv(tokenEnv, "from-env")

		if got := loadToken(); got != "from-env" {
			t.Fatalf("loadToken() = %q, want %q", got, "from-env")
		}
	})

	t.Run("empty when unset", func(t *testing.T) {
		t.Chdir(t.TempDir())
		t.Setenv(tokenEnv, "")

		if got := loadToken(); got != "" {
			t.Fatalf("loadToken() = %q, want empty", got)
		}
	})
}

func writeEnvFile(t *testing.T, token string) string {
	t.Helper()

	dir := t.TempDir()
	content := tokenEnv + "=" + token + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	return dir
}
