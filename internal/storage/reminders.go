package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"telegram.bot.remind/internal/domain"
)

// ReminderDelivery is the immutable payload captured when a job is leased.
// The attempt token fences every result write, including cancelled in-flight jobs.
type ReminderDelivery struct {
	ID, PeriodID, RuleID, Cycle, Revision, Attempts int64
	TelegramUserID                                  int64
	ChatID                                          *int64
	ServiceName, PeriodName                         string
	Expiration                                      domain.Expiration
	Token                                           string
}

// ErrReminderStore hides SQL details from scheduler logs and API responses.
var ErrReminderStore = errors.New("reminder storage unavailable")

const reminderJoins = ` FROM reminder_jobs j
    JOIN validity_periods p ON p.id=j.validity_period_id
    JOIN reminder_rules r ON r.id=j.reminder_rule_id AND r.validity_period_id=p.id
    JOIN service_items s ON s.id=p.service_id
    JOIN users u ON u.id=s.user_id `

// A job is current only while every ownership and version gate still matches.
const currentReminder = `p.status='active' AND s.status='active' AND r.enabled=1
	AND p.cycle=j.cycle AND r.revision=j.rule_revision
	AND p.expires_at=j.expires_at_snapshot`

// Delivery identity is the real deduplication key: a rule time change or a
// recovered card must not resend a reminder already delivered for this cutoff.
const unsentReminder = `NOT EXISTS (
	SELECT 1 FROM reminder_jobs sent
	WHERE sent.validity_period_id=j.validity_period_id
	  AND sent.reminder_rule_id=j.reminder_rule_id
	  AND sent.expires_at_snapshot=j.expires_at_snapshot
	  AND sent.status='sent')`

// At most one lease may exist for a period cycle or a rule. A cancelled
// in-flight job intentionally keeps its token until the result is recorded.
const noReminderLease = `NOT EXISTS (
	SELECT 1 FROM reminder_jobs leased
	WHERE leased.validity_period_id=j.validity_period_id
	  AND leased.cycle=j.cycle AND leased.id<>j.id
	  AND leased.attempt_token IS NOT NULL AND leased.locked_until>?)
	AND NOT EXISTS (
	SELECT 1 FROM reminder_jobs leased
	WHERE leased.reminder_rule_id=j.reminder_rule_id AND leased.id<>j.id
	  AND leased.attempt_token IS NOT NULL AND leased.locked_until>?)`

func permit(allowed func(int64) bool, telegramUserID int64) bool {
	return allowed != nil && allowed(telegramUserID)
}

func (s *Store) reminderTransaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ErrReminderStore
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return ErrReminderStore
	}
	if err = tx.Commit(); err != nil {
		return ErrReminderStore
	}
	return nil
}

// ScanReminderJobs atomically synchronizes expiration, revokes unauthorized
// work, recovers expired leases, generates current-cycle jobs and applies the
// catch-up policy. Network calls never run inside this transaction.
func (s *Store) ScanReminderJobs(ctx context.Context, now time.Time, window time.Duration, allowed func(int64) bool) error {
	n := now.Unix()
	return s.reminderTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE validity_periods SET status='expired',updated_at=? WHERE status='active' AND expires_at<=?`,
			n, n); err != nil {
			return err
		}

		rows, err := tx.QueryContext(ctx, `SELECT id,telegram_user_id FROM users`)
		if err != nil {
			return err
		}
		var denied []int64
		for rows.Next() {
			var id, telegramID int64
			if err := rows.Scan(&id, &telegramID); err != nil {
				rows.Close()
				return err
			}
			if !permit(allowed, telegramID) {
				denied = append(denied, id)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range denied {
			if _, err := tx.ExecContext(ctx,
				`UPDATE reminder_jobs SET status='cancelled',next_attempt_at=NULL,last_error_code='unauthorized',updated_at=?
				 WHERE (status IN ('pending','sending') OR (status='failed' AND next_attempt_at IS NOT NULL))
				   AND validity_period_id IN (
                    SELECT p.id FROM validity_periods p JOIN service_items s ON s.id=p.service_id WHERE s.user_id=?)`,
				n, id); err != nil {
				return err
			}
		}

		// A lease whose worker disappeared is retryable until the attempt cap.
		if _, err := tx.ExecContext(ctx,
			`UPDATE reminder_jobs
			 SET status='failed',
			     next_attempt_at=CASE WHEN attempts<6 THEN ? ELSE NULL END,
			     locked_until=NULL,attempt_token=NULL,last_error_code='lease_expired',updated_at=?
			 WHERE status='sending' AND locked_until<=?`,
			n, n, n); err != nil {
			return err
		}
		// A cancelled lease is never revived; it only needs its token cleared.
		if _, err := tx.ExecContext(ctx,
			`UPDATE reminder_jobs SET locked_until=NULL,attempt_token=NULL,updated_at=?
			 WHERE status='cancelled' AND locked_until<=?`,
			n, n); err != nil {
			return err
		}

		type candidate struct {
			period, rule, cycle, revision, days, telegramID int64
			clock                                           string
			exp                                             domain.Expiration
		}
		rows, err = tx.QueryContext(ctx, `
			SELECT p.id,r.id,p.cycle,r.revision,r.days_before,r.reminder_time,
			       p.expires_at,p.date_precision,p.local_expires_date,p.timezone,u.telegram_user_id
			FROM validity_periods p
			JOIN reminder_rules r ON r.validity_period_id=p.id
            JOIN service_items s ON s.id=p.service_id
            JOIN users u ON u.id=s.user_id
            WHERE p.status='active' AND p.expires_at>? AND s.status='active' AND r.enabled=1`,
			n)
		if err != nil {
			return err
		}
		var candidates []candidate
		for rows.Next() {
			var c candidate
			var expiry int64
			if err := rows.Scan(&c.period, &c.rule, &c.cycle, &c.revision, &c.days, &c.clock,
				&expiry, &c.exp.DatePrecision, &c.exp.LocalExpiresDate, &c.exp.Timezone,
				&c.telegramID); err != nil {
				rows.Close()
				return err
			}
			c.exp.ExpiresAt = time.Unix(expiry, 0).UTC()
			if permit(allowed, c.telegramID) {
				candidates = append(candidates, c)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, c := range candidates {
			at, err := domain.ReminderAt(c.exp, c.days, c.clock)
			if err != nil {
				// Service validation keeps this unreachable for accepted rules.
				// Skipping one stale row must not block every user's reminders.
				continue
			}

			// Existing rows of any status keep their unique key, so a user
			// cancellation or permanent failure is never undone by a later scan.
			// A successful delivery for this cutoff blocks reinstatement too.
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO reminder_jobs(
					validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,
					expires_at_snapshot,status,attempts,next_attempt_at,created_at,updated_at)
				 SELECT ?,?,?,?,?,?,'pending',0,?,?,?
				 WHERE NOT EXISTS (
					SELECT 1 FROM reminder_jobs
					WHERE validity_period_id=? AND reminder_rule_id=? AND cycle=? AND rule_revision=?)
				   AND NOT EXISTS (
					SELECT 1 FROM reminder_jobs
					WHERE validity_period_id=? AND reminder_rule_id=?
					  AND expires_at_snapshot=? AND status='sent')`,
				c.period, c.rule, c.cycle, c.revision, at.Unix(), c.exp.ExpiresAt.Unix(),
				at.Unix(), n, n,
				c.period, c.rule, c.cycle, c.revision,
				c.period, c.rule, c.exp.ExpiresAt.Unix()); err != nil {
				return err
			}
		}

		// Cancel invalid versions and terminal catch-up candidates before ranking.
		if _, err := tx.ExecContext(ctx,
			`UPDATE reminder_jobs
			 SET status='cancelled',next_attempt_at=NULL,
			     last_error_code=CASE WHEN expires_at_snapshot<=? THEN 'expired'
			                           WHEN scheduled_at<? THEN 'missed_window'
			                           ELSE 'obsolete' END,
			     updated_at=?
			 WHERE status IN ('pending','failed') AND next_attempt_at IS NOT NULL
			   AND (expires_at_snapshot<=? OR scheduled_at<?
			        OR id IN (SELECT j.id`+reminderJoins+`
			                  WHERE NOT (`+currentReminder+`) OR NOT (`+unsentReminder+`)))`,
			n, now.Add(-window).Unix(), n, n, now.Add(-window).Unix()); err != nil {
			return err
		}

		// For each period cycle, only the newest due candidate is delivered.
		// Older failed retries and newly due tasks collapse into that one send.
		if _, err := tx.ExecContext(ctx,
			`UPDATE reminder_jobs
			 SET status='cancelled',next_attempt_at=NULL,last_error_code='superseded_catchup',updated_at=?
			 WHERE id IN (
				SELECT id FROM (
					SELECT id,ROW_NUMBER() OVER(
						PARTITION BY validity_period_id,cycle
						ORDER BY scheduled_at DESC,id DESC) AS position
					FROM reminder_jobs
					WHERE status IN ('pending','failed') AND next_attempt_at IS NOT NULL
					  AND scheduled_at<=?)
				WHERE position>1)`,
			n, n); err != nil {
			return err
		}
		return nil
	})
}

// ClaimReminder selects one due current job and commits its 60-second lease.
// A zero result means no eligible work, not a storage failure.
func (s *Store) ClaimReminder(ctx context.Context, now time.Time, window time.Duration, allowed func(int64) bool, token string) (*ReminderDelivery, error) {
	var out *ReminderDelivery
	err := s.reminderTransaction(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT j.id,u.telegram_user_id`+reminderJoins+`
			WHERE j.status IN ('pending','failed')
			  AND j.next_attempt_at<=? AND j.scheduled_at<=? AND j.scheduled_at>=?
			  AND j.expires_at_snapshot>? AND j.attempts<6
			  AND `+currentReminder+` AND `+unsentReminder+` AND `+noReminderLease+`
			ORDER BY j.next_attempt_at,j.id`,
			now.Unix(), now.Unix(), now.Add(-window).Unix(), now.Unix(), now.Unix(), now.Unix())
		if err != nil {
			return err
		}
		var id int64
		for rows.Next() {
			var candidate, telegramID int64
			if err := rows.Scan(&candidate, &telegramID); err != nil {
				rows.Close()
				return err
			}
			if permit(allowed, telegramID) {
				id = candidate
				break
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if id == 0 {
			return nil
		}
		result, err := tx.ExecContext(ctx,
			`UPDATE reminder_jobs
			 SET status='sending',attempts=attempts+1,locked_until=?,attempt_token=?,updated_at=?
			 WHERE id=? AND status IN ('pending','failed') AND next_attempt_at<=?`,
			now.Add(time.Minute).Unix(), token, now.Unix(), id, now.Unix())
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return nil
		}
		v, err := readReminderDelivery(ctx, tx, id, token)
		if err != nil {
			return err
		}
		out = &v
		return nil
	})
	return out, err
}

func readReminderDelivery(ctx context.Context, tx *sql.Tx, id int64, token string) (ReminderDelivery, error) {
	var v ReminderDelivery
	var expiry int64
	err := tx.QueryRowContext(ctx, `
        SELECT j.id,j.validity_period_id,j.reminder_rule_id,j.cycle,j.rule_revision,j.attempts,
               u.telegram_user_id,u.private_chat_id,s.name,p.name,j.expires_at_snapshot,
               p.date_precision,p.local_expires_date,p.timezone,j.attempt_token`+reminderJoins+`
        WHERE j.id=? AND j.attempt_token=?`, id, token).
		Scan(&v.ID, &v.PeriodID, &v.RuleID, &v.Cycle, &v.Revision, &v.Attempts,
			&v.TelegramUserID, &v.ChatID, &v.ServiceName, &v.PeriodName, &expiry,
			&v.Expiration.DatePrecision, &v.Expiration.LocalExpiresDate,
			&v.Expiration.Timezone, &v.Token)
	v.Expiration.ExpiresAt = time.Unix(expiry, 0).UTC()
	return v, err
}

// CheckReminder rechecks cancellation, permissions and versions immediately
// before sending. It cannot eliminate a cancellation racing with the external
// request; FinishReminder records that outcome truthfully. An expired lease is
// left untouched for the scan recovery path to make retryable.
func (s *Store) CheckReminder(ctx context.Context, id int64, token string, now time.Time, allowed func(int64) bool) (bool, error) {
	ok := false
	err := s.reminderTransaction(ctx, func(tx *sql.Tx) error {
		var telegramID int64
		var lockedUntil sql.NullInt64
		var status string
		var current bool
		err := tx.QueryRowContext(ctx, `
			SELECT u.telegram_user_id,j.status,j.locked_until,
			       ((`+currentReminder+`) AND (`+unsentReminder+`) AND j.expires_at_snapshot>?)`+reminderJoins+`
			WHERE j.id=? AND j.attempt_token=?`,
			now.Unix(), id, token).Scan(&telegramID, &status, &lockedUntil, &current)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if status == "cancelled" {
			// The user already cancelled; send nothing and release the unused lease.
			_, err = tx.ExecContext(ctx,
				`UPDATE reminder_jobs SET locked_until=NULL,attempt_token=NULL,updated_at=?
				 WHERE id=? AND attempt_token=? AND status='cancelled'`,
				now.Unix(), id, token)
			return err
		}
		if status != "sending" || !lockedUntil.Valid || lockedUntil.Int64 <= now.Unix() {
			// Let the next scan recover the lease instead of cancelling the job.
			return nil
		}
		code := ""
		switch {
		case !permit(allowed, telegramID):
			code = "unauthorized"
		case !current:
			code = "obsolete"
		default:
			ok = true
			return nil
		}
		// No external request was made, so this job can be cancelled safely.
		_, err = tx.ExecContext(ctx,
			`UPDATE reminder_jobs
			 SET status='cancelled',next_attempt_at=NULL,locked_until=NULL,attempt_token=NULL,
			     last_error_code=?,updated_at=?
			 WHERE id=? AND attempt_token=? AND status='sending'`,
			code, now.Unix(), id, token)
		return err
	})
	return ok, err
}

// FinishReminder records success even when cancellation raced with delivery.
// A failed attempt for an already cancelled job stays cancelled and is not
// retried. A stale token never changes a row.
func (s *Store) FinishReminder(ctx context.Context, id int64, token string, now time.Time, messageID int64, success bool, code string, next *int64) error {
	var err error
	if success {
		_, err = s.DB.ExecContext(ctx,
			`UPDATE reminder_jobs
			 SET status='sent',sent_at=?,telegram_message_id=?,next_attempt_at=NULL,
			     locked_until=NULL,attempt_token=NULL,last_error_code=NULL,updated_at=?
			 WHERE id=? AND attempt_token=? AND status IN ('sending','cancelled')`,
			now.Unix(), messageID, now.Unix(), id, token)
	} else {
		_, err = s.DB.ExecContext(ctx,
			`UPDATE reminder_jobs
			 SET next_attempt_at=CASE WHEN status='cancelled' THEN NULL ELSE ? END,
			     last_error_code=CASE WHEN status='cancelled' THEN last_error_code ELSE ? END,
			     status=CASE WHEN status='cancelled' THEN 'cancelled' ELSE 'failed' END,
			     locked_until=NULL,attempt_token=NULL,updated_at=?
			 WHERE id=? AND attempt_token=? AND status IN ('sending','cancelled')`,
			next, code, now.Unix(), id, token)
	}
	if err != nil {
		return ErrReminderStore
	}
	return nil
}
