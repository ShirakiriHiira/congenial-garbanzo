package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDeleteRuleRemovesJobsAndFreesSlot(t *testing.T) {
	s, st, a, b := setup(t)
	ctx := context.Background()
	_, p, r := fixture(t, s, a)
	if _, err := st.DB.ExecContext(ctx, `INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,1,?,'pending',1,1,1)`, p.ID, r.ID, p.Cycle, r.Revision, p.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRule(ctx, b.ID, r.ID, r.Revision); err != ErrNotFound {
		t.Fatalf("rule owner: %v", err)
	}
	if err := s.DeleteRule(ctx, a.ID, r.ID, r.Revision+1); err != ErrConflict {
		t.Fatalf("stale revision: %v", err)
	}
	if err := s.DeleteRule(ctx, a.ID, r.ID, r.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRule(ctx, a.ID, r.ID); err != ErrNotFound {
		t.Fatalf("rule remains: %v", err)
	}
	var jobs int
	if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM reminder_jobs WHERE reminder_rule_id=?", r.ID).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs remain: count=%d err=%v", jobs, err)
	}
	if _, err := s.GetPeriod(ctx, a.ID, p.ID); err != nil {
		t.Fatalf("period removed with rule: %v", err)
	}
	if _, err := s.CreateRule(ctx, a.ID, p.ID, RuleInput{DaysBefore: r.DaysBefore, ReminderTime: r.ReminderTime}); err != nil {
		t.Fatalf("deleted rule still occupies its slot: %v", err)
	}
}

func TestHardDeleteCascadesAndFencesReminder(t *testing.T) {
	for _, card := range []bool{false, true} {
		name := "period"
		if card {
			name = "card"
		}
		t.Run(name, func(t *testing.T) {
			s, st, a, b := setup(t)
			ctx := context.Background()
			sim, p, r := fixture(t, s, a)
			keptSIM, keptPeriod, _ := fixture(t, s, b)
			_, err := s.Renew(ctx, a.ID, p.ID, RenewInput{ExpectedCycle: p.Cycle, DatePrecision: "date", ExpiresAt: "2027-03-01"})
			if err != nil {
				t.Fatal(err)
			}
			p, err = s.GetPeriod(ctx, a.ID, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Unix(p.ExpiresAt-3600, 0)
			result, err := st.DB.ExecContext(ctx, `INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,locked_until,attempt_token,created_at,updated_at) VALUES(?,?,?,?,?,?,'sending',1,?,'delete-test',?,?)`, p.ID, r.ID, p.Cycle, r.Revision, now.Unix(), p.ExpiresAt, now.Unix()+60, now.Unix(), now.Unix())
			if err != nil {
				t.Fatal(err)
			}
			jid, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = st.DB.ExecContext(ctx, `INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,validity_period_id,expected_cycle,expires_at,created_at) VALUES(?,?,'renew','renew_expiration','{}',?,?,?,?)`, a.ID, a.TelegramUserID, p.ID, p.Cycle, now.Unix()+300, now.Unix()); err != nil {
				t.Fatal(err)
			}
			if ok, err := st.CheckReminder(ctx, jid, "delete-test", now, s.allowed); err != nil || !ok {
				t.Fatalf("initial lease: allowed=%v err=%v", ok, err)
			}
			if err := s.DeleteService(ctx, b.ID, sim.ID); err != ErrNotFound {
				t.Fatalf("card owner: %v", err)
			}
			if err := s.DeletePeriod(ctx, b.ID, p.ID, p.Cycle); err != ErrNotFound {
				t.Fatalf("period owner: %v", err)
			}
			if err := s.DeletePeriod(ctx, a.ID, p.ID, p.Cycle-1); err != ErrConflict {
				t.Fatalf("stale cycle: %v", err)
			}
			if card {
				err = s.DeleteService(ctx, a.ID, sim.ID)
			} else {
				err = s.DeletePeriod(ctx, a.ID, p.ID, p.Cycle)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.GetPeriod(ctx, a.ID, p.ID); err != ErrNotFound {
				t.Fatalf("period remains: %v", err)
			}
			if _, err := s.GetRule(ctx, a.ID, r.ID); err != ErrNotFound {
				t.Fatalf("rule remains: %v", err)
			}
			if _, err := s.GetService(ctx, a.ID, sim.ID); (card && err != ErrNotFound) || (!card && err != nil) {
				t.Fatalf("card removal=%v: %v", card, err)
			}
			for _, table := range []string{"renewals", "reminder_jobs", "bot_dialogs"} {
				var count int
				if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE validity_period_id=?", p.ID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("%s remains: count=%d err=%v", table, count, err)
				}
			}
			if ok, err := st.CheckReminder(ctx, jid, "delete-test", now, s.allowed); err != nil || ok {
				t.Fatalf("deleted lease: allowed=%v err=%v", ok, err)
			}
			if err := st.FinishReminder(ctx, jid, "delete-test", now, 99, true, "", nil); err != nil {
				t.Fatal(err)
			}
			if err := st.ScanReminderJobs(ctx, now, time.Hour, s.allowed); err != nil {
				t.Fatal(err)
			}
			if job, err := st.ClaimReminder(ctx, now, time.Hour, s.allowed, "after-delete"); err != nil || job != nil {
				t.Fatalf("claimed deleted work: %+v %v", job, err)
			}
			if _, err := s.GetService(ctx, b.ID, keptSIM.ID); err != nil {
				t.Fatalf("unrelated card: %v", err)
			}
			if _, err := s.GetPeriod(ctx, b.ID, keptPeriod.ID); err != nil {
				t.Fatalf("unrelated period: %v", err)
			}
		})
	}
}

func TestDeletedIdentifiersAreNotReused(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	sim, p, rule := fixture(t, s, a)
	renewal, err := s.Renew(ctx, a.ID, p.ID, RenewInput{ExpectedCycle: p.Cycle, DatePrecision: "date", ExpiresAt: "2027-03-01"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := st.DB.ExecContext(ctx, `INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,created_at,updated_at) VALUES(?,?,2,1,1,?,'sent',1,1)`, p.ID, rule.ID, renewal.NewExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteService(ctx, a.ID, sim.ID); err != nil {
		t.Fatal(err)
	}
	nextSIM, nextPeriod, nextRule := fixture(t, s, a)
	nextRenewal, err := s.Renew(ctx, a.ID, nextPeriod.ID, RenewInput{ExpectedCycle: 1, DatePrecision: "date", ExpiresAt: "2027-03-01"})
	if err != nil {
		t.Fatal(err)
	}
	result, err = st.DB.ExecContext(ctx, `INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,created_at,updated_at) VALUES(?,?,2,1,1,?,'pending',1,1)`, nextPeriod.ID, nextRule.ID, nextRenewal.NewExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	nextJobID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if nextSIM.ID <= sim.ID || nextPeriod.ID <= p.ID || nextRule.ID <= rule.ID || nextRenewal.ID <= renewal.ID || nextJobID <= jobID {
		t.Fatalf("identifiers reused: card %d/%d period %d/%d rule %d/%d renewal %d/%d job %d/%d", sim.ID, nextSIM.ID, p.ID, nextPeriod.ID, rule.ID, nextRule.ID, renewal.ID, nextRenewal.ID, jobID, nextJobID)
	}
	if err := s.ArchiveService(ctx, a.ID, sim.ID); err != ErrNotFound {
		t.Fatalf("stale card mutation: %v", err)
	}
	if err := s.DeletePeriod(ctx, a.ID, p.ID, 2); err != ErrNotFound {
		t.Fatalf("stale period mutation: %v", err)
	}
	if err := s.DisableRule(ctx, a.ID, rule.ID, 1); err != ErrNotFound {
		t.Fatalf("stale rule mutation: %v", err)
	}
	if err := s.CancelJob(ctx, a.ID, jobID); err != ErrNotFound {
		t.Fatalf("stale reminder mutation: %v", err)
	}
}

func TestOptionalPeriodName(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	sim, _, _ := fixture(t, s, a)
	for _, name := range []string{"", " \t\n"} {
		p, err := s.CreatePeriod(ctx, a.ID, sim.ID, PeriodInput{PeriodType: "custom", Name: name, DatePrecision: "date", ExpiresAt: "2027-02-01"})
		if err != nil || p.Name != "" {
			t.Fatalf("blank name: %+v %v", p, err)
		}
		named := "named"
		p, err = s.UpdatePeriod(ctx, a.ID, p.ID, PeriodPatch{ExpectedCycle: p.Cycle, Name: &named})
		if err != nil {
			t.Fatal(err)
		}
		p, err = s.UpdatePeriod(ctx, a.ID, p.ID, PeriodPatch{ExpectedCycle: p.Cycle, Name: &name})
		if err != nil || p.Name != "" {
			t.Fatalf("clear name: %+v %v", p, err)
		}
	}
	tooLong := strings.Repeat("中", 201)
	if _, err := s.CreatePeriod(ctx, a.ID, sim.ID, PeriodInput{PeriodType: "custom", Name: tooLong, DatePrecision: "date", ExpiresAt: "2027-02-01"}); err != ErrInvalid {
		t.Fatalf("oversize name: %v", err)
	}
}

func TestHardDeleteRollbackAndAuthorization(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	sim, p, r := fixture(t, s, a)
	if _, err := st.DB.ExecContext(ctx, `CREATE TRIGGER fail_rule_delete BEFORE DELETE ON reminder_rules BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteService(ctx, a.ID, sim.ID); err != ErrUnavailable {
		t.Fatalf("cascade failure: %v", err)
	}
	if _, err := s.GetService(ctx, a.ID, sim.ID); err != nil {
		t.Fatalf("card not rolled back: %v", err)
	}
	if _, err := s.GetPeriod(ctx, a.ID, p.ID); err != nil {
		t.Fatalf("period not rolled back: %v", err)
	}
	if _, err := s.GetRule(ctx, a.ID, r.ID); err != nil {
		t.Fatalf("rule not rolled back: %v", err)
	}
	s.allowed = func(int64) bool { return false }
	if err := s.DeleteService(ctx, a.ID, sim.ID); err != ErrForbidden {
		t.Fatalf("revoked card delete: %v", err)
	}
	if err := s.DeletePeriod(ctx, a.ID, p.ID, p.Cycle); err != ErrForbidden {
		t.Fatalf("revoked period delete: %v", err)
	}
}
