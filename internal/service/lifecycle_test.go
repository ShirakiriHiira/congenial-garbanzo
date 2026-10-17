package service

import (
	"context"
	"database/sql"
	"testing"
)

func TestRenewExpiredAndCancelPreservesLease(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	sim, _, _ := fixture(t, s, a)
	p, e := s.CreatePeriod(ctx, a.ID, sim.ID, PeriodInput{PeriodType: "custom", Name: "expired", DatePrecision: "date", ExpiresAt: "2026-12-01"})
	if e != nil {
		t.Fatal(e)
	}
	if p.Status != "expired" {
		t.Fatal(p.Status)
	}
	r, e := s.CreateRule(ctx, a.ID, p.ID, RuleInput{DaysBefore: 0, ReminderTime: "09:00"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = st.DB.ExecContext(ctx, `INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,locked_until,attempt_token,created_at,updated_at) VALUES(?,?,1,1,1,?,'sending',1,9999999999,'lease',1,1)`, p.ID, r.ID, p.ExpiresAt)
	if e != nil {
		t.Fatal(e)
	}
	renewed, e := s.Renew(ctx, a.ID, p.ID, RenewInput{ExpectedCycle: 1, DatePrecision: "date", ExpiresAt: "2027-03-01"})
	if e != nil {
		t.Fatal(e)
	}
	if renewed.PreviousCycle != 1 {
		t.Fatal(renewed)
	}
	got, e := s.GetPeriod(ctx, a.ID, p.ID)
	if e != nil || got.Status != "active" || got.Cycle != 2 {
		t.Fatalf("reactivation %+v %v", got, e)
	}
	var state string
	var token sql.NullString
	var next sql.NullInt64
	if e = st.DB.QueryRowContext(ctx, "SELECT status,attempt_token,next_attempt_at FROM reminder_jobs WHERE validity_period_id=?", p.ID).Scan(&state, &token, &next); e != nil {
		t.Fatal(e)
	}
	if state != "cancelled" || !token.Valid || token.String != "lease" || next.Valid {
		t.Fatalf("lease %s %+v %+v", state, token, next)
	}
	if e = s.CancelPeriod(ctx, a.ID, p.ID, 2); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Renew(ctx, a.ID, p.ID, RenewInput{ExpectedCycle: 2, DatePrecision: "date", ExpiresAt: "2027-04-01"}); e != ErrConflict {
		t.Fatal("cancelled renewal", e)
	}
}
