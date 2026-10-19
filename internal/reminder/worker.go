package reminder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"telegram.bot.remind/internal/domain"
	"telegram.bot.remind/internal/storage"
)

func safeCode(code string) string {
	switch code {
	case "rate_limited", "network", "remote_unavailable", "remote_rejected", "no_private_chat", "unknown":
		return code
	default:
		return "unknown"
	}
}

// retryDelay is 30s,1m,2m,4m,8m for attempts 1-5. Attempt 6 never schedules
// another try, so a larger shift is unreachable in normal operation.
func retryDelay(attempts int64) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 5 {
		attempts = 5
	}
	return 30 * time.Second * time.Duration(1<<uint(attempts-1))
}

func (s *Scheduler) deliver(parent context.Context, job *storage.ReminderDelivery) error {
	// Claiming commits before network I/O. Retain the lease if the database becomes
	// unavailable; recovery will retry rather than falsely recording a delivery.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	ok, err := s.store.CheckReminder(ctx, job.ID, job.Token, s.now(), s.allowed)
	if err != nil {
		cancel()
		return err
	}
	if !ok {
		cancel()
		// The storage layer either cancelled the job or left its lease for the
		// recovery sweep; in both cases nothing was sent.
		return nil
	}

	var messageID int64
	var deliveryErr error
	switch {
	case s.sender == nil:
		// Configuration mistakes are retryable, not silently recorded as sent.
		deliveryErr = &DeliveryError{Code: "unknown"}
	case job.ChatID == nil:
		deliveryErr = &DeliveryError{Code: "no_private_chat", Permanent: true}
	default:
		messageID, deliveryErr = s.sender.SendReminder(ctx, *job.ChatID, reminderText(job), job.ID)
	}
	cancel()
	now := s.now()
	var next *int64
	code := ""
	if deliveryErr != nil {
		code = "unknown"
		permanent := false
		delay := retryDelay(job.Attempts)
		var classified *DeliveryError
		if errors.As(deliveryErr, &classified) && classified != nil {
			code = safeCode(classified.Code)
			permanent = classified.Permanent || code == "no_private_chat"
			if code == "rate_limited" && classified.RetryAfter > delay {
				delay = classified.RetryAfter
			}
		} else if errors.Is(deliveryErr, context.DeadlineExceeded) || errors.Is(deliveryErr, context.Canceled) {
			code = "network"
		}
		if !permanent && job.Attempts < 6 {
			// Ceiling to whole seconds ensures a fractional RetryAfter cannot cause
			// an early retry in the integer-timestamp database.
			at := now.Add(delay)
			unix := at.Unix()
			if at.Nanosecond() != 0 {
				unix++
			}
			next = &unix
		}
	}
	// Result persistence must survive shutdown of the scan loop.
	resultCtx, resultCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer resultCancel()
	return s.store.FinishReminder(resultCtx, job.ID, job.Token, now, messageID, deliveryErr == nil, code, next)
}

func reminderText(job *storage.ReminderDelivery) string {
	exp := job.Expiration
	cutoff := exp.ExpiresAt.UTC().Format(time.RFC3339)
	if exp.DatePrecision == domain.PrecisionDate && exp.LocalExpiresDate != nil {
		cutoff = *exp.LocalExpiresDate + "（当日结束前有效，" + exp.Timezone + "）"
	} else if loc, err := time.LoadLocation(exp.Timezone); err == nil {
		cutoff = exp.ExpiresAt.In(loc).Format(time.RFC3339) + "（" + exp.Timezone + "）"
	}
	name := job.PeriodName
	if strings.TrimSpace(name) == "" {
		name = "有效期"
	}
	return fmt.Sprintf("有效期提醒\n服务：%s\n项目：%s\n截止：%s\n项目 ID：%d\n确认续期时请提供新的截止日期；不会自动延长有效期。", job.ServiceName, name, cutoff, job.PeriodID)
}
