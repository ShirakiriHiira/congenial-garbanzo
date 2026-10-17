package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"telegram.bot.remind/internal/service"
)

// optional preserves the distinction between an omitted field and explicit null.
type optional[T any] struct {
	Set   bool
	Value *T
}

func (o *optional[T]) UnmarshalJSON(raw []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		o.Value = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}
func (o optional[T]) serviceValue() service.Optional[T] {
	return service.Optional[T]{Set: o.Set, Value: o.Value}
}
func identifier(v int64) string { return strconv.FormatInt(v, 10) }
func timestamp(v int64) string  { return time.Unix(v, 0).UTC().Format(time.RFC3339) }
func nullableTime(v *int64) *string {
	if v == nil {
		return nil
	}
	text := timestamp(*v)
	return &text
}
func nullableID(v *int64) *string {
	if v == nil {
		return nil
	}
	text := identifier(*v)
	return &text
}

func userView(v service.User) map[string]any {
	return map[string]any{"id": identifier(v.ID), "telegram_user_id": identifier(v.TelegramUserID), "timezone": v.Timezone, "has_private_chat": v.PrivateChatID != nil, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
}
func simView(v service.SIM) map[string]any {
	return map[string]any{"id": identifier(v.ID), "user_id": identifier(v.UserID), "name": v.Name, "card_type": v.CardType, "operator": v.Operator, "phone_number": v.PhoneNumber, "country_or_region": v.CountryOrRegion, "iccid": v.ICCID, "notes": v.Notes, "status": v.Status, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
}
func periodView(v service.Period) map[string]any {
	return map[string]any{"id": identifier(v.ID), "sim_id": identifier(v.SIMID), "period_type": v.PeriodType, "name": v.Name, "starts_at": nullableTime(v.StartsAt), "expires_at": timestamp(v.ExpiresAt), "date_precision": v.DatePrecision, "local_expires_date": v.LocalExpiresDate, "timezone": v.Timezone, "status": v.Status, "cycle": v.Cycle, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
}
func ruleView(v service.Rule) map[string]any {
	return map[string]any{"id": identifier(v.ID), "validity_period_id": identifier(v.ValidityPeriodID), "days_before": v.DaysBefore, "reminder_time": v.ReminderTime, "enabled": v.Enabled, "revision": v.Revision, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
}
func renewalView(v service.Renewal) map[string]any {
	return map[string]any{"id": identifier(v.ID), "validity_period_id": identifier(v.ValidityPeriodID), "previous_cycle": v.PreviousCycle, "previous_expires_at": timestamp(v.PreviousExpiresAt), "new_expires_at": timestamp(v.NewExpiresAt), "previous_date_precision": v.PreviousDatePrecision, "new_date_precision": v.NewDatePrecision, "previous_local_expires_date": v.PreviousLocalExpiresDate, "new_local_expires_date": v.NewLocalExpiresDate, "timezone": v.Timezone, "renewed_at": timestamp(v.RenewedAt), "amount": v.Amount, "currency": v.Currency, "notes": v.Notes, "created_at": timestamp(v.CreatedAt)}
}
func jobView(v service.Job) map[string]any {
	return map[string]any{"id": identifier(v.ID), "validity_period_id": identifier(v.ValidityPeriodID), "reminder_rule_id": identifier(v.ReminderRuleID), "cycle": v.Cycle, "rule_revision": v.RuleRevision, "scheduled_at": timestamp(v.ScheduledAt), "expires_at_snapshot": timestamp(v.ExpiresAtSnapshot), "status": v.Status, "attempts": v.Attempts, "next_attempt_at": nullableTime(v.NextAttemptAt), "sent_at": nullableTime(v.SentAt), "telegram_message_id": nullableID(v.TelegramMessageID), "last_error_code": v.LastErrorCode, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
}
func page[T any](w http.ResponseWriter, p service.Page[T], err error, view func(T) map[string]any) {
	if err != nil {
		serviceError(w, err)
		return
	}
	data := make([]map[string]any, 0, len(p.Data))
	for _, v := range p.Data {
		data = append(data, view(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "next_cursor": cursor(p.NextCursor)})
}

func overviewView(v service.Overview) map[string]any {
	items := make([]map[string]any, 0, len(v.Items.Data))
	for _, item := range v.Items.Data {
		var period any
		if item.PrimaryPeriod != nil {
			period = periodView(*item.PrimaryPeriod)
		}
		items = append(items, map[string]any{"sim": simView(item.SIM), "primary_period": period})
	}
	return map[string]any{
		"counts": map[string]int{
			"total":     v.Counts.Total,
			"due_soon":  v.Counts.DueSoon,
			"expired":   v.Counts.Expired,
			"no_period": v.Counts.NoPeriod,
		},
		"items": items,
	}
}

type simCreateRequest struct {
	Name            string  `json:"name"`
	CardType        string  `json:"card_type"`
	Operator        *string `json:"operator"`
	PhoneNumber     *string `json:"phone_number"`
	CountryOrRegion *string `json:"country_or_region"`
	ICCID           *string `json:"iccid"`
	Notes           *string `json:"notes"`
}
type simPatchRequest struct {
	Name            *string          `json:"name"`
	CardType        *string          `json:"card_type"`
	Operator        optional[string] `json:"operator"`
	PhoneNumber     optional[string] `json:"phone_number"`
	CountryOrRegion optional[string] `json:"country_or_region"`
	ICCID           optional[string] `json:"iccid"`
	Notes           optional[string] `json:"notes"`
	Status          *string          `json:"status"`
}
type periodCreateRequest struct {
	PeriodType    string  `json:"period_type"`
	Name          string  `json:"name"`
	StartsAt      *string `json:"starts_at"`
	DatePrecision string  `json:"date_precision"`
	ExpiresAt     string  `json:"expires_at"`
}
type periodPatchRequest struct {
	ExpectedCycle int64            `json:"expected_cycle"`
	PeriodType    *string          `json:"period_type"`
	Name          *string          `json:"name"`
	StartsAt      optional[string] `json:"starts_at"`
	DatePrecision *string          `json:"date_precision"`
	ExpiresAt     *string          `json:"expires_at"`
}
type ruleCreateRequest struct {
	DaysBefore   *int   `json:"days_before"`
	ReminderTime string `json:"reminder_time"`
	Enabled      *bool  `json:"enabled"`
}
type rulePatchRequest struct {
	ExpectedRevision int64   `json:"expected_revision"`
	DaysBefore       *int    `json:"days_before"`
	ReminderTime     *string `json:"reminder_time"`
	Enabled          *bool   `json:"enabled"`
}
type renewalRequest struct {
	ExpectedCycle int64   `json:"expected_cycle"`
	DatePrecision string  `json:"date_precision"`
	ExpiresAt     string  `json:"expires_at"`
	Amount        *int64  `json:"amount"`
	Currency      *string `json:"currency"`
	Notes         *string `json:"notes"`
}
