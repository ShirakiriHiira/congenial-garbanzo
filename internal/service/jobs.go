package service

import (
	"context"
	"database/sql"
)

const jobColumns = `j.id,j.validity_period_id,j.reminder_rule_id,j.cycle,j.rule_revision,j.scheduled_at,j.expires_at_snapshot,j.status,j.attempts,j.next_attempt_at,j.locked_until,j.attempt_token,j.sent_at,j.telegram_message_id,j.last_error_code,j.created_at,j.updated_at`

func scanJob(row scanner) (Job, error) {
	var v Job
	e := row.Scan(&v.ID, &v.ValidityPeriodID, &v.ReminderRuleID, &v.Cycle, &v.RuleRevision, &v.ScheduledAt, &v.ExpiresAtSnapshot, &v.Status, &v.Attempts, &v.NextAttemptAt, &v.LockedUntil, &v.AttemptToken, &v.SentAt, &v.TelegramMessageID, &v.LastErrorCode, &v.CreatedAt, &v.UpdatedAt)
	return v, e
}
func getJob(ctx context.Context, q queryer, uid, id int64) (Job, error) {
	v, e := scanJob(q.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM reminder_jobs j JOIN validity_periods p ON p.id=j.validity_period_id JOIN service_items s ON s.id=p.service_id WHERE s.user_id=? AND j.id=?", uid, id))
	return v, dbError(e)
}
func (s *Service) GetJob(ctx context.Context, uid, id int64) (Job, error) {
	if e := s.authorize(ctx, s.store.DB, uid); e != nil {
		return Job{}, e
	}
	return getJob(ctx, s.store.DB, uid, id)
}
func (s *Service) ListJobs(ctx context.Context, uid int64, filter string, after int64, limit int) (Page[Job], error) {
	out := Page[Job]{Data: []Job{}}
	if e := pageArgs(after, limit); e != nil {
		return out, e
	}
	switch filter {
	case "", "all", "pending", "sending", "sent", "failed", "cancelled":
	default:
		return out, ErrInvalid
	}
	if e := s.authorize(ctx, s.store.DB, uid); e != nil {
		return out, e
	}
	query := "SELECT " + jobColumns + " FROM reminder_jobs j JOIN validity_periods p ON p.id=j.validity_period_id JOIN service_items s ON s.id=p.service_id WHERE s.user_id=? AND j.id>?"
	args := []any{uid, after}
	if filter != "" && filter != "all" {
		query += " AND j.status=?"
		args = append(args, filter)
	}
	query += " ORDER BY j.id LIMIT ?"
	args = append(args, limit+1)
	rows, e := s.store.DB.QueryContext(ctx, query, args...)
	if e != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanJob(rows)
		if e != nil {
			return out, ErrUnavailable
		}
		out.Data = append(out.Data, v)
	}
	return finishPage(out.Data, limit, func(v Job) int64 { return v.ID }), dbError(rows.Err())
}
func (s *Service) CancelJob(ctx context.Context, uid, id int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		v, e := getJob(ctx, tx, uid, id)
		if e != nil {
			return e
		}
		if v.Status == "sent" {
			return ErrConflict
		}
		_, e = tx.ExecContext(ctx, "UPDATE reminder_jobs SET status='cancelled',next_attempt_at=NULL,updated_at=? WHERE id=?", s.now().Unix(), id)
		return e
	})
}
func scanDialog(row scanner) (Dialog, error) {
	var d Dialog
	e := row.Scan(&d.UserID, &d.ChatID, &d.Action, &d.ValidityPeriodID, &d.ExpectedCycle, &d.ExpiresAt, &d.CreatedAt)
	return d, e
}
func (s *Service) getDialog(ctx context.Context, q queryer, uid int64) (Dialog, error) {
	d, e := scanDialog(q.QueryRowContext(ctx, `SELECT d.user_id,d.chat_id,d.action,d.validity_period_id,d.expected_cycle,d.expires_at,d.created_at FROM bot_dialogs d JOIN validity_periods p ON p.id=d.validity_period_id JOIN service_items s ON s.id=p.service_id WHERE d.user_id=? AND s.user_id=?`, uid, uid))
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

// PeriodForJob is the single ownership and cycle gate shared by both callback actions.
func (s *Service) PeriodForJob(ctx context.Context, uid, jid int64) (Period, error) {
	var p Period
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		j, e := getJob(ctx, tx, uid, jid)
		if e != nil {
			return e
		}
		p, e = s.getPeriod(ctx, tx, uid, j.ValidityPeriodID)
		if e != nil {
			return e
		}
		parent, e := getServiceItem(ctx, tx, uid, p.ServiceID)
		if e != nil {
			return e
		}
		if p.Cycle != j.Cycle || p.Status == "cancelled" || parent.Status != "active" {
			return ErrConflict
		}
		return nil
	})
	return p, e
}
func (s *Service) StartRenewDialog(ctx context.Context, uid, jid, chatID int64) (Dialog, error) {
	var out Dialog
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var private *int64
		if e := tx.QueryRowContext(ctx, "SELECT private_chat_id FROM users WHERE id=?", uid).Scan(&private); e != nil {
			return e
		}
		if private == nil || *private != chatID {
			return ErrForbidden
		}
		j, e := getJob(ctx, tx, uid, jid)
		if e != nil {
			return e
		}
		p, e := s.getPeriod(ctx, tx, uid, j.ValidityPeriodID)
		if e != nil {
			return e
		}
		parent, e := getServiceItem(ctx, tx, uid, p.ServiceID)
		if e != nil {
			return e
		}
		if p.Cycle != j.Cycle || p.Status == "cancelled" || parent.Status != "active" {
			return ErrConflict
		}
		old, e := s.getDialog(ctx, tx, uid)
		if e == nil && old.ValidityPeriodID == p.ID && old.ExpectedCycle == p.Cycle && old.ChatID == chatID {
			out = old
			return nil
		}
		if e != nil && e != ErrNotFound {
			return e
		}
		now := s.now().Unix()
		out = Dialog{UserID: uid, ChatID: chatID, Action: "renew", ValidityPeriodID: p.ID, ExpectedCycle: p.Cycle, ExpiresAt: now + 900, CreatedAt: now}
		_, e = tx.ExecContext(ctx, `INSERT INTO bot_dialogs(user_id,chat_id,action,validity_period_id,expected_cycle,expires_at,created_at) VALUES(?,?,'renew',?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET chat_id=excluded.chat_id,action=excluded.action,validity_period_id=excluded.validity_period_id,expected_cycle=excluded.expected_cycle,expires_at=excluded.expires_at,created_at=excluded.created_at`, uid, chatID, p.ID, p.Cycle, out.ExpiresAt, now)
		return e
	})
	return out, e
}
func (s *Service) RenewFromDialog(ctx context.Context, uid int64, in RenewInput) (Renewal, error) {
	var out Renewal
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		d, e := s.getDialog(ctx, tx, uid)
		if e != nil {
			return e
		}
		in.ExpectedCycle = d.ExpectedCycle
		out, e = s.renew(ctx, tx, uid, d.ValidityPeriodID, in)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM bot_dialogs WHERE user_id=?", uid)
		return e
	})
	return out, e
}
