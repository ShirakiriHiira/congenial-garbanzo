package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"telegram.bot.remind/internal/storage"
	"time"
	"unicode/utf8"
)

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

var (
	ErrInvalid     = &Error{"invalid_input"}
	ErrNotFound    = &Error{"not_found"}
	ErrConflict    = &Error{"conflict"}
	ErrUnavailable = &Error{"unavailable"}
	ErrForbidden   = &Error{"forbidden"}
)

func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "unavailable"
}
func dbError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return ErrUnavailable
}

type Service struct {
	store    *storage.Store
	timezone string
	allowed  func(int64) bool
	now      func() time.Time
}

func New(store *storage.Store, timezone string, allowed func(int64) bool, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store, timezone, allowed, now}
}
func (s *Service) Allowed(id int64) bool { return id > 0 && s.allowed != nil && s.allowed(id) }

type User struct {
	ID                int64
	TelegramUserID    int64
	Timezone          string
	PrivateChatID     *int64
	SessionTTLSeconds *int64
	// MenuButtonURL is the Mini App menu button URL already installed for this
	// user's private chat; nil means no install has succeeded yet.
	MenuButtonURL *string
	CreatedAt     int64
	UpdatedAt     int64
}
type SIMDetails struct {
	CardType        string
	PhoneNumber     *string
	CountryOrRegion *string
	ICCID           *string
}

type ServiceItem struct {
	ID            int64
	UserID        int64
	Name          string
	CategoryID    int64
	Provider      *string
	Identifier    *string
	ManagementURL *string
	Notes         *string
	Status        string
	CreatedAt     int64
	UpdatedAt     int64
	SIMDetails    *SIMDetails
}

type Category struct {
	ID        int64
	UserID    *int64
	Code      *string
	Name      string
	Kind      string
	CreatedAt int64
	UpdatedAt int64
}

type PeriodTerms struct {
	Mode          string
	IntervalCount *int64
	IntervalUnit  *string
	Amount        *int64
	Currency      *string
	Revision      int64
}

type Period struct {
	ID               int64
	ServiceID        int64
	Name             string
	StartsAt         *int64
	ExpiresAt        int64
	DatePrecision    string
	LocalExpiresDate *string
	Timezone         string
	Status           string
	Cycle            int64
	CreatedAt        int64
	UpdatedAt        int64
	Terms            PeriodTerms
}

type Rule struct {
	ID           int64
	PeriodID     int64
	DaysBefore   int
	ReminderTime string
	Enabled      bool
	Revision     int64
	CreatedAt    int64
	UpdatedAt    int64
}
type Renewal struct {
	ID                       int64
	PeriodID                 int64
	PreviousCycle            int64
	PreviousExpiresAt        int64
	NewExpiresAt             int64
	PreviousDatePrecision    string
	NewDatePrecision         string
	PreviousLocalExpiresDate *string
	NewLocalExpiresDate      *string
	Timezone                 string
	RenewedAt                int64
	Amount                   *int64
	Currency                 *string
	Notes                    *string
	CreatedAt                int64
	Replayed                 bool
}
type Job struct {
	ID                int64
	PeriodID          int64
	ReminderRuleID    int64
	Cycle             int64
	RuleRevision      int64
	ScheduledAt       int64
	ExpiresAtSnapshot int64
	Status            string
	Attempts          int
	NextAttemptAt     *int64
	LockedUntil       *int64
	AttemptToken      *string
	SentAt            *int64
	TelegramMessageID *int64
	LastErrorCode     *string
	CreatedAt         int64
	UpdatedAt         int64
}

// Dialog is the single durable wizard state for one user. Optional targets
// point at the object a flow operates on; validation never trusts them.
type Dialog struct {
	UserID                int64
	ChatID                int64
	Action                string
	Step                  string
	Draft                 string
	ServiceID             *int64
	PeriodID              *int64
	RuleID                *int64
	ExpectedCycle         *int64
	ExpectedTermsRevision *int64
	ExpectedRevision      *int64
	ExpiresAt             int64
	CreatedAt             int64
}

// OptionalString distinguishes absent fields from explicit null in partial
// updates. An absent field leaves the stored value untouched; an explicit null
// clears it.
type OptionalString struct {
	Set   bool
	Value *string
}

type SIMDetailsPatch struct {
	CardType        OptionalString
	PhoneNumber     OptionalString
	CountryOrRegion OptionalString
	ICCID           OptionalString
}

type ServiceInput struct {
	Name          string
	CategoryID    int64
	Provider      *string
	Identifier    *string
	ManagementURL *string
	Notes         *string
	SIMDetails    *SIMDetails
}
type ServicePatch struct {
	Name          *string
	CategoryID    *int64
	Provider      OptionalString
	Identifier    OptionalString
	ManagementURL OptionalString
	Notes         OptionalString
	Status        *string
	// SIMDetailsSet marks that the SIM detail block was part of the request.
	// SIMDetails carries the new values; a nil value with SIMDetailsSet means
	// the block was explicitly null.
	SIMDetailsSet bool
	SIMDetails    *SIMDetailsPatch
}

type TermsInput struct {
	Mode          string
	IntervalCount *int64
	IntervalUnit  *string
	Amount        *int64
	Currency      *string
}
type PeriodInput struct {
	Name          string
	StartsAt      *string
	DatePrecision string
	ExpiresAt     string
	Terms         *TermsInput
}
type PeriodPatch struct {
	ExpectedCycle         int64
	Name                  *string
	StartsAt              OptionalString
	DatePrecision         *string
	ExpiresAt             *string
	ExpectedTermsRevision *int64
	// TermsSet marks that the terms were part of the request. Terms carries
	// the new values; a nil value with TermsSet means "reset to the default
	// terms", which is distinct from an absent request.
	TermsSet bool
	Terms    *TermsInput
}
type RuleInput struct {
	DaysBefore   int
	ReminderTime string
	Enabled      *bool
}
type RulePatch struct {
	ExpectedRevision int64
	DaysBefore       *int
	ReminderTime     *string
	Enabled          *bool
}
type RenewInput struct {
	ExpectedCycle int64
	DatePrecision string
	ExpiresAt     string
	Amount        *int64
	Currency      *string
	Notes         *string
}

type ServiceFilter struct {
	Query      string
	CategoryID *int64
	Status     string
}
type ServiceOverviewItem struct {
	Service       ServiceItem
	PrimaryPeriod *Period
	Periods       []Period
}
type ServiceOverview struct {
	Counts OverviewCounts
	Items  Page[ServiceOverviewItem]
}

type Page[T any] struct {
	Data       []T
	NextCursor *int64
}

// rowQuerier is the single SQL read path shared by *sql.DB and *sql.Tx.
type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type scanner interface{ Scan(...any) error }

func (s *Service) transaction(ctx context.Context, uid int64, fn func(*sql.Tx) error) error {
	tx, e := s.store.DB.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	if e = s.authorize(ctx, tx, uid); e != nil {
		return e
	}
	if e = fn(tx); e != nil {
		return dbError(e)
	}
	return dbError(tx.Commit())
}
func (s *Service) authorize(ctx context.Context, q rowQuerier, uid int64) error {
	var tid int64
	if e := q.QueryRowContext(ctx, "SELECT telegram_user_id FROM users WHERE id=?", uid).Scan(&tid); e != nil {
		return dbError(e)
	}
	if !s.Allowed(tid) {
		return ErrForbidden
	}
	return nil
}
func (s *Service) Authenticate(ctx context.Context, tid int64, privateChatID *int64) (User, error) {
	var u User
	if !s.Allowed(tid) {
		return u, ErrForbidden
	}
	if privateChatID != nil && *privateChatID != tid {
		return u, ErrInvalid
	}
	now := s.now().Unix()
	_, e := s.store.DB.ExecContext(ctx, `INSERT INTO users(telegram_user_id,timezone,private_chat_id,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(telegram_user_id) DO UPDATE SET private_chat_id=COALESCE(excluded.private_chat_id,users.private_chat_id),updated_at=excluded.updated_at`, tid, s.timezone, privateChatID, now, now)
	if e != nil {
		return u, ErrUnavailable
	}
	e = s.store.DB.QueryRowContext(ctx, "SELECT id,telegram_user_id,timezone,private_chat_id,session_ttl_seconds,menu_button_url,created_at,updated_at FROM users WHERE telegram_user_id=?", tid).Scan(&u.ID, &u.TelegramUserID, &u.Timezone, &u.PrivateChatID, &u.SessionTTLSeconds, &u.MenuButtonURL, &u.CreatedAt, &u.UpdatedAt)
	return u, dbError(e)
}
func (s *Service) Me(ctx context.Context, uid int64) (User, error) {
	var u User
	if e := s.authorize(ctx, s.store.DB, uid); e != nil {
		return u, e
	}
	e := s.store.DB.QueryRowContext(ctx, "SELECT id,telegram_user_id,timezone,private_chat_id,session_ttl_seconds,menu_button_url,created_at,updated_at FROM users WHERE id=?", uid).Scan(&u.ID, &u.TelegramUserID, &u.Timezone, &u.PrivateChatID, &u.SessionTTLSeconds, &u.MenuButtonURL, &u.CreatedAt, &u.UpdatedAt)
	return u, dbError(e)
}

// SetMenuButtonURL records the menu button URL installed for this chat so /start
// does not repeat the API call on every message.
func (s *Service) SetMenuButtonURL(ctx context.Context, uid int64, url string) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE users SET menu_button_url=?,updated_at=? WHERE id=?", url, s.now().Unix(), uid)
		return e
	})
}

// UserByTelegramID looks up an existing account without creating one. The API
// uses it to read the per-user session window before checking initData age.
func (s *Service) UserByTelegramID(ctx context.Context, tid int64) (User, bool, error) {
	var u User
	e := s.store.DB.QueryRowContext(ctx, "SELECT id,telegram_user_id,timezone,private_chat_id,session_ttl_seconds,menu_button_url,created_at,updated_at FROM users WHERE telegram_user_id=?", tid).Scan(&u.ID, &u.TelegramUserID, &u.Timezone, &u.PrivateChatID, &u.SessionTTLSeconds, &u.MenuButtonURL, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return u, false, nil
	}
	if e != nil {
		return u, false, dbError(e)
	}
	return u, true, nil
}
func text(v string, max int, required bool) bool {
	return utf8.ValidString(v) && utf8.RuneCountInString(v) <= max && (!required || strings.TrimSpace(v) != "")
}
func optionalText(v *string, max int) bool { return v == nil || text(*v, max, false) }
func status(exp, now int64) string {
	if now >= exp {
		return "expired"
	}
	return "active"
}
func pageArgs(after int64, limit int) error {
	if after < 0 || limit < 1 || limit > 100 {
		return ErrInvalid
	}
	return nil
}
func finishPage[T any](data []T, limit int, id func(T) int64) Page[T] {
	p := Page[T]{Data: data}
	if len(data) > limit {
		v := id(data[limit-1])
		p.NextCursor = &v
		p.Data = data[:limit]
	}
	return p
}
func cancelJobs(ctx context.Context, tx *sql.Tx, period int64, rule *int64, now int64) error {
	query := `UPDATE reminder_jobs SET status='cancelled',next_attempt_at=NULL,updated_at=? WHERE period_id=? AND status IN ('pending','failed','sending')`
	args := []any{now, period}
	if rule != nil {
		query += " AND reminder_rule_id=?"
		args = append(args, *rule)
	}
	_, e := tx.ExecContext(ctx, query, args...)
	return e
}
