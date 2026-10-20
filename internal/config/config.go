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
	// Proxy overrides the Telegram API transport proxy. Nil keeps the standard
	// library behavior of honoring HTTP_PROXY/HTTPS_PROXY/NO_PROXY.
	Proxy *url.URL
	// MiniAppURL is the public https origin installed as the Telegram menu
	// button; an empty value disables the Mini App entry point in the bot.
	MiniAppURL string
	AllowedIDs map[int64]bool
	HTTPAddr   string
	LogLevel   slog.Level
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
	c.AuthMaxAge, err = duration("WEBAPP_AUTH_MAX_AGE", "24h", time.Nanosecond)
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

	c.Proxy, err = proxyURL(setting("TELEGRAM_API_PROXY", ""))
	if err != nil {
		return Config{}, err
	}
	c.MiniAppURL, err = miniAppURL(setting("MINIAPP_PUBLIC_URL", ""))
	if err != nil {
		return Config{}, err
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

// proxyURL validates an explicit proxy for the Telegram transport. An empty
// value returns nil so HTTP_PROXY/HTTPS_PROXY/NO_PROXY keep working unchanged.
func proxyURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	if !strings.Contains(raw, "://") {
		// A bare host:port is the common local-proxy spelling; assume HTTP like
		// net/http's own environment parser does.
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("TELEGRAM_API_PROXY must be an absolute proxy URL")
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("TELEGRAM_API_PROXY scheme must be http, https, socks5, or socks5h")
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("TELEGRAM_API_PROXY must not contain a path, query, or fragment")
	}
	return parsed, nil
}

// miniAppURL validates the public Mini App URL installed as the Telegram menu
// button. Every accepted spelling normalizes to one https .../app/ origin so the
// stored form is comparable and never repeats the install call.
func miniAppURL(raw string) (string, error) {
	const invalid = "MINIAPP_PUBLIC_URL must be https://<host> or https://<host>/app"
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New(invalid)
	}
	switch parsed.Path {
	case "", "/", "/app", "/app/":
	default:
		return "", errors.New(invalid)
	}
	return "https://" + parsed.Host + "/app/", nil
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
