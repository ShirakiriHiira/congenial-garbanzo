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
	if err := seedSIMs(ctx, svc, user.ID, now); err != nil {
		return err
	}
	return seedServices(ctx, svc, user.ID, now)
}

func seedSIMs(ctx context.Context, svc *service.Service, uid int64, now time.Time) error {
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
		card, err := svc.CreateSIM(ctx, uid, service.SIMInput{
			Name: fixture.name, CardType: fixture.cardType,
			Operator: &fixture.operator, CountryOrRegion: &fixture.region,
		})
		if err != nil {
			return err
		}
		for _, input := range fixture.periods {
			period, err := svc.CreatePeriod(ctx, uid, card.ID, input)
			if err != nil {
				return err
			}
			if _, err := svc.CreateRule(ctx, uid, period.ID, service.RuleInput{DaysBefore: 1, ReminderTime: "09:00"}); err != nil {
				return err
			}
		}
		if fixture.archived {
			status := "archived"
			if _, err := svc.UpdateSIM(ctx, uid, card.ID, service.SIMPatch{Status: &status}); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedServices(ctx context.Context, svc *service.Service, uid int64, now time.Time) error {
	categories, err := builtinCategories(ctx, svc, uid)
	if err != nil {
		return err
	}
	custom, err := svc.CreateCategory(ctx, uid, "自架服務")
	if err != nil {
		return fmt.Errorf("custom category: %w", err)
	}

	vps, err := svc.CreateService(ctx, uid, service.ServiceInput{
		Name:          "雲端主機 · 東京",
		CategoryID:    categories["vps"].ID,
		Provider:      new("Example Cloud"),
		Identifier:    new("tokyo-web-01"),
		ManagementURL: new("https://console.example.com/instances/101"),
		Notes:         new("每月自動備份；登入憑證請勿寫在備註。"),
	})
	if err != nil {
		return err
	}
	vpsPeriod, err := svc.CreatePeriod(ctx, uid, vps.ID, service.PeriodInput{
		PeriodType: "custom", Name: "主機租期", DatePrecision: "date",
		ExpiresAt: now.AddDate(0, 0, 45).Format(time.DateOnly),
		Terms:     recurringTerms(1, "month", 4800, "USD"),
	})
	if err != nil {
		return err
	}
	if _, err := svc.CreateRule(ctx, uid, vpsPeriod.ID, service.RuleInput{DaysBefore: 7, ReminderTime: "10:30"}); err != nil {
		return err
	}

	subscription, err := svc.CreateService(ctx, uid, service.ServiceInput{
		Name:          "AI 寫作助手 · 訂閱",
		CategoryID:    categories["subscription"].ID,
		Provider:      new("Example AI"),
		Identifier:    new("team-plan"),
		ManagementURL: new("https://example.com/ai/billing"),
		Notes:         new("年度方案；續期前確認團隊席次。"),
	})
	if err != nil {
		return err
	}
	annual, err := svc.CreatePeriod(ctx, uid, subscription.ID, service.PeriodInput{
		PeriodType: "custom", Name: "年度授權", DatePrecision: "date",
		ExpiresAt: now.AddDate(0, 0, 10).Format(time.DateOnly),
		Terms:     onceTerms(19900, "USD"),
	})
	if err != nil {
		return err
	}
	if _, err := svc.Renew(ctx, uid, annual.ID, service.RenewInput{
		ExpectedCycle: annual.Cycle,
		DatePrecision: "date",
		ExpiresAt:     now.AddDate(0, 0, 15).Format(time.DateOnly),
		Amount:        new(int64(19900)),
		Currency:      new("USD"),
		Notes:         new("手動續期：年度方案"),
	}); err != nil {
		return fmt.Errorf("seed renewal: %w", err)
	}
	if _, err := svc.CreateRule(ctx, uid, annual.ID, service.RuleInput{DaysBefore: 5, ReminderTime: "09:30"}); err != nil {
		return err
	}
	quota, err := svc.CreatePeriod(ctx, uid, subscription.ID, service.PeriodInput{
		PeriodType: "custom", Name: "月度點數加購", DatePrecision: "date",
		ExpiresAt: now.AddDate(0, 0, 25).Format(time.DateOnly),
		Terms:     recurringTerms(1, "month", 500, "USD"),
	})
	if err != nil {
		return err
	}
	if _, err := svc.CreateRule(ctx, uid, quota.ID, service.RuleInput{DaysBefore: 3, ReminderTime: "18:00"}); err != nil {
		return err
	}

	domain, err := svc.CreateService(ctx, uid, service.ServiceInput{
		Name:          "主網域 · example.com",
		CategoryID:    categories["domain"].ID,
		Provider:      new("Example Registrar"),
		Identifier:    new("example.com"),
		ManagementURL: new("https://dash.example-registrar.test/domain/example.com"),
		Notes:         new("到期前確認 DNS 與自動續約設定。"),
	})
	if err != nil {
		return err
	}
	domainPeriod, err := svc.CreatePeriod(ctx, uid, domain.ID, service.PeriodInput{
		PeriodType: "custom", Name: "網域註冊", DatePrecision: "date",
		ExpiresAt: now.AddDate(0, 0, 200).Format(time.DateOnly),
		Terms:     onceTerms(1500, "USD"),
	})
	if err != nil {
		return err
	}
	if _, err := svc.CreateRule(ctx, uid, domainPeriod.ID, service.RuleInput{DaysBefore: 14, ReminderTime: "11:00"}); err != nil {
		return err
	}

	home, err := svc.CreateService(ctx, uid, service.ServiceInput{
		Name:          "自架 Home Assistant",
		CategoryID:    custom.ID,
		Provider:      new("Home Lab"),
		Identifier:    new("home.lab.example"),
		ManagementURL: new("https://home.example.com"),
		Notes:         new("僅供本機測試資料；秘密請放在密碼管理器。"),
	})
	if err != nil {
		return err
	}
	homePeriod, err := svc.CreatePeriod(ctx, uid, home.ID, service.PeriodInput{
		PeriodType: "custom", Name: "硬體保養週期", DatePrecision: "date",
		ExpiresAt: now.AddDate(0, 0, 120).Format(time.DateOnly),
		Terms:     recurringTerms(1, "year", 0, "TWD"),
	})
	if err != nil {
		return err
	}
	if _, err := svc.CreateRule(ctx, uid, homePeriod.ID, service.RuleInput{DaysBefore: 7, ReminderTime: "08:00"}); err != nil {
		return err
	}
	return nil
}

func builtinCategories(ctx context.Context, svc *service.Service, uid int64) (map[string]service.Category, error) {
	out := map[string]service.Category{}
	var after int64
	for {
		page, err := svc.ListCategories(ctx, uid, after, 100)
		if err != nil {
			return nil, err
		}
		for _, category := range page.Data {
			if category.Code != nil {
				out[*category.Code] = category
			}
		}
		if page.NextCursor == nil {
			break
		}
		after = *page.NextCursor
	}
	for _, code := range []string{"sim", "vps", "subscription", "domain"} {
		if _, ok := out[code]; !ok {
			return nil, fmt.Errorf("builtin category %q missing", code)
		}
	}
	return out, nil
}

func recurringTerms(count int64, unit string, amount int64, currency string) *service.TermsInput {
	return &service.TermsInput{Mode: "recurring", IntervalCount: new(count), IntervalUnit: new(unit), Amount: new(amount), Currency: new(currency)}
}

func onceTerms(amount int64, currency string) *service.TermsInput {
	return &service.TermsInput{Mode: "once", Amount: new(amount), Currency: new(currency)}
}
