package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	_ "time/tzdata"

	"telegram.bot.remind/internal/api"
	appbot "telegram.bot.remind/internal/bot"
	"telegram.bot.remind/internal/config"
	"telegram.bot.remind/internal/reminder"
	"telegram.bot.remind/internal/service"
	"telegram.bot.remind/internal/storage"

	telegram "github.com/go-telegram/bot"
	"github.com/joho/godotenv"
)

const tokenEnv = "TELEGRAM_BOT_TOKEN"

func main() {
	if err := mainRun(); err != nil {
		slog.Error("application stopped", "code", "startup_or_runtime_failure", "error", err)
		os.Exit(1)
	}
}

func mainRun() error {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		addr := strings.TrimSpace(os.Getenv("HTTP_ADDR"))
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		return healthcheck(addr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return run(ctx)
}

func run(ctx context.Context) error {
	cfg, err := config.Load(loadToken())
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})))
	return runWith(ctx, cfg, time.Now)
}

func runWith(ctx context.Context, cfg config.Config, now func() time.Time, options ...telegram.Option) error {
	if now == nil {
		now = time.Now
	}
	store, err := storage.Open(ctx, cfg.DatabasePath)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("storage unavailable: %w", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("migration failed: %w", err)
	}

	svc := service.New(store, cfg.Timezone, cfg.Allowed, now)
	opts := []telegram.Option{
		telegram.WithNotAsyncHandlers(), telegram.WithWorkers(1),
		telegram.WithCheckInitTimeout(5 * time.Second),
		telegram.WithHTTPClient(30*time.Second, &http.Client{Timeout: 35 * time.Second}),
		telegram.WithErrorsHandler(func(error) { slog.Warn("telegram request failed", "code", "telegram_error") }),
	}
	opts = append(opts, options...)
	b, err := telegram.New(cfg.Token, opts...)
	if err != nil {
		return fmt.Errorf("telegram initialization failed: %w", err)
	}
	identityCtx, cancelIdentity := context.WithTimeout(ctx, 5*time.Second)
	me, err := b.GetMe(identityCtx)
	cancelIdentity()
	switch {
	case err != nil:
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("telegram identity unavailable: %w", err)
	case me == nil || strings.TrimSpace(me.Username) == "":
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("telegram identity unavailable: empty bot username")
	}
	handler := appbot.New(svc, me.Username, slog.Default())
	handler.Register(b)
	scheduler := reminder.New(store, appbot.NewSender(b, slog.Default()), cfg.Allowed, cfg.ScanInterval, cfg.CatchupWindow, now)

	// Recover expired leases and materialize due jobs before accepting traffic.
	// This path mutates storage only; delivery begins with the scheduler loop.
	if err := store.ScanReminderJobs(ctx, now(), cfg.CatchupWindow, cfg.Allowed); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("reminder recovery failed: %w", err)
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("http listener unavailable: %w", err)
	}
	defer listener.Close()
	if ctx.Err() != nil {
		return nil
	}
	var ready atomic.Bool
	server := &http.Server{
		Handler: api.New(svc, api.Config{
			BotToken:   cfg.Token,
			AuthMaxAge: cfg.AuthMaxAge,
			Ready:      ready.Load,
			Ping:       store.DB.PingContext,
			Now:        now,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRun()
	serveDone := make(chan error, 1)
	go func() {
		serveErr := server.Serve(listener)
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		serveDone <- serveErr
	}()
	select {
	case serveErr := <-serveDone:
		if serveErr == nil {
			serveErr = errors.New("http server stopped")
		}
		return serveErr
	default:
	}
	ready.Store(true)
	botDone := make(chan struct{})
	go func() {
		b.Start(runCtx)
		close(botDone)
	}()
	schedulerDone := make(chan error, 1)
	go func() { schedulerDone <- scheduler.Run(runCtx) }()

	var cause error
	select {
	case <-ctx.Done():
	case runErr := <-schedulerDone:
		// Run only returns nil once its context is canceled; any early exit is a
		// genuine failure and must not be mistaken for a clean shutdown.
		schedulerDone = nil
		cause = runErr
		if cause == nil {
			cause = errors.New("scheduler stopped")
		}
	case <-botDone:
		// Start blocks for the process lifetime; an early return means polling
		// stopped and the process must not keep claiming readiness.
		cause = errors.New("telegram loop stopped")
	case serveErr := <-serveDone:
		serveDone = nil
		cause = serveErr
		if cause == nil {
			cause = errors.New("http server stopped")
		}
	}

	ready.Store(false)
	handler.Stop()
	cancelRun()

	// One bounded budget covers HTTP draining, the SDK's in-flight handler, the
	// scheduler's acquired send plus result write. Delivery work uses contexts
	// detached from runCtx, so cancellation only stops new acquisitions.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(shutdownCtx) }()

	select {
	case <-botDone:
	case <-shutdownCtx.Done():
		if cause == nil {
			cause = errors.New("telegram shutdown incomplete")
		}
	}
	if schedulerDone != nil {
		select {
		case runErr := <-schedulerDone:
			if cause == nil {
				cause = runErr
			}
		case <-shutdownCtx.Done():
			if cause == nil {
				cause = errors.New("scheduler shutdown incomplete")
			}
		}
	}
	if serveDone != nil {
		select {
		case serveErr := <-serveDone:
			if cause == nil {
				cause = serveErr
			}
		case <-shutdownCtx.Done():
			if cause == nil {
				cause = errors.New("http shutdown incomplete")
			}
		}
	}
	select {
	case shutdownErr := <-shutdownDone:
		if shutdownErr != nil {
			_ = server.Close()
			if cause == nil {
				cause = errors.New("http shutdown incomplete")
			}
		}
	case <-shutdownCtx.Done():
		_ = server.Close()
		if cause == nil {
			cause = errors.New("http shutdown incomplete")
		}
	}
	if cause != nil {
		return fmt.Errorf("application runtime failure: %w", cause)
	}
	return nil
}

// healthcheck probes readiness without loading application credentials or storage.
func healthcheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("invalid healthcheck address")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	endpoint := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/readyz"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return errors.New("invalid healthcheck request")
	}
	client := &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("readiness unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("application not ready")
	}
	return nil
}

// loadToken reads the token from .env first and falls back to the process environment.
func loadToken() string {
	if values, err := godotenv.Read(".env"); err == nil {
		if token := values[tokenEnv]; token != "" {
			return token
		}
	}
	return os.Getenv(tokenEnv)
}
