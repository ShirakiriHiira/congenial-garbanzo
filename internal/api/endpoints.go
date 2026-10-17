package api

import (
	"net/http"
	"telegram.bot.remind/internal/service"
)

func resourceID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	v, ok := pathID(r)
	if !ok {
		fail(w, 400, "invalid_input")
	}
	return v, ok
}
func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	object(w, 200, userView(currentUser(r)), nil)
}
func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	limit, after, _, ok := pagination(w, r, false)
	if !ok {
		return
	}
	v, err := h.service.Overview(r.Context(), currentUser(r).ID, after, limit)
	if err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": overviewView(v), "next_cursor": cursor(v.Items.NextCursor)})
}
func (h *Handler) patchMe(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in struct {
		Timezone *string `json:"timezone"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	zone, ok := require(w, in.Timezone)
	if !ok {
		return
	}
	v, err := h.service.SetTimezone(r.Context(), currentUser(r).ID, zone)
	object(w, 200, userView(v), err)
}
func (h *Handler) listSIMs(w http.ResponseWriter, r *http.Request) {
	limit, after, _, ok := pagination(w, r, false)
	if !ok {
		return
	}
	v, err := h.service.ListSIMs(r.Context(), currentUser(r).ID, after, limit)
	page(w, v, err, simView)
}
func (h *Handler) createSIM(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in simCreateRequest
	if !decodeJSON(w, r, &in, "operator", "phone_number", "country_or_region", "iccid", "notes") {
		return
	}
	v, err := h.service.CreateSIM(r.Context(), currentUser(r).ID, service.SIMInput{Name: in.Name, CardType: in.CardType, Operator: in.Operator, PhoneNumber: in.PhoneNumber, CountryOrRegion: in.CountryOrRegion, ICCID: in.ICCID, Notes: in.Notes})
	object(w, 201, simView(v), err)
}
func (h *Handler) getSIM(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	v, err := h.service.GetSIM(r.Context(), currentUser(r).ID, id)
	object(w, 200, simView(v), err)
}
func (h *Handler) patchSIM(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in simPatchRequest
	if !decodeJSON(w, r, &in, "operator", "phone_number", "country_or_region", "iccid", "notes") {
		return
	}
	v, err := h.service.UpdateSIM(r.Context(), currentUser(r).ID, id, service.SIMPatch{Name: in.Name, CardType: in.CardType, Operator: in.Operator.serviceValue(), PhoneNumber: in.PhoneNumber.serviceValue(), CountryOrRegion: in.CountryOrRegion.serviceValue(), ICCID: in.ICCID.serviceValue(), Notes: in.Notes.serviceValue(), Status: in.Status})
	object(w, 200, simView(v), err)
}
func (h *Handler) archiveSIM(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	noContent(w, h.service.ArchiveSIM(r.Context(), currentUser(r).ID, id))
}
func (h *Handler) deleteSIM(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	noContent(w, h.service.DeleteSIM(r.Context(), currentUser(r).ID, id))
}
func (h *Handler) listPeriods(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	limit, after, _, ok := pagination(w, r, false)
	if !ok {
		return
	}
	v, err := h.service.ListPeriods(r.Context(), currentUser(r).ID, id, after, limit)
	page(w, v, err, periodView)
}
func (h *Handler) createPeriod(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in periodCreateRequest
	if !decodeJSON(w, r, &in, "starts_at") {
		return
	}
	v, err := h.service.CreatePeriod(r.Context(), currentUser(r).ID, id, service.PeriodInput{PeriodType: in.PeriodType, Name: in.Name, StartsAt: in.StartsAt, DatePrecision: in.DatePrecision, ExpiresAt: in.ExpiresAt})
	object(w, 201, periodView(v), err)
}
func (h *Handler) getPeriod(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	v, err := h.service.GetPeriod(r.Context(), currentUser(r).ID, id)
	object(w, 200, periodView(v), err)
}
func (h *Handler) patchPeriod(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in periodPatchRequest
	if !decodeJSON(w, r, &in, "starts_at") {
		return
	}
	if in.ExpectedCycle < 1 {
		fail(w, 400, "invalid_input")
		return
	}
	v, err := h.service.UpdatePeriod(r.Context(), currentUser(r).ID, id, service.PeriodPatch{ExpectedCycle: in.ExpectedCycle, PeriodType: in.PeriodType, Name: in.Name, StartsAt: in.StartsAt.serviceValue(), DatePrecision: in.DatePrecision, ExpiresAt: in.ExpiresAt})
	object(w, 200, periodView(v), err)
}
func expectedVersion(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	values, ok := queryOnly(w, r, key)
	if !ok {
		return 0, false
	}
	version, ok := decimal(values.Get(key), false)
	if !ok {
		fail(w, 400, "invalid_input")
	}
	return version, ok
}
func (h *Handler) cancelPeriod(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	version, ok := expectedVersion(w, r, "expected_cycle")
	if !ok {
		return
	}
	noContent(w, h.service.CancelPeriod(r.Context(), currentUser(r).ID, id, version))
}
func (h *Handler) deletePeriod(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	version, ok := expectedVersion(w, r, "expected_cycle")
	if !ok {
		return
	}
	noContent(w, h.service.DeletePeriod(r.Context(), currentUser(r).ID, id, version))
}
func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	limit, after, _, ok := pagination(w, r, false)
	if !ok {
		return
	}
	v, err := h.service.ListRules(r.Context(), currentUser(r).ID, id, after, limit)
	page(w, v, err, ruleView)
}
func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in ruleCreateRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	days, ok := require(w, in.DaysBefore)
	if !ok {
		return
	}
	v, err := h.service.CreateRule(r.Context(), currentUser(r).ID, id, service.RuleInput{DaysBefore: days, ReminderTime: in.ReminderTime, Enabled: in.Enabled})
	object(w, 201, ruleView(v), err)
}
func (h *Handler) getRule(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	v, err := h.service.GetRule(r.Context(), currentUser(r).ID, id)
	object(w, 200, ruleView(v), err)
}
func (h *Handler) patchRule(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in rulePatchRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ExpectedRevision < 1 {
		fail(w, 400, "invalid_input")
		return
	}
	v, err := h.service.UpdateRule(r.Context(), currentUser(r).ID, id, service.RulePatch{ExpectedRevision: in.ExpectedRevision, DaysBefore: in.DaysBefore, ReminderTime: in.ReminderTime, Enabled: in.Enabled})
	object(w, 200, ruleView(v), err)
}
func (h *Handler) disableRule(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	version, ok := expectedVersion(w, r, "expected_revision")
	if !ok {
		return
	}
	noContent(w, h.service.DisableRule(r.Context(), currentUser(r).ID, id, version))
}
func (h *Handler) listRenewals(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	limit, after, _, ok := pagination(w, r, false)
	if !ok {
		return
	}
	v, err := h.service.ListRenewals(r.Context(), currentUser(r).ID, id, after, limit)
	page(w, v, err, renewalView)
}
func (h *Handler) createRenewal(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in renewalRequest
	if !decodeJSON(w, r, &in, "amount", "currency", "notes") {
		return
	}
	if in.ExpectedCycle < 1 {
		fail(w, 400, "invalid_input")
		return
	}
	v, err := h.service.Renew(r.Context(), currentUser(r).ID, id, service.RenewInput{ExpectedCycle: in.ExpectedCycle, DatePrecision: in.DatePrecision, ExpiresAt: in.ExpiresAt, Amount: in.Amount, Currency: in.Currency, Notes: in.Notes})
	status := 201
	if v.Replayed {
		status = 200
	}
	object(w, status, renewalView(v), err)
}
func (h *Handler) listReminders(w http.ResponseWriter, r *http.Request) {
	limit, after, status, ok := pagination(w, r, true)
	if !ok {
		return
	}
	v, err := h.service.ListJobs(r.Context(), currentUser(r).ID, status, after, limit)
	page(w, v, err, jobView)
}
func (h *Handler) getReminder(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	v, err := h.service.GetJob(r.Context(), currentUser(r).ID, id)
	object(w, 200, jobView(v), err)
}
func (h *Handler) cancelReminder(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	noContent(w, h.service.CancelJob(r.Context(), currentUser(r).ID, id))
}
