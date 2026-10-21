package reminder_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"telegram.bot.remind/internal/bot"
	"telegram.bot.remind/internal/reminder"
	"telegram.bot.remind/internal/service"
	"telegram.bot.remind/internal/storage"
)

type serviceDeliveryFixture struct {
	store     *storage.Store
	svc       *service.Service
	user      service.User
	item      service.ServiceItem
	period    service.Period
	scheduler *reminder.Scheduler
	sender    *bot.Sender
	now       time.Time
	path      string
	mu        sync.Mutex
	messages  []url.Values
}

func serviceReminderFixture(t *testing.T) *serviceDeliveryFixture {
	t.Helper()
	f := &serviceDeliveryFixture{now: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), path: filepath.Join(t.TempDir(), "service-reminders.db")}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot123456:service-test/sendMessage" {
			t.Errorf("unexpected Telegram method: %s", r.URL.Path)
			http.Error(w, "unexpected method", http.StatusNotFound)
			return
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Errorf("Telegram form: %v", err)
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.messages = append(f.messages, r.MultipartForm.Value)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":9101,"date":1,"chat":{"id":101,"type":"private"},"text":"ok"}}`)
	}))
	t.Cleanup(server.Close)
	client, err := telegram.New("123456:service-test", telegram.WithServerURL(server.URL), telegram.WithSkipGetMe(), telegram.WithHTTPClient(2*time.Second, server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	f.sender = bot.NewSender(client, slog.New(slog.DiscardHandler))
	f.open(t)
	t.Cleanup(func() { _ = f.store.Close() })
	chat := int64(101)
	f.user, err = f.svc.Authenticate(context.Background(), chat, &chat)
	if err != nil {
		t.Fatal(err)
	}
	categories, err := f.svc.ListCategories(context.Background(), f.user.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var categoryID int64
	for _, category := range categories.Data {
		if category.Code != nil && *category.Code == "vps" {
			categoryID = category.ID
		}
	}
	if categoryID == 0 {
		t.Fatal("missing VPS category")
	}
	f.item, err = f.svc.CreateService(context.Background(), f.user.ID, service.ServiceInput{Name: "云服务器 Frankfurt", CategoryID: categoryID})
	if err != nil {
		t.Fatal(err)
	}
	f.period = f.addPeriod(t, "服务器月租")
	return f
}

func (f *serviceDeliveryFixture) open(t *testing.T) {
	t.Helper()
	var err error
	f.store, err = storage.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	allowed := func(id int64) bool { return id == 101 }
	now := func() time.Time { return f.now }
	f.svc = service.New(f.store, "UTC", allowed, now)
	f.scheduler = reminder.New(f.store, f.sender, allowed, time.Second, 24*time.Hour, now)
}

func (f *serviceDeliveryFixture) addPeriod(t *testing.T, name string) service.Period {
	t.Helper()
	p, err := f.svc.CreatePeriod(context.Background(), f.user.ID, f.item.ID, service.PeriodInput{Name: name, DatePrecision: "date", ExpiresAt: "2027-01-31"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.CreateRule(context.Background(), f.user.ID, p.ID, service.RuleInput{DaysBefore: 1, ReminderTime: "09:00"}); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *serviceDeliveryFixture) jobs(t *testing.T) []service.Job {
	t.Helper()
	jobs, err := f.svc.ListJobs(context.Background(), f.user.ID, "all", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return jobs.Data
}

func (f *serviceDeliveryFixture) scan(t *testing.T) {
	t.Helper()
	if err := f.scheduler.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGenericServiceDeliveryRetainsCallbacksAndSentAcrossRestart(t *testing.T) {
	f := serviceReminderFixture(t)
	second := f.addPeriod(t, "独立备份套餐")
	f.scan(t)
	if jobs := f.jobs(t); len(jobs) != 2 || jobs[0].Status != "pending" || jobs[1].Status != "pending" {
		t.Fatalf("independent future jobs: %+v", jobs)
	}
	f.now = time.Date(2027, 1, 30, 9, 0, 0, 0, time.UTC)
	f.scan(t)
	jobs := f.jobs(t)
	f.mu.Lock()
	messages := append([]url.Values(nil), f.messages...)
	f.mu.Unlock()
	if len(jobs) != 2 || len(messages) != 2 {
		t.Fatalf("delivery jobs=%+v messages=%d", jobs, len(messages))
	}
	seen := map[int64]bool{}
	for _, message := range messages {
		if message.Get("chat_id") != "101" || !strings.Contains(message.Get("text"), "服务："+f.item.Name+"\n") {
			t.Fatalf("generic service payload: %v", message)
		}
		var keyboard models.InlineKeyboardMarkup
		if err := json.Unmarshal([]byte(message.Get("reply_markup")), &keyboard); err != nil {
			t.Fatal(err)
		}
		if len(keyboard.InlineKeyboard) != 1 || len(keyboard.InlineKeyboard[0]) != 2 {
			t.Fatalf("callback keyboard: %+v", keyboard)
		}
		buttons := keyboard.InlineKeyboard[0]
		id, err := strconv.ParseInt(strings.TrimPrefix(buttons[0].CallbackData, "v3:notify:renew:"), 10, 64)
		if err != nil || id != f.period.ID && id != second.ID || buttons[0].CallbackData != fmt.Sprintf("v3:notify:renew:%d", id) || buttons[1].CallbackData != fmt.Sprintf("v3:notify:view:%d", id) || seen[id] {
			t.Fatalf("retained callback IDs: %+v", buttons)
		}
		seen[id] = true
	}
	for _, job := range jobs {
		if !seen[job.ID] || job.Status != "sent" || job.Attempts != 1 || job.TelegramMessageID == nil || *job.TelegramMessageID != 9101 || job.AttemptToken != nil {
			t.Fatalf("persisted delivery: %+v", job)
		}
		if !seen[job.PeriodID] {
			t.Fatalf("job period %d has no callback", job.PeriodID)
		}
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.open(t)
	f.scan(t)
	f.scan(t)
	f.mu.Lock()
	count := len(f.messages)
	f.mu.Unlock()
	if after := f.jobs(t); !reflect.DeepEqual(jobs, after) || count != 2 {
		t.Fatalf("restart resent or changed sent jobs: before=%+v after=%+v messages=%d", jobs, after, count)
	}
}

func TestGenericTermsOnlyPreservesEveryReminderState(t *testing.T) {
	for _, state := range []string{"pending", "sending", "failed", "cancelled", "sent"} {
		t.Run(state, func(t *testing.T) {
			f := serviceReminderFixture(t)
			ctx := context.Background()
			if state != "pending" {
				f.now = time.Date(2027, 1, 30, 9, 0, 0, 0, time.UTC)
			}
			if err := f.store.ScanReminderJobs(ctx, f.now, 24*time.Hour, f.svc.Allowed); err != nil {
				t.Fatal(err)
			}
			switch state {
			case "sending", "failed":
				job, err := f.store.ClaimReminder(ctx, f.now, 24*time.Hour, f.svc.Allowed, "terms-live-attempt")
				if err != nil || job == nil {
					t.Fatalf("claim: %+v %v", job, err)
				}
				if state == "failed" {
					if err = f.store.FinishReminder(ctx, job.ID, job.Token, f.now, 0, false, "remote_rejected", nil); err != nil {
						t.Fatal(err)
					}
				}
			case "cancelled":
				if err := f.svc.CancelJob(ctx, f.user.ID, f.jobs(t)[0].ID); err != nil {
					t.Fatal(err)
				}
			case "sent":
				f.scan(t)
			}
			before := f.jobs(t)
			if len(before) != 1 || before[0].Status != state {
				t.Fatalf("state fixture: %+v", before)
			}
			f.now = f.now.Add(time.Second)
			count, amount, unit, currency := int64(1), int64(1299), "month", "USD"
			terms := service.TermsInput{Mode: "recurring", IntervalCount: &count, IntervalUnit: &unit, Amount: &amount, Currency: &currency}
			p := f.period
			for _, value := range []*service.TermsInput{&terms, nil} {
				updated, err := f.svc.UpdatePeriod(ctx, f.user.ID, p.ID, service.PeriodPatch{ExpectedCycle: p.Cycle, ExpectedTermsRevision: &p.Terms.Revision, TermsSet: true, Terms: value})
				if err != nil {
					t.Fatal(err)
				}
				if updated.Cycle != p.Cycle || updated.Terms.Revision != p.Terms.Revision+1 {
					t.Fatalf("independent terms revision: before=%+v after=%+v", p, updated)
				}
				if after := f.jobs(t); !reflect.DeepEqual(before, after) {
					t.Fatalf("terms changed %s job (including lease/token/attempts): before=%+v after=%+v", state, before, after)
				}
				p = updated
			}
			f.scan(t)
			if after := f.jobs(t); !reflect.DeepEqual(before, after) {
				t.Fatalf("scan revived or replaced %s job after terms reset: before=%+v after=%+v", state, before, after)
			}
		})
	}
}
