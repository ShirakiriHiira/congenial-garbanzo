package reminder

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"telegram.bot.remind/internal/service"
	"telegram.bot.remind/internal/storage"
)

type fakeSender struct {
	calls int
	fn    func(int64) error
}

func (f *fakeSender) SendReminder(ctx context.Context, chatID int64, text string, id, periodID int64) (int64, error) {
	f.calls++
	if chatID != 101 || !strings.Contains(text, "测试卡") {
		return 0, errors.New("unexpected delivery payload")
	}
	if _, ok := ctx.Deadline(); !ok {
		return 0, errors.New("missing send deadline")
	}
	if f.fn != nil {
		if err := f.fn(id); err != nil {
			return 0, err
		}
	}
	return 700 + id, nil
}

type fixture struct {
	store     *storage.Store
	svc       *service.Service
	user      service.User
	sim       service.ServiceItem
	period    service.Period
	rule      service.Rule
	scheduler *Scheduler
	sender    *fakeSender
	now       time.Time
	allow     bool
	path      string
}

func setup(t *testing.T, private bool) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), allow: true, path: filepath.Join(t.TempDir(), "reminders.db")}
	var err error
	f.store, err = storage.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.store.Close() })
	if err = f.store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	allowed := func(id int64) bool { return f.allow && id == 101 }
	clock := func() time.Time { return f.now }
	f.svc = service.New(f.store, "UTC", allowed, clock)
	var chat *int64
	if private {
		id := int64(101)
		chat = &id
	}
	f.user, err = f.svc.Authenticate(context.Background(), 101, chat)
	if err != nil {
		t.Fatal(err)
	}
	var simCategory int64
	if err = f.store.DB.QueryRow("SELECT id FROM service_categories WHERE code='sim'").Scan(&simCategory); err != nil {
		t.Fatal(err)
	}
	f.sim, err = f.svc.CreateService(context.Background(), f.user.ID, service.ServiceInput{Name: "测试卡", CategoryID: simCategory, SIMDetails: &service.SIMDetails{CardType: "esim"}})
	if err != nil {
		t.Fatal(err)
	}
	f.period, err = f.svc.CreatePeriod(context.Background(), f.user.ID, f.sim.ID, service.PeriodInput{PeriodType: "number_validity", Name: "号码", DatePrecision: "date", ExpiresAt: "2027-01-31"})
	if err != nil {
		t.Fatal(err)
	}
	f.rule, err = f.svc.CreateRule(context.Background(), f.user.ID, f.period.ID, service.RuleInput{DaysBefore: 1, ReminderTime: "09:00"})
	if err != nil {
		t.Fatal(err)
	}
	f.sender = &fakeSender{}
	f.scheduler = New(f.store, f.sender, allowed, time.Second, 24*time.Hour, clock)
	return f
}

func (f *fixture) scan(t *testing.T) {
	t.Helper()
	if err := f.scheduler.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) jobs(t *testing.T) []service.Job {
	t.Helper()
	p, err := f.svc.ListJobs(context.Background(), f.user.ID, "all", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return p.Data
}

func (f *fixture) due() { f.now = time.Date(2027, 1, 30, 9, 0, 0, 0, time.UTC) }

func TestGenerationDeduplicationAndDelivery(t *testing.T) {
	f := setup(t, true)
	for range 10 {
		f.scan(t)
	}
	jobs := f.jobs(t)
	if len(jobs) != 1 || jobs[0].Status != "pending" || f.sender.calls != 0 {
		t.Fatalf("future jobs: %+v sends %d", jobs, f.sender.calls)
	}
	f.due()
	f.scan(t)
	f.scan(t)
	jobs = f.jobs(t)
	if f.sender.calls != 1 || jobs[0].Status != "sent" || jobs[0].TelegramMessageID == nil || *jobs[0].TelegramMessageID != 700+jobs[0].ID || jobs[0].AttemptToken != nil {
		t.Fatalf("sent: %+v calls=%d", jobs, f.sender.calls)
	}
	clock := "10:00"
	if _, err := f.svc.UpdateRule(context.Background(), f.user.ID, f.rule.ID, service.RulePatch{ExpectedRevision: 1, ReminderTime: &clock}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(2 * time.Hour)
	f.scan(t)
	sent := 0
	for _, job := range f.jobs(t) {
		if job.Status == "sent" {
			sent++
		}
	}
	if f.sender.calls != 1 || sent != 1 {
		t.Fatal("rule revision resent successful expiration")
	}
	if err := f.svc.ArchiveService(context.Background(), f.user.ID, f.sim.ID); err != nil {
		t.Fatal(err)
	}
	active := "active"
	if _, err := f.svc.UpdateService(context.Background(), f.user.ID, f.sim.ID, service.ServicePatch{Status: &active}); err != nil {
		t.Fatal(err)
	}
	f.scan(t)
	if f.sender.calls != 1 {
		t.Fatal("restoration resent unchanged expiration")
	}
	p, err := f.svc.GetPeriod(context.Background(), f.user.ID, f.period.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Renew(context.Background(), f.user.ID, p.ID, service.RenewInput{ExpectedCycle: p.Cycle, DatePrecision: "date", ExpiresAt: "2027-02-28"}); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2027, 2, 27, 10, 0, 0, 0, time.UTC)
	f.scan(t)
	if f.sender.calls != 2 {
		t.Fatal("new expiration was not delivered")
	}
}

func TestCancelledJobNeverRegenerates(t *testing.T) {
	f := setup(t, true)
	f.scan(t)
	id := f.jobs(t)[0].ID
	if err := f.svc.CancelJob(context.Background(), f.user.ID, id); err != nil {
		t.Fatal(err)
	}
	f.due()
	f.scan(t)
	f.scan(t)
	if jobs := f.jobs(t); len(jobs) != 1 || jobs[0].Status != "cancelled" || f.sender.calls != 0 {
		t.Fatalf("cancelled: %+v", jobs)
	}
}

func TestRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		permanent bool
		delay     time.Duration
		code      string
	}{
		{"rate_limit", &DeliveryError{Code: "rate_limited", RetryAfter: 95 * time.Second}, false, 95 * time.Second, "rate_limited"},
		{"server", &DeliveryError{Code: "remote_unavailable"}, false, 30 * time.Second, "remote_unavailable"},
		{"forbidden", &DeliveryError{Code: "remote_rejected", Permanent: true}, true, 0, "remote_rejected"},
		{"unknown", errors.New("sensitive SDK URL"), false, 30 * time.Second, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t, true)
			f.sender.fn = func(int64) error { return tc.err }
			f.due()
			f.scan(t)
			job := f.jobs(t)[0]
			if job.Status != "failed" || job.LastErrorCode == nil || *job.LastErrorCode != tc.code {
				t.Fatalf("failure: %+v", job)
			}
			if tc.permanent {
				if job.NextAttemptAt != nil {
					t.Fatal("permanent failure retried")
				}
				f.now = f.now.Add(time.Hour)
				f.scan(t)
				if f.sender.calls != 1 {
					t.Fatal("permanent resend")
				}
				return
			}
			if job.NextAttemptAt == nil || *job.NextAttemptAt != f.now.Add(tc.delay).Unix() {
				t.Fatalf("retry timing: %+v", job)
			}
			f.now = time.Unix(*job.NextAttemptAt-1, 0)
			f.scan(t)
			if f.sender.calls != 1 {
				t.Fatal("early retry")
			}
			for attempt := int64(2); attempt <= 6; attempt++ {
				job = f.jobs(t)[0]
				f.now = time.Unix(*job.NextAttemptAt, 0)
				f.scan(t)
				job = f.jobs(t)[0]
				if job.Attempts != int(attempt) {
					t.Fatalf("attempt %d: %+v", attempt, job)
				}
			}
			if job.NextAttemptAt != nil || f.sender.calls != 6 {
				t.Fatalf("retry cap: %+v", job)
			}
			f.now = f.now.Add(time.Hour)
			f.scan(t)
			if f.sender.calls != 6 {
				t.Fatal("seventh attempt")
			}
		})
	}
}

func TestNoPrivateChatPermanentFailure(t *testing.T) {
	f := setup(t, false)
	f.due()
	f.scan(t)
	job := f.jobs(t)[0]
	if f.sender.calls != 0 || job.Status != "failed" || job.NextAttemptAt != nil || job.LastErrorCode == nil || *job.LastErrorCode != "no_private_chat" {
		t.Fatalf("private chat: %+v", job)
	}
}

func TestCatchupSelectsNewestAndExpires(t *testing.T) {
	f := setup(t, true)
	for _, in := range []service.RuleInput{{DaysBefore: 3, ReminderTime: "10:00"}, {DaysBefore: 0, ReminderTime: "09:00"}, {DaysBefore: 0, ReminderTime: "10:00"}} {
		if _, err := f.svc.CreateRule(context.Background(), f.user.ID, f.period.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	f.now = time.Date(2027, 1, 31, 10, 0, 0, 0, time.UTC)
	f.scan(t)
	jobs := f.jobs(t)
	var sent, superseded, missed, pending int
	for _, j := range jobs {
		switch {
		case j.Status == "sent":
			sent++
		case j.Status == "pending":
			pending++
		case j.Status == "cancelled" && j.LastErrorCode != nil && *j.LastErrorCode == "missed_window":
			missed++
		case j.Status == "cancelled" && j.LastErrorCode != nil && *j.LastErrorCode == "superseded_catchup":
			superseded++
		}
	}
	if len(jobs) != 4 || f.sender.calls != 1 || sent != 1 || pending != 0 || superseded != 1 || missed != 2 {
		t.Fatalf("catchup: %+v", jobs)
	}
	t.Run("expired", func(t *testing.T) {
		f := setup(t, true)
		f.scan(t)
		f.now = time.Unix(f.period.ExpiresAt, 0)
		f.scan(t)
		job := f.jobs(t)[0]
		if f.sender.calls != 0 || job.Status != "cancelled" || job.LastErrorCode == nil || *job.LastErrorCode != "expired" {
			t.Fatalf("expired %+v", job)
		}
	})
}

func TestRecoveryAcrossDatabaseReopen(t *testing.T) {
	f := setup(t, true)
	f.due()
	if err := f.store.ScanReminderJobs(context.Background(), f.now, 24*time.Hour, f.scheduler.allowed); err != nil {
		t.Fatal(err)
	}
	job, err := f.store.ClaimReminder(context.Background(), f.now, 24*time.Hour, f.scheduler.allowed, "crashed-attempt")
	if err != nil || job == nil {
		t.Fatalf("claim %v %v", job, err)
	}
	if err = f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store, err = storage.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.scheduler.store = f.store
	f.svc = service.New(f.store, "UTC", f.scheduler.allowed, func() time.Time { return f.now })
	f.scan(t)
	if f.sender.calls != 0 {
		t.Fatal("unexpired lease stolen")
	}
	f.now = f.now.Add(time.Minute)
	f.scan(t)
	got := f.jobs(t)[0]
	if f.sender.calls != 1 || got.Status != "sent" || got.Attempts != 2 {
		t.Fatalf("recovered %+v", got)
	}
	if err = f.store.FinishReminder(context.Background(), job.ID, job.Token, f.now, 999, true, "", nil); err != nil {
		t.Fatal(err)
	}
	got = f.jobs(t)[0]
	if *got.TelegramMessageID == 999 {
		t.Fatal("stale token overwrote delivery")
	}
}

func TestInFlightCancellationAndRuleChange(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failure], func(t *testing.T) {
			f := setup(t, true)
			f.sender.fn = func(id int64) error {
				if err := f.svc.CancelJob(context.Background(), f.user.ID, id); err != nil {
					t.Fatal(err)
				}
				if failure {
					return errors.New("network")
				}
				return nil
			}
			f.due()
			f.scan(t)
			f.scan(t)
			j := f.jobs(t)[0]
			want := "sent"
			if failure {
				want = "cancelled"
			}
			if j.Status != want || j.NextAttemptAt != nil || j.AttemptToken != nil || f.sender.calls != 1 {
				t.Fatalf("inflight %+v", j)
			}
		})
	}
	t.Run("old_revision_success", func(t *testing.T) {
		f := setup(t, true)
		f.sender.fn = func(int64) error {
			clock := "09:01"
			_, err := f.svc.UpdateRule(context.Background(), f.user.ID, f.rule.ID, service.RulePatch{ExpectedRevision: 1, ReminderTime: &clock})
			return err
		}
		f.due()
		f.scan(t)
		f.now = f.now.Add(time.Minute)
		f.scan(t)
		if f.sender.calls != 1 {
			t.Fatal("new revision duplicated in-flight success")
		}
	})
}

func TestRevokedAuthorizationCancelsWithoutReviving(t *testing.T) {
	f := setup(t, true)
	f.scan(t)
	f.allow = false
	f.due()
	f.scan(t)
	f.allow = true
	j := f.jobs(t)[0]
	if j.Status != "cancelled" || j.LastErrorCode == nil || *j.LastErrorCode != "unauthorized" {
		t.Fatalf("revocation %+v", j)
	}
	f.scan(t)
	if f.sender.calls != 0 {
		t.Fatal("revoked job revived")
	}
}

func TestShutdownPreservesAcquiredResult(t *testing.T) {
	f := setup(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	f.sender.fn = func(int64) error {
		cancel()
		return nil
	}
	f.due()
	if err := f.scheduler.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	j := f.jobs(t)[0]
	if j.Status != "sent" {
		t.Fatalf("shutdown lost result %+v", j)
	}
}
