package config

import (
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

var configurationKeys = []string{
	"DATABASE_PATH", "APP_TIMEZONE", "REMINDER_SCAN_INTERVAL",
	"REMINDER_CATCHUP_WINDOW", "WEBAPP_AUTH_MAX_AGE",
	"ALLOWED_TELEGRAM_USER_IDS", "HTTP_ADDR", "LOG_LEVEL", "TELEGRAM_API_PROXY", "MINIAPP_PUBLIC_URL",
}

func cleanEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range configurationKeys {
		// Setenv registers restoration, including whether the original was unset.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ALLOWED_TELEGRAM_USER_IDS", "10001")
}

func TestLoadDefaults(t *testing.T) {
	cleanEnvironment(t)
	c, err := Load("test-token")
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "test-token" || c.DatabasePath != "./data/remind.db" || c.Timezone != "Asia/Taipei" {
		t.Fatalf("unexpected default configuration: timezone=%q database=%q", c.Timezone, c.DatabasePath)
	}
	if c.Location == nil || c.Location.String() != "Asia/Taipei" {
		t.Fatal("default location was not loaded")
	}
	_, offset := time.Date(2027, 1, 1, 0, 0, 0, 0, c.Location).Zone()
	if offset != 8*60*60 {
		t.Fatalf("Taipei offset = %d", offset)
	}
	if c.ScanInterval != 30*time.Second || c.CatchupWindow != 24*time.Hour || c.AuthMaxAge != 24*time.Hour {
		t.Fatal("unexpected default durations")
	}
	if c.HTTPAddr != "127.0.0.1:8080" || c.LogLevel != slog.LevelInfo {
		t.Fatal("unexpected HTTP or logging defaults")
	}
	if c.Proxy != nil {
		t.Fatal("proxy is set without TELEGRAM_API_PROXY")
	}
	if c.MiniAppURL != "" {
		t.Fatalf("mini app URL = %q without MINIAPP_PUBLIC_URL", c.MiniAppURL)
	}
	if !c.Allowed(10001) || c.Allowed(10002) || c.Allowed(0) || c.Allowed(-1) {
		t.Fatal("authorization does not enforce the configured allowlist")
	}
}

func TestLoadOverrides(t *testing.T) {
	cleanEnvironment(t)
	values := map[string]string{
		"DATABASE_PATH":             "/data/custom.db",
		"APP_TIMEZONE":              "America/New_York",
		"REMINDER_SCAN_INTERVAL":    "1s",
		"REMINDER_CATCHUP_WINDOW":   "48h",
		"WEBAPP_AUTH_MAX_AGE":       "10m",
		"ALLOWED_TELEGRAM_USER_IDS": " 10001,10002,10001,9223372036854775807 ",
		"HTTP_ADDR":                 ":9090",
		"LOG_LEVEL":                 "debug",
		"TELEGRAM_API_PROXY":        "socks5://proxy.internal:1080",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	c, err := Load("argument-token")
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabasePath != "/data/custom.db" || c.Timezone != "America/New_York" || c.Location.String() != c.Timezone {
		t.Fatal("path or timezone override was ignored")
	}
	if c.ScanInterval != time.Second || c.CatchupWindow != 48*time.Hour || c.AuthMaxAge != 10*time.Minute {
		t.Fatal("duration override was ignored")
	}
	if c.HTTPAddr != ":9090" || c.LogLevel != slog.LevelDebug {
		t.Fatal("HTTP or logging override was ignored")
	}
	if c.Proxy == nil || c.Proxy.String() != "socks5://proxy.internal:1080" {
		t.Fatalf("proxy override was ignored: %v", c.Proxy)
	}
	if len(c.AllowedIDs) != 3 || !c.Allowed(10002) || !c.Allowed(9223372036854775807) {
		t.Fatal("IDs were not parsed or deduplicated correctly")
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	cases := []struct{ key, value string }{
		{"DATABASE_PATH", ""},
		{"DATABASE_PATH", ":memory:"},
		{"DATABASE_PATH", "file::memory:?cache=shared"},
		{"DATABASE_PATH", "file:remind?mode=memory&cache=shared"},
		{"APP_TIMEZONE", ""},
		{"APP_TIMEZONE", "Not/A_Zone"},
		{"APP_TIMEZONE", "Local"},
		{"REMINDER_SCAN_INTERVAL", ""},
		{"REMINDER_SCAN_INTERVAL", "999ms"},
		{"REMINDER_SCAN_INTERVAL", "-1s"},
		{"REMINDER_SCAN_INTERVAL", "thirty seconds"},
		{"REMINDER_CATCHUP_WINDOW", "0s"},
		{"REMINDER_CATCHUP_WINDOW", "-24h"},
		{"REMINDER_CATCHUP_WINDOW", "forever"},
		{"WEBAPP_AUTH_MAX_AGE", "0s"},
		{"WEBAPP_AUTH_MAX_AGE", "-1m"},
		{"WEBAPP_AUTH_MAX_AGE", ""},
		{"ALLOWED_TELEGRAM_USER_IDS", ""},
		{"ALLOWED_TELEGRAM_USER_IDS", "   "},
		{"ALLOWED_TELEGRAM_USER_IDS", "0"},
		{"ALLOWED_TELEGRAM_USER_IDS", "-1"},
		{"ALLOWED_TELEGRAM_USER_IDS", "1,0"},
		{"ALLOWED_TELEGRAM_USER_IDS", "1,"},
		{"ALLOWED_TELEGRAM_USER_IDS", "1,,2"},
		{"ALLOWED_TELEGRAM_USER_IDS", "1.5"},
		{"ALLOWED_TELEGRAM_USER_IDS", "9223372036854775808"},
		{"ALLOWED_TELEGRAM_USER_IDS", "not-an-id"},
		{"LOG_LEVEL", ""},
		{"LOG_LEVEL", "trace"},
		{"LOG_LEVEL", "INFO"},
		{"TELEGRAM_API_PROXY", "://missing-scheme"},
		{"TELEGRAM_API_PROXY", "http://"},
		{"TELEGRAM_API_PROXY", "ftp://127.0.0.1:21"},
		{"TELEGRAM_API_PROXY", "http://127.0.0.1:7890/path"},
		{"TELEGRAM_API_PROXY", "http://127.0.0.1:7890?x=1"},
		{"MINIAPP_PUBLIC_URL", "http://h"},
		{"MINIAPP_PUBLIC_URL", "https://"},
		{"MINIAPP_PUBLIC_URL", "https://h/other"},
		{"MINIAPP_PUBLIC_URL", "https://h/app?x=1"},
		{"MINIAPP_PUBLIC_URL", "https://h/app#f"},
		{"MINIAPP_PUBLIC_URL", "https://u:p@h"},
		{"MINIAPP_PUBLIC_URL", "not-a-url"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			cleanEnvironment(t)
			t.Setenv(tc.key, tc.value)
			_, err := Load("test-token")
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected error identifying %s, got %v", tc.key, err)
			}
		})
	}
}

func TestLoadProxy(t *testing.T) {
	tests := map[string]string{
		"http://127.0.0.1:7890":           "http://127.0.0.1:7890",
		"127.0.0.1:7890":                  "http://127.0.0.1:7890",
		"http://u:p@127.0.0.1:8443":       "http://u:p@127.0.0.1:8443",
		"socks5://127.0.0.1:1080":         "socks5://127.0.0.1:1080",
		"socks5h://alice@127.0.0.1:1080/": "socks5h://alice@127.0.0.1:1080/",
	}
	for raw, want := range tests {
		t.Run(raw, func(t *testing.T) {
			cleanEnvironment(t)
			t.Setenv("TELEGRAM_API_PROXY", raw)
			c, err := Load("test-token")
			if err != nil {
				t.Fatal(err)
			}
			if c.Proxy == nil || c.Proxy.String() != want {
				t.Fatalf("proxy = %v, want %s", c.Proxy, want)
			}
		})
	}
}

func TestLoadMiniAppURL(t *testing.T) {
	tests := map[string]string{
		"https://remind.example.com":      "https://remind.example.com/app/",
		"https://remind.example.com/":     "https://remind.example.com/app/",
		"https://remind.example.com/app":  "https://remind.example.com/app/",
		"https://remind.example.com/app/": "https://remind.example.com/app/",
		"https://h:8443":                  "https://h:8443/app/",
		"https://h:8443/app":              "https://h:8443/app/",
	}
	for raw, want := range tests {
		t.Run(raw, func(t *testing.T) {
			cleanEnvironment(t)
			t.Setenv("MINIAPP_PUBLIC_URL", raw)
			c, err := Load("test-token")
			if err != nil {
				t.Fatal(err)
			}
			if c.MiniAppURL != want {
				t.Fatalf("mini app URL = %q, want %q", c.MiniAppURL, want)
			}
		})
	}
}

func TestLoadRequiresAllowlist(t *testing.T) {
	cleanEnvironment(t)
	if err := os.Unsetenv("ALLOWED_TELEGRAM_USER_IDS"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("test-token"); err == nil {
		t.Fatal("missing allowlist was accepted")
	}
}

func TestLoadTokenComesOnlyFromArgument(t *testing.T) {
	cleanEnvironment(t)
	t.Setenv("TELEGRAM_BOT_TOKEN", "environment-token")
	for _, token := range []string{"", " \t\n"} {
		if _, err := Load(token); err == nil {
			t.Fatal("empty token argument fell back to environment")
		}
	}
	c, err := Load("argument-token")
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "argument-token" {
		t.Fatal("token argument was overridden")
	}
}

func TestLoadDoesNotReadDotenv(t *testing.T) {
	cleanEnvironment(t)
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("APP_TIMEZONE=UTC\nALLOWED_TELEGRAM_USER_IDS=999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load("argument-token")
	if err != nil {
		t.Fatal(err)
	}
	if c.Timezone != "Asia/Taipei" || c.Allowed(999) || !c.Allowed(10001) {
		t.Fatal("dotenv settings leaked into configuration")
	}
}

func TestLoadLogLevels(t *testing.T) {
	for value, expected := range map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo,
		"warn": slog.LevelWarn, "error": slog.LevelError,
	} {
		t.Run(value, func(t *testing.T) {
			cleanEnvironment(t)
			t.Setenv("LOG_LEVEL", value)
			c, err := Load("test-token")
			if err != nil || c.LogLevel != expected {
				t.Fatalf("level=%v err=%v", c.LogLevel, err)
			}
		})
	}
}

func TestLoadErrorsDoNotExposeValues(t *testing.T) {
	cleanEnvironment(t)
	t.Setenv("APP_TIMEZONE", "private-secret-value")
	_, err := Load("private-token-value")
	if err == nil || strings.Contains(err.Error(), "private-secret-value") || strings.Contains(err.Error(), "private-token-value") {
		t.Fatalf("unsafe or missing configuration error: %v", err)
	}
}

func TestAllowedZeroValue(t *testing.T) {
	if (Config{}).Allowed(10001) {
		t.Fatal("zero-value config permitted access")
	}
	c := Config{AllowedIDs: map[int64]bool{-1: true, 0: true, 1: false}}
	if c.Allowed(-1) || c.Allowed(0) || c.Allowed(1) {
		t.Fatal("invalid or explicitly disabled IDs were permitted")
	}
}
