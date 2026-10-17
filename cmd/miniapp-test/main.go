// Command miniapp-test serves the real Mini App API against disposable local data.
// Run ./scripts/test-miniapp.sh to start it together with the browser UI.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata"

	"telegram.bot.remind/internal/api"
	"telegram.bot.remind/internal/service"
	"telegram.bot.remind/internal/storage"
)

const (
	testUserID = int64(900000001)
	testAddr   = "127.0.0.1:8088"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("test API stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	// No production configuration, environment file, bot client, or sender is loaded.
	listener, err := net.Listen("tcp4", testAddr)
	if err != nil {
		return fmt.Errorf("test listener: %w", err)
	}
	defer listener.Close()

	dir, err := os.MkdirTemp("", "miniapp-test-data-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	store, err := storage.Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}
	svc := service.New(store, "Asia/Taipei", func(id int64) bool { return id == testUserID }, time.Now)
	if err := seed(ctx, svc); err != nil {
		return fmt.Errorf("seed test data: %w", err)
	}
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return err
	}
	token := "900000001:" + hex.EncodeToString(tokenBytes[:])
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__test/init-data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			InitData string `json:"initData"`
		}{InitData: signedInitData(token, time.Now())})
	})
	mux.Handle("/", api.New(svc, api.Config{
		BotToken:   token,
		AuthMaxAge: 24 * time.Hour,
		Ready:      func() bool { return true },
		Ping:       store.DB.PingContext,
		Now:        time.Now,
	}))
	server := &http.Server{
		Handler:           localRequestsOnly(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	slog.Info("TEST MODE: disposable API ready; no Telegram delivery", "address", testAddr, "database", filepath.Join(dir, "test.db"))
	select {
	case err := <-serveDone:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			<-serveDone
			return err
		}
		err := <-serveDone
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// The UI proxy preserves its Host and Origin. Reject public hosts and browser
// cross-site requests even though the listener itself is loopback-only.
func localRequestsOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != testAddr && r.Host != "127.0.0.1:5180" && r.Host != "localhost:5180" {
			http.Error(w, "test mode requires a local host", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+testAddr && origin != "http://127.0.0.1:5180" && origin != "http://localhost:5180" {
			http.Error(w, "test mode requires a local origin", http.StatusForbidden)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "cross-site test requests are forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func signedInitData(token string, now time.Time) string {
	date := strconv.FormatInt(now.Unix(), 10)
	user := `{"id":900000001,"first_name":"Local Test","username":"miniapp_test"}`
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte("auth_date=" + date + "\nuser=" + user))
	return url.Values{
		"auth_date": {date},
		"user":      {user},
		"hash":      {hex.EncodeToString(mac.Sum(nil))},
	}.Encode()
}

func seed(ctx context.Context, svc *service.Service) error {
	user, err := svc.Authenticate(ctx, testUserID, nil)
	if err != nil {
		return err
	}
	now := time.Now().Truncate(time.Second)
	fixtures := []struct {
		name, cardType, operator, region string
		archived                         bool
		periods                          []service.PeriodInput
	}{
		{
			name: "日常主卡 · SIM", cardType: "physical_sim", operator: "測試電信", region: "台灣",
			periods: []service.PeriodInput{
				{PeriodType: "number_validity", Name: "門號效期", DatePrecision: "date", ExpiresAt: now.AddDate(0, 0, 60).Format(time.DateOnly)},
				{PeriodType: "plan_validity", Name: "", DatePrecision: "datetime", ExpiresAt: now.Add(72*time.Hour + 17*time.Second).Format(time.RFC3339)},
			},
		},
		{
			name: "旅行流量 · eSIM", cardType: "esim", operator: "Demo Mobile", region: "日本",
			periods: []service.PeriodInput{
				{PeriodType: "data_package_validity", Name: "即將到期的流量包", DatePrecision: "datetime", ExpiresAt: now.Add(36*time.Hour + 43*time.Second).Format(time.RFC3339)},
			},
		},
		{
			name: "已過期測試卡", cardType: "physical_sim", operator: "Demo Mobile", region: "香港",
			periods: []service.PeriodInput{
				{PeriodType: "number_validity", Name: "", DatePrecision: "datetime", ExpiresAt: now.Add(-48*time.Hour - 29*time.Second).Format(time.RFC3339)},
			},
		},
		{
			name: "封存備用 · eSIM", cardType: "esim", operator: "測試電信", region: "新加坡", archived: true,
			periods: []service.PeriodInput{
				{PeriodType: "custom", Name: "封存測試效期", DatePrecision: "date", ExpiresAt: now.AddDate(0, 0, 90).Format(time.DateOnly)},
			},
		},
	}
	for _, fixture := range fixtures {
		card, err := svc.CreateSIM(ctx, user.ID, service.SIMInput{
			Name: fixture.name, CardType: fixture.cardType,
			Operator: &fixture.operator, CountryOrRegion: &fixture.region,
		})
		if err != nil {
			return err
		}
		for _, input := range fixture.periods {
			period, err := svc.CreatePeriod(ctx, user.ID, card.ID, input)
			if err != nil {
				return err
			}
			if _, err := svc.CreateRule(ctx, user.ID, period.ID, service.RuleInput{DaysBefore: 1, ReminderTime: "09:00"}); err != nil {
				return err
			}
		}
		if fixture.archived {
			status := "archived"
			if _, err := svc.UpdateSIM(ctx, user.ID, card.ID, service.SIMPatch{Status: &status}); err != nil {
				return err
			}
		}
	}
	return nil
}
