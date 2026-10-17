package reminder

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"telegram.bot.remind/internal/storage"
)

// Sender is the only external delivery boundary used by the scheduler.
type Sender interface {
	SendReminder(ctx context.Context, chatID int64, text string, jobID int64) (messageID int64, err error)
}

// DeliveryError carries only a safe category, never SDK URLs or response bodies.
type DeliveryError struct {
	Code       string
	RetryAfter time.Duration
	Permanent  bool
}

func (e *DeliveryError) Error() string {
	if e == nil {
		return "unknown"
	}
	return safeCode(e.Code)
}

// Scheduler serializes scans and sends. Deploy exactly one scheduler per database.
// Cancellation stops acquisition immediately; an already acquired send and its
// result write use independent bounded contexts so shutdown can preserve facts.
type Scheduler struct {
	store    *storage.Store
	sender   Sender
	allowed  func(int64) bool
	interval time.Duration
	window   time.Duration
	now      func() time.Time
	mu       sync.Mutex
}

// New uses the same authorization predicate as Bot/API. A nil predicate denies
// everyone. Configuration validation belongs to the application configuration.
func New(store *storage.Store, sender Sender, allowed func(int64) bool, interval, catchupWindow time.Duration, now func() time.Time) *Scheduler {
	if now == nil {
		now = time.Now
	}
	if allowed == nil {
		allowed = func(int64) bool { return false }
	}
	return &Scheduler{store: store, sender: sender, allowed: allowed, interval: interval, window: catchupWindow, now: now}
}

// Run scans immediately and then once per configured interval. Its caller owns
// lifetime/error handling; storage failures are never mistaken for an empty queue.
func (s *Scheduler) Run(ctx context.Context) error {
	if s.interval <= 0 || s.window <= 0 {
		return errors.New("invalid reminder interval")
	}
	if err := s.Scan(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.Scan(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

// Scan recovers/generates jobs and drains due work, with no goroutine per job.
// Future jobs are persisted but never sent early. Rechecking after every send
// also handles a clock advance or a long external request.
func (s *Scheduler) Scan(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.window <= 0 {
		return errors.New("invalid reminder window")
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := s.store.ScanReminderJobs(ctx, s.now(), s.window, s.allowed); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		var read [16]byte
		if _, err := rand.Read(read[:]); err != nil {
			return errors.New("reminder lease token unavailable")
		}
		job, err := s.store.ClaimReminder(ctx, s.now(), s.window, s.allowed, hex.EncodeToString(read[:]))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if job == nil {
			return nil
		}
		if err := s.deliver(ctx, job); err != nil {
			return err
		}
	}
}
