package service

import (
	"context"
	"database/sql"
)

const jobColumns = `j.id,j.period_id,j.reminder_rule_id,j.cycle,j.rule_revision,j.scheduled_at,j.expires_at_snapshot,j.status,j.attempts,j.next_attempt_at,j.locked_until,j.attempt_token,j.sent_at,j.telegram_message_id,j.last_error_code,j.created_at,j.updated_at`

func scanJob(row scanner) (Job, error) {
	var v Job
	e := row.Scan(&v.ID, &v.PeriodID, &v.ReminderRuleID, &v.Cycle, &v.RuleRevision, &v.ScheduledAt, &v.ExpiresAtSnapshot, &v.Status, &v.Attempts, &v.NextAttemptAt, &v.LockedUntil, &v.AttemptToken, &v.SentAt, &v.TelegramMessageID, &v.LastErrorCode, &v.CreatedAt, &v.UpdatedAt)
	return v, e
}
func getJob(ctx context.Context, q rowQuerier, uid, id int64) (Job, error) {
	v, e := scanJob(q.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM reminder_jobs j JOIN service_periods p ON p.id=j.period_id JOIN service_items s ON s.id=p.service_id WHERE s.user_id=? AND j.id=?", uid, id))
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
	query := "SELECT " + jobColumns + " FROM reminder_jobs j JOIN service_periods p ON p.id=j.period_id JOIN service_items s ON s.id=p.service_id WHERE s.user_id=? AND j.id>?"
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

// CancelJob cancels a task that has not been delivered. A cancelled task is
// idempotent; sent work cannot be taken back.
func (s *Service) CancelJob(ctx context.Context, uid, id int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		v, e := getJob(ctx, tx, uid, id)
		if e != nil {
			return e
		}
		switch v.Status {
		case "sent":
			return ErrConflict
		case "cancelled":
			return nil
		}
		_, e = tx.ExecContext(ctx, "UPDATE reminder_jobs SET status='cancelled',next_attempt_at=NULL,updated_at=? WHERE id=?", s.now().Unix(), id)
		return e
	})
}
