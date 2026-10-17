package reminder

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"telegram.bot.remind/internal/service"
	"telegram.bot.remind/internal/storage"
)

// TestCatchupRanksOldestAndNewestCorrectly exercises the plan's canonical
// out-of-window, two-hour-old and one-hour-old reminders. The fixture also
// contributes a 14-hour-old reminder that must be superseded as well.
func TestCatchupRanksOldestAndNewestCorrectly(t *testing.T) {
	f := setup(t, true)
	for _, in := range []service.RuleInput{
		{DaysBefore: 4, ReminderTime: "23:00"},
		{DaysBefore: 1, ReminderTime: "21:00"},
		{DaysBefore: 1, ReminderTime: "22:00"},
	} {
		if _, err := f.svc.CreateRule(context.Background(), f.user.ID, f.period.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	// The fixture timezone is UTC and the period ends at 2027-02-01T00:00Z.
	// At 23:00 on the 30th the four-day-old job is outside the 24-hour window;
	// among the remaining three due jobs only the newest (22:00) may be sent.
	f.now = time.Date(2027, 1, 30, 23, 0, 0, 0, time.UTC)
	f.scan(t)
	jobs := f.jobs(t)
	reasons := map[string]int{}
	statuses := map[string]int{}
	var sent service.Job
	for _, j := range jobs {
		statuses[j.Status]++
		if j.Status == "sent" {
			sent = j
		}
		if j.Status == "cancelled" && j.LastErrorCode != nil {
			reasons[*j.LastErrorCode]++
		}
	}
	if f.sender.calls != 1 || len(jobs) != 4 || statuses["sent"] != 1 ||
		sent.ScheduledAt != time.Date(2027, 1, 30, 22, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("catchup ranking: %+v sends=%d", jobs, f.sender.calls)
	}
	if reasons["missed_window"] != 1 || reasons["superseded_catchup"] != 2 {
		t.Fatalf("catchup reasons: %+v", reasons)
	}
}

// TestRunStopsOnCancellation checks Run returns promptly once its context ends.
func TestRunStopsOnCancellation(t *testing.T) {
	f := setup(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.scheduler.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run ignored cancellation")
	}
}

// TestDowngradedLeaseBecomesReclaimable covers a SendReminder that ignored its
// context and outlived the 60-second lease. A live lease is never stolen; an
// expired one becomes reclaimable after the invariant sweep, and the stale
// worker cannot overwrite the eventual result.
func TestDowngradedLeaseBecomesReclaimable(t *testing.T) {
	f := setup(t, true)
	f.due()
	if err := f.store.ScanReminderJobs(context.Background(), f.now, 24*time.Hour, f.scheduler.allowed); err != nil {
		t.Fatal(err)
	}
	stale, err := f.store.ClaimReminder(context.Background(), f.now, 24*time.Hour, f.scheduler.allowed, "slow-attempt")
	if err != nil || stale == nil {
		t.Fatalf("claim %v %v", stale, err)
	}
	f.scan(t)
	if f.sender.calls != 0 {
		t.Fatal("live lease stolen")
	}
	f.now = f.now.Add(61 * time.Second)
	f.scan(t)
	got := f.jobs(t)[0]
	if f.sender.calls != 1 || got.Status != "sent" || got.Attempts != 2 {
		t.Fatalf("lease recovery: %+v", got)
	}
	if err = f.store.FinishReminder(context.Background(), stale.ID, stale.Token, f.now, 999, true, "", nil); err != nil {
		t.Fatal(err)
	}
	if got = f.jobs(t)[0]; got.TelegramMessageID == nil || *got.TelegramMessageID == 999 {
		t.Fatalf("stale lease overwrote result: %+v", got)
	}
}

// TestDatabaseFailureIsNotAnEmptyQueue ensures storage errors surface.
func TestDatabaseFailureIsNotAnEmptyQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.db")
	st, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	if err = st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	s := New(st, &fakeSender{}, func(int64) bool { return true }, time.Second, time.Hour, func() time.Time { return time.Now() })
	if err = s.Scan(context.Background()); err == nil {
		t.Fatal("closed store reported empty queue")
	}
}

// TestScanIsSerialized proves that concurrent Scan calls cannot double-claim
// the same job even though the scheduler only guarantees single-instance use.
func TestScanIsSerialized(t *testing.T) {
	f := setup(t, true)
	f.due()
	done := make(chan struct{})
	for range 4 {
		go func() {
			defer func() { done <- struct{}{} }()
			_ = f.scheduler.Scan(context.Background())
		}()
	}
	for range 4 {
		<-done
	}
	if f.sender.calls != 1 {
		t.Fatalf("concurrent scans sent %d", f.sender.calls)
	}
}

// TestRetrySurvivesReopen verifies failed state and its retry timestamp persist
// across a fresh Store on the same file, and that the retry classification uses
// the safe code rather than the underlying error message.
func TestRetrySurvivesReopen(t *testing.T) {
	f := setup(t, true)
	f.sender.fn = func(int64) error { return &DeliveryError{Code: "remote_unavailable"} }
	f.due()
	f.scan(t)
	before := f.jobs(t)[0]
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = storage.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.store.Close() })
	f.scheduler.store = f.store
	f.svc = service.New(f.store, "UTC", f.scheduler.allowed, func() time.Time { return f.now })
	f.now = time.Unix(*before.NextAttemptAt, 0)
	f.scan(t)
	after := f.jobs(t)[0]
	if after.Attempts != 2 || after.Status != "failed" || f.sender.calls != 2 {
		t.Fatalf("retry after reopen: %+v", after)
	}
}
