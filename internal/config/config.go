package config

import (
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

// Config contains validated process configuration. Only Token may originate in
// the root entry point's dotenv loader; all other settings come from the environment.
type Config struct {
	Token         string
	DatabasePath  string
	Timezone      string
	Location      *time.Location
	ScanInterval  time.Duration
	CatchupWindow time.Duration
	AuthMaxAge    time.Duration
	AllowedIDs    map[int64]bool
	HTTPAddr      string
	LogLevel      slog.Level
}

// Allowed is the shared authorization policy for API, Bot, and reminder delivery.
func (c Config) Allowed(telegramUserID int64) bool {
	return telegramUserID > 0 && c.AllowedIDs[telegramUserID]
}

// Load reads configuration without loading a dotenv file or modifying the
// environment. Errors identify settings, never their potentially sensitive values.
func Load(token string) (Config, error) {
	c := Config{Token: strings.TrimSpace(token)}
	if c.Token == "" {
		return Config{}, errors.New("TELEGRAM_BOT_TOKEN is required")
	}

	c.DatabasePath = setting("DATABASE_PATH", "./data/remind.db")
	if c.DatabasePath == "" || memoryDatabase(c.DatabasePath) {
		return Config{}, errors.New("DATABASE_PATH must name a persistent database")
	}

	c.Timezone = setting("APP_TIMEZONE", "Asia/Taipei")
	if c.Timezone == "" || c.Timezone == "Local" {
		return Config{}, errors.New("APP_TIMEZONE must be a valid IANA timezone")
	}
	var err error
	c.Location, err = time.LoadLocation(c.Timezone)
	if err != nil {
		return Config{}, errors.New("APP_TIMEZONE must be a valid timezone")
	}

	c.ScanInterval, err = duration("REMINDER_SCAN_INTERVAL", "30s", time.Second)
	if err != nil {
		return Config{}, err
	}
	c.CatchupWindow, err = duration("REMINDER_CATCHUP_WINDOW", "24h", time.Nanosecond)
	if err != nil {
		return Config{}, err
	}
	c.AuthMaxAge, err = duration("WEBAPP_AUTH_MAX_AGE", "5m", time.Nanosecond)
	if err != nil {
		return Config{}, err
	}

	c.AllowedIDs = make(map[int64]bool)
	for _, raw := range strings.Split(setting("ALLOWED_TELEGRAM_USER_IDS", ""), ",") {
		id, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if parseErr != nil || id <= 0 {
			return Config{}, errors.New("ALLOWED_TELEGRAM_USER_IDS must contain comma-separated positive int64 IDs")
		}
		c.AllowedIDs[id] = true
	}
	c.HTTPAddr = setting("HTTP_ADDR", "127.0.0.1:8080")

	switch setting("LOG_LEVEL", "info") {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "info":
		c.LogLevel = slog.LevelInfo
	case "warn":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		return Config{}, errors.New("LOG_LEVEL must be debug, info, warn, or error")
	}
	return c, nil
}

func setting(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func duration(key, fallback string, minimum time.Duration) (time.Duration, error) {
	value, err := time.ParseDuration(setting(key, fallback))
	if err != nil || value < minimum {
		if minimum == time.Second {
			return 0, errors.New(key + " must be a duration of at least 1s")
		}
		return 0, errors.New(key + " must be a positive duration")
	}
	return value, nil
}

func memoryDatabase(path string) bool {
	if path == ":memory:" {
		return true
	}
	if strings.HasPrefix(path, "file:") {
		u, err := url.Parse(path)
		if err != nil {
			return true
		}
		return u.Opaque == ":memory:" || u.Path == ":memory:" || u.Query().Get("mode") == "memory"
	}
	return false
}
