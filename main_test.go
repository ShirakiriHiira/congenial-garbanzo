package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestHealthcheck(t *testing.T) {
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ready.Close()

	if err := healthcheck(ready.Listener.Addr().String()); err != nil {
		t.Fatalf("healthcheck(ready) = %v, want nil", err)
	}

	_, port, err := net.SplitHostPort(ready.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	for _, wildcard := range []string{":" + port, "0.0.0.0:" + port, "[::]:" + port} {
		if err := healthcheck(wildcard); err != nil {
			t.Fatalf("healthcheck(%q) = %v, want nil", wildcard, err)
		}
	}

	unready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unready.Close()
	if err := healthcheck(unready.Listener.Addr().String()); err == nil {
		t.Fatal("healthcheck(unready) = nil, want error")
	}

	if err := healthcheck("not-an-address"); err == nil {
		t.Fatal("healthcheck(invalid) = nil, want error")
	}
}

func TestHealthcheckCommandNeedsNoTokenOrDatabase(t *testing.T) {
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ready.Close()

	_, port, err := net.SplitHostPort(ready.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	t.Setenv("HTTP_ADDR", "0.0.0.0:"+port)
	t.Setenv(tokenEnv, "")
	database := filepath.Join(t.TempDir(), "nested", "remind.db")
	t.Setenv("DATABASE_PATH", database)

	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"remind", "healthcheck"}

	if err := mainRun(); err != nil {
		t.Fatalf("mainRun(healthcheck) = %v, want nil", err)
	}
	if _, err := os.Stat(database); !os.IsNotExist(err) {
		t.Fatalf("healthcheck touched database path: %v", err)
	}

	t.Setenv("HTTP_ADDR", "127.0.0.1:1")
	if err := mainRun(); err == nil {
		t.Fatal("mainRun(healthcheck) = nil for unreachable readiness, want error")
	}
}

func TestMainRunReportsStartupCause(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(tokenEnv, "")

	err := mainRun()
	if err == nil {
		t.Fatal("mainRun() = nil, want startup error")
	}
	if !strings.Contains(err.Error(), tokenEnv) {
		t.Fatalf("mainRun() error = %q, want it to name %s", err, tokenEnv)
	}
}

func TestLoadTokenBlankDotenvFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(tokenEnv+"=\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Chdir(dir)
	t.Setenv(tokenEnv, "from-env")

	if got := loadToken(); got != "from-env" {
		t.Fatalf("loadToken() = %q, want %q", got, "from-env")
	}
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
