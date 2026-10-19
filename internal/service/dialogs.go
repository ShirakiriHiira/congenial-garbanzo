package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Dialog actions are a closed set. The bot never accepts an action or step
// from a user; commands and callbacks select one of these constants.
const (
	DialogRenew       = "renew"
	DialogServiceAdd  = "service_add"
	DialogServiceEdit = "service_edit"
	DialogPeriodAdd   = "period_add"
	DialogTermsEdit   = "terms_edit"
	DialogRuleAdd     = "rule_add"
	DialogRuleEdit    = "rule_edit"
	DialogTimezone    = "timezone"
)

const (
	dialogTTL      = 900
	dialogDraftMax = 16384
)

var dialogActions = map[string]bool{
	DialogRenew: true, DialogServiceAdd: true, DialogServiceEdit: true,
	DialogPeriodAdd: true, DialogTermsEdit: true, DialogRuleAdd: true,
	DialogRuleEdit: true, DialogTimezone: true,
}

const dialogColumns = `d.user_id,d.chat_id,d.action,d.step,d.draft_json,d.service_id,d.validity_period_id,d.rule_id,d.expected_cycle,d.expected_terms_revision,d.expected_revision,d.expires_at,d.created_at`

// validDialogDraft rejects oversized or non-JSON drafts before they reach the
// database. Drafts may contain user notes or SIM identifiers, so callers must
// never log this value.
func validDialogDraft(draft string) bool {
	return utf8.ValidString(draft) && len(draft) <= dialogDraftMax && json.Valid([]byte(draft))
}

func nullableID(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func scanDialog(row scanner) (Dialog, error) {
	var d Dialog
	var serviceID, periodID, ruleID, cycle, termsRevision, revision sql.NullInt64
	e := row.Scan(&d.UserID, &d.ChatID, &d.Action, &d.Step, &d.Draft, &serviceID, &periodID, &ruleID, &cycle, &termsRevision, &revision, &d.ExpiresAt, &d.CreatedAt)
	if e != nil {
		return d, e
	}
	d.ServiceID = nullableID(serviceID)
	d.ValidityPeriodID = nullableID(periodID)
	d.RuleID = nullableID(ruleID)
	d.ExpectedCycle = nullableID(cycle)
	d.ExpectedTermsRevision = nullableID(termsRevision)
	d.ExpectedRevision = nullableID(revision)
	return d, e
}

// getDialog reads the current row and enforces both ownership and expiry.
// Referenced objects are joined so a row cannot outlive a transfer of a
// service, period, or rule to another user.
func (s *Service) getDialog(ctx context.Context, q queryer, uid int64) (Dialog, error) {
	d, e := scanDialog(q.QueryRowContext(ctx, `SELECT `+dialogColumns+`
        FROM bot_dialogs d
        LEFT JOIN service_items s1 ON s1.id=d.service_id
        LEFT JOIN validity_periods p ON p.id=d.validity_period_id
        LEFT JOIN service_items s2 ON s2.id=p.service_id
        LEFT JOIN reminder_rules r ON r.id=d.rule_id
        LEFT JOIN validity_periods p2 ON p2.id=r.validity_period_id
        LEFT JOIN service_items s3 ON s3.id=p2.service_id
        WHERE d.user_id=? AND COALESCE(s1.user_id,s2.user_id,s3.user_id,?)=?`, uid, uid, uid))
	if e != nil {
		return d, dbError(e)
	}
	if d.ExpiresAt <= s.now().Unix() {
		return Dialog{}, ErrNotFound
	}
	return d, nil
}

func (s *Service) GetDialog(ctx context.Context, uid int64) (Dialog, error) {
	if e := s.authorize(ctx, s.store.DB, uid); e != nil {
		return Dialog{}, e
	}
	return s.getDialog(ctx, s.store.DB, uid)
}

// validateDialogTargets proves every referenced object still belongs to the
// user and that parent/child references agree.
func (s *Service) validateDialogTargets(ctx context.Context, q queryer, uid int64, d Dialog) error {
	if d.RuleID != nil {
		r, e := getRule(ctx, q, uid, *d.RuleID)
		if e != nil {
			return e
		}
		if d.ValidityPeriodID != nil && r.ValidityPeriodID != *d.ValidityPeriodID {
			return ErrNotFound
		}
	}
	if d.ValidityPeriodID != nil {
		p, e := s.getPeriod(ctx, q, uid, *d.ValidityPeriodID)
		if e != nil {
			return e
		}
		if d.ServiceID != nil && p.ServiceID != *d.ServiceID {
			return ErrNotFound
		}
	}
	if d.ServiceID != nil {
		if _, e := getServiceItem(ctx, q, uid, *d.ServiceID); e != nil {
			return e
		}
	}
	return nil
}

func sameDialogTarget(a, b Dialog) bool {
	return a.Action == b.Action && equalPtr(a.ServiceID, b.ServiceID) &&
		equalPtr(a.ValidityPeriodID, b.ValidityPeriodID) && equalPtr(a.RuleID, b.RuleID)
}

func validDialogStep(step string) bool {
	return step == strings.TrimSpace(step) && len(step) >= 1 && len(step) <= 64
}

// StartDialog creates or replaces the single live dialog for a user. Repeating
// the same action and target reuses the unexpired row without extending its
// TTL, so a double click cannot refresh a stale wizard indefinitely. Switching
// to a different target replaces the row; the bot must ask before doing that.
func (s *Service) StartDialog(ctx context.Context, uid, chatID int64, action, step string, d Dialog) (Dialog, error) {
	if !dialogActions[action] || !validDialogStep(step) || !validDialogDraft(d.Draft) {
		return Dialog{}, ErrInvalid
	}
	var out Dialog
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var e error
		out, e = s.startDialogTx(ctx, tx, uid, chatID, action, step, d)
		return e
	})
	return out, e
}

// PurgeExpiredDialog reports whether an expired dialog row was removed. Unlike
// GetDialog it distinguishes "never started" from "expired", so the bot can
// tell the user why their input was ignored.
func (s *Service) PurgeExpiredDialog(ctx context.Context, uid int64) (bool, error) {
	removed := false
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var expires int64
		e := tx.QueryRowContext(ctx, "SELECT expires_at FROM bot_dialogs WHERE user_id=?", uid).Scan(&expires)
		if e == sql.ErrNoRows {
			return nil
		}
		if e != nil {
			return e
		}
		if expires > s.now().Unix() {
			return nil
		}
		result, e := tx.ExecContext(ctx, "DELETE FROM bot_dialogs WHERE user_id=?", uid)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		removed = n > 0
		return e
	})
	return removed, e
}

// UpdateDialogDraft advances the live dialog without extending its TTL. The
// action and targets are fixed at start; a wizard that needs a different
// target must start a new dialog explicitly.
func (s *Service) UpdateDialogDraft(ctx context.Context, uid int64, step, draft string) (Dialog, error) {
	if !validDialogStep(step) || !validDialogDraft(draft) {
		return Dialog{}, ErrInvalid
	}
	var out Dialog
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		d, e := s.getDialog(ctx, tx, uid)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE bot_dialogs SET step=?,draft_json=? WHERE user_id=?", step, draft, uid); e != nil {
			return e
		}
		d.Step, d.Draft = step, draft
		out = d
		return nil
	})
	return out, e
}

// CancelDialog removes any dialog row, including expired ones.
func (s *Service) CancelDialog(ctx context.Context, uid int64) (bool, error) {
	removed := false
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "DELETE FROM bot_dialogs WHERE user_id=?", uid)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		removed = n > 0
		return e
	})
	return removed, e
}

// CancelDialogsForTarget removes dialogs that refer to an object being closed
// by a business operation. Foreign-key cascades already cover permanent
// deletes; this covers state transitions such as archiving a service.
func (s *Service) CancelDialogsForTarget(ctx context.Context, tx *sql.Tx, uid, serviceID, periodID, ruleID int64) error {
	if serviceID > 0 {
		if _, e := tx.ExecContext(ctx, "DELETE FROM bot_dialogs WHERE user_id=? AND (service_id=? OR validity_period_id IN (SELECT id FROM validity_periods WHERE service_id=?))", uid, serviceID, serviceID); e != nil {
			return e
		}
	}
	if periodID > 0 {
		if _, e := tx.ExecContext(ctx, "DELETE FROM bot_dialogs WHERE user_id=? AND validity_period_id=?", uid, periodID); e != nil {
			return e
		}
	}
	if ruleID > 0 {
		_, e := tx.ExecContext(ctx, "DELETE FROM bot_dialogs WHERE user_id=? AND rule_id=?", uid, ruleID)
		return e
	}
	return nil
}

// startRenewTarget opens the renewal wizard for one period after the same
// private chat, ownership, status, and cycle checks as callback renewals.
func (s *Service) startRenewTarget(ctx context.Context, tx *sql.Tx, uid, periodID, chatID int64) (Dialog, error) {
	var private *int64
	if e := tx.QueryRowContext(ctx, "SELECT private_chat_id FROM users WHERE id=?", uid).Scan(&private); e != nil {
		return Dialog{}, e
	}
	if private == nil || *private != chatID {
		return Dialog{}, ErrForbidden
	}
	p, e := s.getPeriod(ctx, tx, uid, periodID)
	if e != nil {
		return Dialog{}, e
	}
	parent, e := getServiceItem(ctx, tx, uid, p.ServiceID)
	if e != nil {
		return Dialog{}, e
	}
	if p.Status == "cancelled" || parent.Status != "active" {
		return Dialog{}, ErrConflict
	}
	cycle := p.Cycle
	serviceID := p.ServiceID
	d := Dialog{ServiceID: &serviceID, ValidityPeriodID: &periodID, ExpectedCycle: &cycle}
	return s.startDialogTx(ctx, tx, uid, chatID, DialogRenew, "renew_choose_precision", d)
}

// StartPeriodRenewDialog is the direct service entry used by the period detail
// button and /renew <period_id>.
func (s *Service) StartPeriodRenewDialog(ctx context.Context, uid, periodID, chatID int64) (Dialog, error) {
	var out Dialog
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var e error
		out, e = s.startRenewTarget(ctx, tx, uid, periodID, chatID)
		return e
	})
	return out, e
}

// startDialogTx shares the insert path between top-level and transactional
// callers. Ownership targets were already validated by the caller.
func (s *Service) startDialogTx(ctx context.Context, tx *sql.Tx, uid, chatID int64, action, step string, d Dialog) (Dialog, error) {
	if !dialogActions[action] || !validDialogStep(step) {
		return Dialog{}, ErrInvalid
	}
	d.Action, d.Step, d.UserID, d.ChatID, d.Draft = action, step, uid, chatID, "{}"
	var private *int64
	if e := tx.QueryRowContext(ctx, "SELECT private_chat_id FROM users WHERE id=?", uid).Scan(&private); e != nil {
		return Dialog{}, e
	}
	if private == nil || *private != chatID {
		return Dialog{}, ErrForbidden
	}
	if e := s.validateDialogTargets(ctx, tx, uid, d); e != nil {
		return Dialog{}, e
	}
	old, e := s.getDialog(ctx, tx, uid)
	if e == nil && sameDialogTarget(old, d) {
		return old, nil
	}
	if e != nil && e != ErrNotFound {
		return Dialog{}, e
	}
	now := s.now().Unix()
	d.ExpiresAt = now + dialogTTL
	d.CreatedAt = now
	_, e = tx.ExecContext(ctx, `INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,service_id,validity_period_id,rule_id,expected_cycle,expected_terms_revision,expected_revision,expires_at,created_at)
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(user_id) DO UPDATE SET chat_id=excluded.chat_id,action=excluded.action,step=excluded.step,draft_json=excluded.draft_json,service_id=excluded.service_id,validity_period_id=excluded.validity_period_id,rule_id=excluded.rule_id,expected_cycle=excluded.expected_cycle,expected_terms_revision=excluded.expected_terms_revision,expected_revision=excluded.expected_revision,expires_at=excluded.expires_at,created_at=excluded.created_at`,
		uid, chatID, action, step, d.Draft, d.ServiceID, d.ValidityPeriodID, d.RuleID, d.ExpectedCycle, d.ExpectedTermsRevision, d.ExpectedRevision, d.ExpiresAt, now)
	if e != nil {
		return Dialog{}, e
	}
	return d, nil
}

// RenewFromDialog commits the renewal using the cycle captured when the
// wizard started. The draft is irrelevant to the transaction: the caller must
// supply final values, and the cycle check rejects a concurrently advanced
// period while keeping the dialog row for correction.
func (s *Service) RenewFromDialog(ctx context.Context, uid int64, in RenewInput) (Renewal, error) {
	var out Renewal
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		d, e := s.getDialog(ctx, tx, uid)
		if e != nil {
			return e
		}
		if d.Action != DialogRenew || d.ValidityPeriodID == nil || d.ExpectedCycle == nil {
			return ErrConflict
		}
		// Re-check the parent inside the committing transaction: the service
		// may have been archived after the preview was rendered.
		p, e := s.getPeriod(ctx, tx, uid, *d.ValidityPeriodID)
		if e != nil {
			return e
		}
		parent, e := getServiceItem(ctx, tx, uid, p.ServiceID)
		if e != nil {
			return e
		}
		if parent.Status != "active" {
			return ErrConflict
		}
		in.ExpectedCycle = *d.ExpectedCycle
		out, e = s.renew(ctx, tx, uid, *d.ValidityPeriodID, in)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM bot_dialogs WHERE user_id=?", uid)
		return e
	})
	return out, e
}
