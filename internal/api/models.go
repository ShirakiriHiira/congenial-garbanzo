package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"telegram.bot.remind/internal/service"
)

// optionalString preserves the distinction between an omitted field and an
// explicit null.
type optionalString struct {
	Set   bool
	Value *string
}

func (o *optionalString) UnmarshalJSON(raw []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		o.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}
func (o optionalString) serviceValue() service.OptionalString {
	return service.OptionalString{Set: o.Set, Value: o.Value}
}

// optionalTerms preserves the distinction between an omitted terms object and
// an explicit null, which resets the terms to the default set.
type optionalTerms struct {
	Set   bool
	Value *termsRequest
}

func (o *optionalTerms) UnmarshalJSON(raw []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		o.Value = nil
		return nil
	}
	var value termsRequest
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

// optionalSimDetails preserves the distinction between an omitted SIM detail
// block and an explicit null.
type optionalSimDetails struct {
	Set   bool
	Value *simDetailsPatchRequest
}

func (o *optionalSimDetails) UnmarshalJSON(raw []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		o.Value = nil
		return nil
	}
	var value simDetailsPatchRequest
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
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
	return map[string]any{"id": identifier(v.ID), "telegram_user_id": identifier(v.TelegramUserID), "timezone": v.Timezone, "has_private_chat": v.PrivateChatID != nil, "session_ttl_seconds": v.SessionTTLSeconds, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
}
func termsView(v service.PeriodTerms) map[string]any {
	return map[string]any{"mode": v.Mode, "interval_count": v.IntervalCount, "interval_unit": v.IntervalUnit, "amount": v.Amount, "currency": v.Currency, "revision": v.Revision}
}
func periodView(v service.Period) map[string]any {
	return map[string]any{"id": identifier(v.ID), "service_id": identifier(v.ServiceID), "name": v.Name, "starts_at": nullableTime(v.StartsAt), "expires_at": timestamp(v.ExpiresAt), "date_precision": v.DatePrecision, "local_expires_date": v.LocalExpiresDate, "timezone": v.Timezone, "status": v.Status, "cycle": v.Cycle, "terms": termsView(v.Terms), "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
}
func ruleView(v service.Rule) map[string]any {
	return map[string]any{"id": identifier(v.ID), "period_id": identifier(v.PeriodID), "days_before": v.DaysBefore, "reminder_time": v.ReminderTime, "enabled": v.Enabled, "revision": v.Revision, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
}
func renewalView(v service.Renewal) map[string]any {
	return map[string]any{"id": identifier(v.ID), "period_id": identifier(v.PeriodID), "previous_cycle": v.PreviousCycle, "previous_expires_at": timestamp(v.PreviousExpiresAt), "new_expires_at": timestamp(v.NewExpiresAt), "previous_date_precision": v.PreviousDatePrecision, "new_date_precision": v.NewDatePrecision, "previous_local_expires_date": v.PreviousLocalExpiresDate, "new_local_expires_date": v.NewLocalExpiresDate, "timezone": v.Timezone, "renewed_at": timestamp(v.RenewedAt), "amount": v.Amount, "currency": v.Currency, "notes": v.Notes, "created_at": timestamp(v.CreatedAt)}
}
func jobView(v service.Job) map[string]any {
	return map[string]any{"id": identifier(v.ID), "period_id": identifier(v.PeriodID), "reminder_rule_id": identifier(v.ReminderRuleID), "cycle": v.Cycle, "rule_revision": v.RuleRevision, "scheduled_at": timestamp(v.ScheduledAt), "expires_at_snapshot": timestamp(v.ExpiresAtSnapshot), "status": v.Status, "attempts": v.Attempts, "next_attempt_at": nullableTime(v.NextAttemptAt), "sent_at": nullableTime(v.SentAt), "telegram_message_id": nullableID(v.TelegramMessageID), "last_error_code": v.LastErrorCode, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
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

type periodCreateRequest struct {
	Name          string        `json:"name"`
	StartsAt      *string       `json:"starts_at"`
	DatePrecision string        `json:"date_precision"`
	ExpiresAt     string        `json:"expires_at"`
	Terms         *termsRequest `json:"terms"`
}
type periodPatchRequest struct {
	ExpectedCycle         int64          `json:"expected_cycle"`
	Name                  *string        `json:"name"`
	StartsAt              optionalString `json:"starts_at"`
	DatePrecision         *string        `json:"date_precision"`
	ExpiresAt             *string        `json:"expires_at"`
	ExpectedTermsRevision *int64         `json:"expected_terms_revision"`
	Terms                 optionalTerms  `json:"terms"`
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

// Nested objects enforce their own schema because optional.UnmarshalJSON uses
// json.Unmarshal, which does not inherit the outer decoder's strictness.
func strictNested(raw []byte, target any, required []string, nullable ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("expected object")
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("missing %s", key)
		}
	}
	for key, value := range fields {
		if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		allowed := false
		for _, name := range nullable {
			if name == key {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("null %s", key)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

type termsRequest struct {
	Mode          string  `json:"mode"`
	IntervalCount *int64  `json:"interval_count"`
	IntervalUnit  *string `json:"interval_unit"`
	Amount        *int64  `json:"amount"`
	Currency      *string `json:"currency"`
}

func (t *termsRequest) UnmarshalJSON(raw []byte) error {
	type plain termsRequest
	return strictNested(raw, (*plain)(t), []string{"mode", "interval_count", "interval_unit", "amount", "currency"}, "interval_count", "interval_unit", "amount", "currency")
}
func (t termsRequest) serviceValue() service.TermsInput {
	return service.TermsInput{Mode: t.Mode, IntervalCount: t.IntervalCount, IntervalUnit: t.IntervalUnit, Amount: t.Amount, Currency: t.Currency}
}
func (t *termsRequest) servicePointer() *service.TermsInput {
	if t == nil {
		return nil
	}
	v := t.serviceValue()
	return &v
}

// termsPatch converts the request's tri-state terms field into the service's
// explicit set flag and payload.
func termsPatch(t optionalTerms) (bool, *service.TermsInput) {
	if !t.Set {
		return false, nil
	}
	return true, t.Value.servicePointer()
}

type simDetailsRequest struct {
	CardType        string  `json:"card_type"`
	PhoneNumber     *string `json:"phone_number"`
	CountryOrRegion *string `json:"country_or_region"`
	ICCID           *string `json:"iccid"`
}

func (s *simDetailsRequest) UnmarshalJSON(raw []byte) error {
	type plain simDetailsRequest
	return strictNested(raw, (*plain)(s), nil, "phone_number", "country_or_region", "iccid")
}
func (s *simDetailsRequest) serviceValue() *service.SIMDetails {
	if s == nil {
		return nil
	}
	return &service.SIMDetails{CardType: s.CardType, PhoneNumber: s.PhoneNumber, CountryOrRegion: s.CountryOrRegion, ICCID: s.ICCID}
}

type simDetailsPatchRequest struct {
	CardType        optionalString `json:"card_type"`
	PhoneNumber     optionalString `json:"phone_number"`
	CountryOrRegion optionalString `json:"country_or_region"`
	ICCID           optionalString `json:"iccid"`
}

func (s *simDetailsPatchRequest) UnmarshalJSON(raw []byte) error {
	type plain simDetailsPatchRequest
	return strictNested(raw, (*plain)(s), nil, "phone_number", "country_or_region", "iccid")
}

// simDetailsPatch converts the request's tri-state details field into the
// service's explicit set flag and payload.
func simDetailsPatch(s optionalSimDetails) (bool, *service.SIMDetailsPatch) {
	if !s.Set {
		return false, nil
	}
	if s.Value == nil {
		return true, nil
	}
	return true, &service.SIMDetailsPatch{CardType: s.Value.CardType.serviceValue(), PhoneNumber: s.Value.PhoneNumber.serviceValue(), CountryOrRegion: s.Value.CountryOrRegion.serviceValue(), ICCID: s.Value.ICCID.serviceValue()}
}

type serviceCreateRequest struct {
	Name          string             `json:"name"`
	CategoryID    string             `json:"category_id"`
	Provider      *string            `json:"provider"`
	Identifier    *string            `json:"identifier"`
	ManagementURL *string            `json:"management_url"`
	Notes         *string            `json:"notes"`
	SIMDetails    *simDetailsRequest `json:"sim_details"`
}
type servicePatchRequest struct {
	Name          *string            `json:"name"`
	CategoryID    *string            `json:"category_id"`
	Provider      optionalString     `json:"provider"`
	Identifier    optionalString     `json:"identifier"`
	ManagementURL optionalString     `json:"management_url"`
	Notes         optionalString     `json:"notes"`
	Status        *string            `json:"status"`
	SIMDetails    optionalSimDetails `json:"sim_details"`
}

func categoryView(v service.Category) map[string]any {
	return map[string]any{"id": identifier(v.ID), "user_id": nullableID(v.UserID), "code": v.Code, "name": v.Name, "kind": v.Kind, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt)}
}
func serviceView(v service.ServiceItem) map[string]any {
	var details any
	if v.SIMDetails != nil {
		s := v.SIMDetails
		details = map[string]any{"card_type": s.CardType, "phone_number": s.PhoneNumber, "country_or_region": s.CountryOrRegion, "iccid": s.ICCID}
	}
	return map[string]any{"id": identifier(v.ID), "user_id": identifier(v.UserID), "name": v.Name, "category_id": identifier(v.CategoryID), "provider": v.Provider, "identifier": v.Identifier, "management_url": v.ManagementURL, "notes": v.Notes, "status": v.Status, "created_at": timestamp(v.CreatedAt), "updated_at": timestamp(v.UpdatedAt), "sim_details": details}
}
func serviceOverviewView(v service.ServiceOverview) map[string]any {
	items := make([]map[string]any, 0, len(v.Items.Data))
	for _, item := range v.Items.Data {
		var period any
		if item.PrimaryPeriod != nil {
			period = periodView(*item.PrimaryPeriod)
		}
		periods := make([]map[string]any, 0, len(item.Periods))
		for _, value := range item.Periods {
			periods = append(periods, periodView(value))
		}
		items = append(items, map[string]any{"service": serviceView(item.Service), "primary_period": period, "periods": periods})
	}
	return map[string]any{"counts": map[string]int{"total": v.Counts.Total, "due_soon": v.Counts.DueSoon, "expired": v.Counts.Expired, "no_period": v.Counts.NoPeriod}, "items": items}
}
