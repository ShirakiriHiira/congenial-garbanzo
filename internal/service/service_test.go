package service

import (
	"context"
	"path/filepath"
	"telegram.bot.remind/internal/storage"
	"testing"
	"time"
)

func setup(t *testing.T) (*Service, *storage.Store, User, User) {
	t.Helper()
	ctx := context.Background()
	st, e := storage.Open(ctx, filepath.Join(t.TempDir(), "service.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	if e = st.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	s := New(st, "Asia/Taipei", func(id int64) bool { return id == 101 || id == 202 }, func() time.Time { return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) })
	a, e := s.Authenticate(ctx, 101, nil)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Authenticate(ctx, 202, nil)
	if e != nil {
		t.Fatal(e)
	}
	return s, st, a, b
}
func fixture(t *testing.T, s *Service, u User) (SIM, Period, Rule) {
	t.Helper()
	ctx := context.Background()
	sim, e := s.CreateSIM(ctx, u.ID, SIMInput{Name: "日本卡", CardType: "esim"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.CreatePeriod(ctx, u.ID, sim.ID, PeriodInput{PeriodType: "number_validity", Name: "号码", DatePrecision: "date", ExpiresAt: "2027-01-31"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateRule(ctx, u.ID, p.ID, RuleInput{DaysBefore: 1, ReminderTime: "09:00"})
	if e != nil {
		t.Fatal(e)
	}
	return sim, p, r
}
func TestOwnershipAndAuthorization(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()
	sim, p, r := fixture(t, s, a)
	if _, e := s.Authenticate(ctx, 303, nil); e != ErrForbidden {
		t.Fatalf("unauthorized: %v", e)
	}
	checks := []func() error{
		func() error { _, e := s.GetSIM(ctx, b.ID, sim.ID); return e },
		func() error { _, e := s.GetPeriod(ctx, b.ID, p.ID); return e },
		func() error { _, e := s.GetRule(ctx, b.ID, r.ID); return e },
		func() error { return s.ArchiveSIM(ctx, b.ID, sim.ID) },
		func() error { _, e := s.UpdatePeriod(ctx, b.ID, p.ID, PeriodPatch{ExpectedCycle: 1}); return e },
		func() error { _, e := s.ListRules(ctx, b.ID, p.ID, 0, 10); return e },
		func() error { _, e := s.ListRenewals(ctx, b.ID, p.ID, 0, 10); return e },
		func() error {
			_, e := s.Renew(ctx, b.ID, p.ID, RenewInput{ExpectedCycle: 1, DatePrecision: "date", ExpiresAt: "2027-03-01"})
			return e
		},
	}
	for i, f := range checks {
		if e := f(); e != ErrNotFound {
			t.Errorf("ownership %d: %v", i, e)
		}
	}
	s.allowed = func(int64) bool { return false }
	if _, e := s.GetSIM(ctx, a.ID, sim.ID); e != ErrForbidden {
		t.Fatalf("revocation: %v", e)
	}
}
func TestRenewIdempotenceAndRollback(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	_, p, r := fixture(t, s, a)
	_, e := st.DB.ExecContext(ctx, `INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,created_at,updated_at) VALUES(?,?,1,1,1,?,'pending',1,1,1)`, p.ID, r.ID, p.ExpiresAt)
	if e != nil {
		t.Fatal(e)
	}
	in := RenewInput{ExpectedCycle: 1, DatePrecision: "date", ExpiresAt: "2027-03-01"}
	_, e = st.DB.ExecContext(ctx, `CREATE TRIGGER fail_period_update BEFORE UPDATE ON validity_periods BEGIN SELECT RAISE(ABORT,'test failure'); END`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Renew(ctx, a.ID, p.ID, in); e != ErrUnavailable {
		t.Fatalf("expected transaction failure: %v", e)
	}
	var count int
	if e = st.DB.QueryRowContext(ctx, "SELECT count(*) FROM renewals").Scan(&count); e != nil || count != 0 {
		t.Fatalf("history rollback %d %v", count, e)
	}
	got, e := s.GetPeriod(ctx, a.ID, p.ID)
	if e != nil || got.Cycle != 1 || got.ExpiresAt != p.ExpiresAt {
		t.Fatalf("period rollback %+v %v", got, e)
	}
	var state string
	if e = st.DB.QueryRowContext(ctx, "SELECT status FROM reminder_jobs").Scan(&state); e != nil || state != "pending" {
		t.Fatalf("job rollback %s %v", state, e)
	}
	if _, e = st.DB.ExecContext(ctx, "DROP TRIGGER fail_period_update"); e != nil {
		t.Fatal(e)
	}
	first, e := s.Renew(ctx, a.ID, p.ID, in)
	if e != nil || first.Replayed {
		t.Fatalf("renew %+v %v", first, e)
	}
	s.now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	again, e := s.Renew(ctx, a.ID, p.ID, in)
	if e != nil || !again.Replayed || again.ID != first.ID {
		t.Fatalf("replay %+v %v", again, e)
	}
	in.ExpiresAt = "2027-04-01"
	if _, e = s.Renew(ctx, a.ID, p.ID, in); e != ErrConflict {
		t.Fatalf("different replay: %v", e)
	}
	if e = st.DB.QueryRowContext(ctx, "SELECT count(*) FROM renewals").Scan(&count); e != nil || count != 1 {
		t.Fatalf("history %d %v", count, e)
	}
}
func TestTimezoneAndMoney(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	sim, p, _ := fixture(t, s, a)
	absolute, e := s.CreatePeriod(ctx, a.ID, sim.ID, PeriodInput{PeriodType: "custom", Name: "instant", DatePrecision: "datetime", ExpiresAt: "2027-02-01T00:00:00+08:00"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetTimezone(ctx, a.ID, "America/New_York"); e != nil {
		t.Fatal(e)
	}
	changed, e := s.GetPeriod(ctx, a.ID, p.ID)
	if e != nil || changed.ExpiresAt == p.ExpiresAt || changed.Cycle != 2 {
		t.Fatalf("date %+v %v", changed, e)
	}
	same, e := s.GetPeriod(ctx, a.ID, absolute.ID)
	if e != nil || same.ExpiresAt != absolute.ExpiresAt || same.Cycle != 2 {
		t.Fatalf("datetime %+v %v", same, e)
	}
	if _, e = s.SetTimezone(ctx, a.ID, "Local"); e != ErrInvalid {
		t.Fatalf("Local timezone accepted: %v", e)
	}
	sameDate, samePrecision := *changed.LocalExpiresDate, "date"
	patched, e := s.UpdatePeriod(ctx, a.ID, p.ID, PeriodPatch{ExpectedCycle: changed.Cycle, DatePrecision: &samePrecision, ExpiresAt: &sameDate})
	if e != nil || patched.Cycle != changed.Cycle || patched.ExpiresAt != changed.ExpiresAt {
		t.Fatalf("unchanged expiry patched cycle: %+v %v", patched, e)
	}
	zero := int64(0)
	negative := int64(-1)
	currency := "TWD"
	in := RenewInput{ExpectedCycle: 2, DatePrecision: "date", ExpiresAt: "2027-03-01", Amount: &zero}
	if _, e = s.Renew(ctx, a.ID, p.ID, in); e != ErrInvalid {
		t.Fatalf("missing currency: %v", e)
	}
	in.Currency = &currency
	in.Amount = &negative
	if _, e = s.Renew(ctx, a.ID, p.ID, in); e != ErrInvalid {
		t.Fatalf("negative: %v", e)
	}
	in.Amount = &zero
	if _, e = s.Renew(ctx, a.ID, p.ID, in); e != nil {
		t.Fatalf("zero: %v", e)
	}
}
func TestDialogPersistenceAndPagination(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	chat := a.TelegramUserID
	if _, e := s.Authenticate(ctx, a.TelegramUserID, &chat); e != nil {
		t.Fatal(e)
	}
	_, p, r := fixture(t, s, a)
	res, e := st.DB.ExecContext(ctx, `INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,created_at,updated_at) VALUES(?,?,1,1,1,?,'sent',1,1)`, p.ID, r.ID, p.ExpiresAt)
	if e != nil {
		t.Fatal(e)
	}
	jid, e := res.LastInsertId()
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.StartRenewDialog(ctx, a.ID, jid, chat)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.StartRenewDialog(ctx, a.ID, jid, chat)
	if e != nil || d.ExpiresAt != again.ExpiresAt {
		t.Fatal("duplicate extends dialog", e)
	}
	restarted := New(st, "Asia/Taipei", s.allowed, s.now)
	if _, e = restarted.RenewFromDialog(ctx, a.ID, RenewInput{ExpectedCycle: 999, DatePrecision: "date", ExpiresAt: "2027-03-01"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.GetDialog(ctx, a.ID); e != ErrNotFound {
		t.Fatal("dialog remains", e)
	}
	if _, e = s.PeriodForJob(ctx, a.ID, jid); e != ErrConflict {
		t.Fatal("old callback", e)
	}
	for i := 0; i < 10; i++ {
		if _, e = s.CreatePeriod(ctx, a.ID, p.SIMID, PeriodInput{PeriodType: "custom", Name: "extra", DatePrecision: "date", ExpiresAt: "2027-03-01"}); e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.ListPeriods(ctx, a.ID, p.SIMID, 0, 10)
	if e != nil || len(page.Data) != 10 || page.NextCursor == nil {
		t.Fatalf("page %+v %v", page, e)
	}
	last, e := s.ListPeriods(ctx, a.ID, p.SIMID, *page.NextCursor, 10)
	if e != nil || len(last.Data) != 1 || last.NextCursor != nil {
		t.Fatalf("last %+v %v", last, e)
	}
}
