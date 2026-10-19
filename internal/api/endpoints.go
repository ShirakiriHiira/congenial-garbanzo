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
func (h *Handler) patchMe(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in struct {
		Timezone          *string `json:"timezone"`
		SessionTTLSeconds *int64  `json:"session_ttl_seconds"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Timezone == nil && in.SessionTTLSeconds == nil {
		fail(w, 400, "invalid_input")
		return
	}
	user := currentUser(r)
	if in.Timezone != nil {
		zone, ok := require(w, in.Timezone)
		if !ok {
			return
		}
		v, err := h.service.SetTimezone(r.Context(), user.ID, zone)
		if err != nil {
			serviceError(w, err)
			return
		}
		user = v
	}
	if in.SessionTTLSeconds != nil {
		v, err := h.service.SetSessionTTL(r.Context(), user.ID, *in.SessionTTLSeconds)
		if err != nil {
			serviceError(w, err)
			return
		}
		user = v
	}
	object(w, 200, userView(user), nil)
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
	if !decodeJSON(w, r, &in, "starts_at", "terms") {
		return
	}
	if in.ExpectedCycle < 1 || (in.Terms.Set != (in.ExpectedTermsRevision != nil)) || (in.ExpectedTermsRevision != nil && *in.ExpectedTermsRevision < 1) {
		fail(w, 400, "invalid_input")
		return
	}
	v, err := h.service.UpdatePeriod(r.Context(), currentUser(r).ID, id, service.PeriodPatch{ExpectedCycle: in.ExpectedCycle, ExpectedTermsRevision: in.ExpectedTermsRevision, Terms: termsPatch(in.Terms), Name: in.Name, StartsAt: in.StartsAt.serviceValue(), DatePrecision: in.DatePrecision, ExpiresAt: in.ExpiresAt})
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
func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	version, ok := expectedVersion(w, r, "expected_revision")
	if !ok {
		return
	}
	noContent(w, h.service.DeleteRule(r.Context(), currentUser(r).ID, id, version))
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

func (h *Handler) listCategories(w http.ResponseWriter, r *http.Request) {
	limit, after, _, ok := pagination(w, r, false)
	if !ok {
		return
	}
	v, err := h.service.ListCategories(r.Context(), currentUser(r).ID, after, limit)
	page(w, v, err, categoryView)
}
func (h *Handler) createCategory(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	v, err := h.service.CreateCategory(r.Context(), currentUser(r).ID, in.Name)
	object(w, 201, categoryView(v), err)
}
func (h *Handler) patchCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	v, err := h.service.UpdateCategory(r.Context(), currentUser(r).ID, id, in.Name)
	object(w, 200, categoryView(v), err)
}
func (h *Handler) listServices(w http.ResponseWriter, r *http.Request) {
	limit, after, filter, ok := servicePagination(w, r)
	if !ok {
		return
	}
	v, err := h.service.ListServices(r.Context(), currentUser(r).ID, after, limit, filter)
	page(w, v, err, serviceView)
}
func (h *Handler) serviceOverview(w http.ResponseWriter, r *http.Request) {
	limit, after, filter, ok := servicePagination(w, r)
	if !ok {
		return
	}
	v, err := h.service.ServiceOverview(r.Context(), currentUser(r).ID, after, limit, filter)
	if err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": serviceOverviewView(v), "next_cursor": cursor(v.Items.NextCursor)})
}
func (h *Handler) createService(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in serviceCreateRequest
	if !decodeJSON(w, r, &in, "provider", "identifier", "management_url", "notes", "sim_details") {
		return
	}
	category, ok := decimal(in.CategoryID, false)
	if !ok {
		fail(w, 400, "invalid_input")
		return
	}
	v, err := h.service.CreateService(r.Context(), currentUser(r).ID, service.ServiceInput{Name: in.Name, CategoryID: category, Provider: in.Provider, Identifier: in.Identifier, ManagementURL: in.ManagementURL, Notes: in.Notes, SIMDetails: in.SIMDetails.serviceValue()})
	object(w, 201, serviceView(v), err)
}
func (h *Handler) getService(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	v, err := h.service.GetService(r.Context(), currentUser(r).ID, id)
	object(w, 200, serviceView(v), err)
}
func (h *Handler) patchService(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in servicePatchRequest
	if !decodeJSON(w, r, &in, "provider", "identifier", "management_url", "notes", "sim_details") {
		return
	}
	var category *int64
	if in.CategoryID != nil {
		value, valid := decimal(*in.CategoryID, false)
		if !valid {
			fail(w, 400, "invalid_input")
			return
		}
		category = &value
	}
	v, err := h.service.UpdateService(r.Context(), currentUser(r).ID, id, service.ServicePatch{Name: in.Name, CategoryID: category, Provider: in.Provider.serviceValue(), Identifier: in.Identifier.serviceValue(), ManagementURL: in.ManagementURL.serviceValue(), Notes: in.Notes.serviceValue(), Status: in.Status, SIMDetails: simDetailsPatch(in.SIMDetails)})
	object(w, 200, serviceView(v), err)
}
func (h *Handler) deleteService(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	noContent(w, h.service.DeleteService(r.Context(), currentUser(r).ID, id))
}
func (h *Handler) listServicePeriods(w http.ResponseWriter, r *http.Request) {
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
func (h *Handler) createServicePeriod(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if _, ok := queryOnly(w, r); !ok {
		return
	}
	var in periodCreateRequest
	if !decodeJSON(w, r, &in, "starts_at", "terms") {
		return
	}
	v, err := h.service.CreatePeriod(r.Context(), currentUser(r).ID, id, service.PeriodInput{Name: in.Name, StartsAt: in.StartsAt, DatePrecision: in.DatePrecision, ExpiresAt: in.ExpiresAt, Terms: in.Terms.servicePointer()})
	object(w, 201, periodView(v), err)
}
