package service

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"telegram.bot.remind/internal/storage"
)

func stageTwoCategory(t *testing.T, st *storage.Store, code string) int64 {
	t.Helper()
	var id int64
	if err := st.DB.QueryRowContext(context.Background(), "SELECT id FROM service_categories WHERE code=?", code).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func stageTwoService(t *testing.T, s *Service, uid, category int64, name string) ServiceItem {
	t.Helper()
	v, err := s.CreateService(context.Background(), uid, ServiceInput{Name: name, CategoryID: category})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func stageTwoPeriod(t *testing.T, s *Service, uid, serviceID int64, date string, terms *TermsInput) Period {
	t.Helper()
	p, err := s.CreatePeriod(context.Background(), uid, serviceID, PeriodInput{DatePrecision: "date", ExpiresAt: date, Terms: terms})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGenericServicesMixedCategoriesAndFilters(t *testing.T) {
	s, st, a, b := setup(t)
	ctx := context.Background()
	sim, _, _ := fixture(t, s, a)
	vpsID := stageTwoCategory(t, st, "vps")
	subID := stageTwoCategory(t, st, "subscription")
	domainID := stageTwoCategory(t, st, "domain")
	custom, err := s.CreateCategory(ctx, a.ID, "  自定义服务  ")
	if err != nil || custom.Name != "自定义服务" || custom.UserID == nil || *custom.UserID != a.ID {
		t.Fatalf("category %+v %v", custom, err)
	}
	private, err := s.CreateCategory(ctx, b.ID, "其他用户")
	if err != nil {
		t.Fatal(err)
	}
	vps := stageTwoService(t, s, a.ID, vpsID, "云主机")
	sub := stageTwoService(t, s, a.ID, subID, "AI 订阅")
	dom := stageTwoService(t, s, a.ID, domainID, "域名")
	own := stageTwoService(t, s, a.ID, custom.ID, "自定义")
	stageTwoPeriod(t, s, a.ID, sub.ID, "2026-12-31", nil)
	stageTwoPeriod(t, s, a.ID, dom.ID, "2027-01-05", nil)
	stageTwoPeriod(t, s, a.ID, own.ID, "2027-03-01", nil)

	for _, tc := range []struct {
		status string
		want   []int64
	}{
		{"", []int64{sim.ID, vps.ID, sub.ID, dom.ID, own.ID}},
		{"active", []int64{sim.ID, vps.ID, sub.ID, dom.ID, own.ID}},
		{"due_soon", []int64{dom.ID}},
		{"expired", []int64{sub.ID}},
		{"no_period", []int64{vps.ID}},
	} {
		page, err := s.ServiceOverview(ctx, a.ID, 0, 100, ServiceFilter{Status: tc.status})
		var ids []int64
		for _, item := range page.Items.Data {
			ids = append(ids, item.Service.ID)
		}
		if err != nil || !reflect.DeepEqual(ids, tc.want) {
			t.Fatalf("status %q ids=%v want=%v err=%v", tc.status, ids, tc.want, err)
		}
	}
	combined, err := s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{CategoryID: &domainID, Status: "due_soon"})
	if err != nil || len(combined.Items.Data) != 1 || combined.Items.Data[0].Service.ID != dom.ID || combined.Items.Data[0].Service.SIMDetails != nil {
		t.Fatalf("combined %+v %v", combined, err)
	}
	if _, err = s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{CategoryID: &private.ID}); err != ErrNotFound {
		t.Fatalf("foreign category: %v", err)
	}
	missing := int64(999999)
	if _, err = s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{CategoryID: &missing}); err != ErrNotFound {
		t.Fatalf("missing category: %v", err)
	}
	if _, err = s.CreateService(ctx, a.ID, ServiceInput{Name: "invalid", CategoryID: private.ID}); err != ErrNotFound {
		t.Fatalf("foreign create category: %v", err)
	}
	if _, err = s.GetService(ctx, b.ID, vps.ID); err != ErrNotFound {
		t.Fatalf("foreign service: %v", err)
	}
	if _, err = s.UpdateService(ctx, a.ID, sim.ID, ServicePatch{CategoryID: &vpsID}); err != ErrConflict {
		t.Fatalf("cross-kind category: %v", err)
	}
	if _, err = s.UpdateCategory(ctx, a.ID, vpsID, "changed"); err != ErrConflict {
		t.Fatalf("builtin category rename: %v", err)
	}
	if _, err = s.CreateCategory(ctx, a.ID, custom.Name); err != ErrConflict {
		t.Fatalf("duplicate category: %v", err)
	}
	status := "archived"
	if _, err = s.UpdateService(ctx, a.ID, dom.ID, ServicePatch{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if page, err := s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{Status: "due_soon"}); err != nil || len(page.Items.Data) != 0 {
		t.Fatalf("archived due soon %+v %v", page, err)
	}
	archived, err := s.ServiceOverview(ctx, a.ID, 0, 1, ServiceFilter{Status: "archived"})
	if err != nil || archived.Counts != (OverviewCounts{Total: 1, DueSoon: 1}) || len(archived.Items.Data) != 1 || archived.Items.Data[0].Service.ID != dom.ID {
		t.Fatalf("archived matched set %+v %v", archived, err)
	}
	all, err := s.ServiceOverview(ctx, a.ID, 0, 100, ServiceFilter{Status: "all"})
	if err != nil || len(all.Items.Data) != 5 || all.Counts.Total != 5 {
		t.Fatalf("all %+v %v", all, err)
	}
	active := "active"
	if _, err = s.UpdateService(ctx, a.ID, dom.ID, ServicePatch{Status: &active}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{Status: "unknown"}); err != ErrInvalid {
		t.Fatalf("invalid status: %v", err)
	}
}

func TestServiceSearchEscapingAndFilterPagination(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	category := stageTwoCategory(t, st, "vps")
	for i := range 12 {
		stageTwoService(t, s, a.ID, category, fmt.Sprintf("filler %d", i))
	}
	provider, identifier := "cloud 100%", `host_a\b`
	needle, err := s.CreateService(ctx, a.ID, ServiceInput{Name: "needle", CategoryID: category, Provider: &provider, Identifier: &identifier})
	if err != nil {
		t.Fatal(err)
	}
	stageTwoService(t, s, a.ID, category, "cloud 100x hostXab")
	for _, query := range []string{"needle", "  needle  ", "100%", "_", `\`, `host_a\b`, "' OR 1=1 --"} {
		page, err := s.ServiceOverview(ctx, a.ID, 0, 1, ServiceFilter{Query: query, CategoryID: &category})
		want := 1
		if query == "' OR 1=1 --" {
			want = 0
		}
		if err != nil || len(page.Items.Data) != want || page.Items.NextCursor != nil || (want == 1 && page.Items.Data[0].Service.ID != needle.ID) {
			t.Fatalf("query %q page=%+v err=%v", query, page, err)
		}
	}
	first, err := s.ServiceOverview(ctx, a.ID, 0, 3, ServiceFilter{Query: "filler", CategoryID: &category})
	if err != nil || len(first.Items.Data) != 3 || first.Items.NextCursor == nil {
		t.Fatalf("first %+v %v", first, err)
	}
	second, err := s.ServiceOverview(ctx, a.ID, *first.Items.NextCursor, 3, ServiceFilter{Query: "filler", CategoryID: &category})
	if err != nil || len(second.Items.Data) != 3 || second.Items.Data[0].Service.ID <= *first.Items.NextCursor {
		t.Fatalf("second %+v %v", second, err)
	}
	for _, after := range []int64{0, *first.Items.NextCursor, needle.ID} {
		overview, err := s.ServiceOverview(ctx, a.ID, after, 1, ServiceFilter{Query: "filler", CategoryID: &category})
		if err != nil || overview.Counts != (OverviewCounts{Total: 12, NoPeriod: 12}) {
			t.Fatalf("full filtered counts after=%d %+v %v", after, overview, err)
		}
	}
	if _, err = s.ServiceOverview(ctx, a.ID, 0, 1, ServiceFilter{Query: strings.Repeat("界", 201)}); err != ErrInvalid {
		t.Fatalf("long query: %v", err)
	}
	if _, err = s.ServiceOverview(ctx, a.ID, -1, 10, ServiceFilter{}); err != ErrInvalid {
		t.Fatalf("invalid cursor: %v", err)
	}
}
func TestServiceOverviewPrimaryAndCalendarFiltering(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	category := stageTwoCategory(t, st, "domain")
	service := stageTwoService(t, s, a.ID, category, "primary selection")
	stageTwoPeriod(t, s, a.ID, service.ID, "2026-12-01", nil)
	future := stageTwoPeriod(t, s, a.ID, service.ID, "2027-03-01", nil)
	stageTwoPeriod(t, s, a.ID, service.ID, "2026-12-01", &TermsInput{Mode: "once"})
	expiredService := stageTwoService(t, s, a.ID, category, "expired selection")
	stageTwoPeriod(t, s, a.ID, expiredService.ID, "2026-10-01", nil)
	stageTwoPeriod(t, s, a.ID, expiredService.ID, "2026-12-01", nil)
	expiredTie := stageTwoPeriod(t, s, a.ID, expiredService.ID, "2026-12-01", nil)
	s.now = func() time.Time { return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) }
	overview, err := s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{CategoryID: &category, Status: "expired"})
	if err != nil || overview.Counts != (OverviewCounts{Total: 1, Expired: 1}) || len(overview.Items.Data) != 1 {
		t.Fatalf("overview %+v %v", overview, err)
	}
	p := overview.Items.Data[0].PrimaryPeriod
	if p == nil || p.ID != expiredTie.ID || p.ServiceID != expiredService.ID || p.Terms.Mode != "custom" || p.Status != "expired" {
		t.Fatalf("primary %+v", p)
	}
	activeOverview, err := s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{CategoryID: &category, Status: "active"})
	if err != nil || len(activeOverview.Items.Data) != 2 {
		t.Fatalf("active overview %+v %v", activeOverview, err)
	}
	found := false
	for _, item := range activeOverview.Items.Data {
		if item.Service.ID == service.ID && item.PrimaryPeriod != nil && item.PrimaryPeriod.ID == future.ID && item.PrimaryPeriod.Status == "active" {
			found = true
		}
	}
	if !found {
		t.Fatalf("earliest unexpired period was not primary: %+v", activeOverview)
	}
	if _, err = s.SetTimezone(ctx, a.ID, "America/New_York"); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2027, 3, 7, 17, 0, 0, 0, time.UTC) }
	due := stageTwoService(t, s, a.ID, category, "DST day seven")
	stageTwoPeriod(t, s, a.ID, due.ID, "2027-03-14", nil)
	notDue := stageTwoService(t, s, a.ID, category, "day eight")
	stageTwoPeriod(t, s, a.ID, notDue.ID, "2027-03-15", nil)
	page, err := s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{Status: "due_soon"})
	if err != nil || len(page.Items.Data) != 1 || page.Items.Data[0].Service.ID != due.ID {
		t.Fatalf("calendar filter %+v %v", page, err)
	}
}

func TestPeriodTermsVersioningAndJobPreservation(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	parent := stageTwoService(t, s, a.ID, stageTwoCategory(t, st, "vps"), "monthly")
	count, amount, currency, unit := int64(1), int64(0), "TWD", "month"
	terms := TermsInput{Mode: "recurring", IntervalCount: &count, IntervalUnit: &unit, Amount: &amount, Currency: &currency}
	p := stageTwoPeriod(t, s, a.ID, parent.ID, "2027-02-01", &terms)
	if p.Terms.Revision != 1 || p.Terms.Amount == nil || *p.Terms.Amount != 0 {
		t.Fatalf("create terms %+v", p)
	}
	var jobs []Job
	for i, state := range []string{"pending", "sending", "failed", "cancelled", "sent"} {
		rule, err := s.CreateRule(ctx, a.ID, p.ID, RuleInput{DaysBefore: i + 1, ReminderTime: "09:00"})
		if err != nil {
			t.Fatal(err)
		}
		res, err := st.DB.ExecContext(ctx, `INSERT INTO reminder_jobs(period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,locked_until,attempt_token,last_error_code,created_at,updated_at) VALUES(?,?,1,1,1,?,?,3,9,10,'fenced-token','permanent',1,1)`, p.ID, rule.ID, p.ExpiresAt, state)
		if err != nil {
			t.Fatal(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		job, err := s.GetJob(ctx, a.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, job)
	}
	amount = 999
	revision := int64(1)
	patch := PeriodPatch{ExpectedCycle: 1, ExpectedTermsRevision: &revision, Terms: Optional[TermsInput]{Set: true, Value: &terms}}
	got, err := s.UpdatePeriod(ctx, a.ID, p.ID, patch)
	if err != nil || got.Cycle != 1 || got.Terms.Revision != 2 || *got.Terms.Amount != amount {
		t.Fatalf("terms-only %+v %v", got, err)
	}
	for _, before := range jobs {
		after, err := s.GetJob(ctx, a.ID, before.ID)
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatalf("job changed before=%+v after=%+v err=%v", before, after, err)
		}
	}
	if _, err = s.UpdatePeriod(ctx, a.ID, p.ID, patch); err != ErrConflict {
		t.Fatalf("stale identical terms: %v", err)
	}
	revision = 2
	got, err = s.UpdatePeriod(ctx, a.ID, p.ID, patch)
	if err != nil || got.Terms.Revision != 2 {
		t.Fatalf("matching no-op %+v %v", got, err)
	}
	if _, err = s.UpdatePeriod(ctx, a.ID, p.ID, PeriodPatch{ExpectedCycle: 1, ExpectedTermsRevision: &revision}); err != ErrInvalid {
		t.Fatalf("revision without terms: %v", err)
	}
	if _, err = s.UpdatePeriod(ctx, a.ID, p.ID, PeriodPatch{ExpectedCycle: 1, Terms: Optional[TermsInput]{Set: true}}); err != ErrInvalid {
		t.Fatalf("terms without revision: %v", err)
	}
	got, err = s.UpdatePeriod(ctx, a.ID, p.ID, PeriodPatch{ExpectedCycle: 1, ExpectedTermsRevision: &revision, Terms: Optional[TermsInput]{Set: true}})
	if err != nil || got.Terms.Revision != 3 || !sameTerms(got.Terms, defaultTerms()) {
		t.Fatalf("reset %+v %v", got, err)
	}
	var rows, storedRevision int
	if err = st.DB.QueryRowContext(ctx, "SELECT count(*),revision FROM period_terms WHERE period_id=?", p.ID).Scan(&rows, &storedRevision); err != nil || rows != 1 || storedRevision != 3 {
		t.Fatalf("reset row count=%d revision=%d err=%v", rows, storedRevision, err)
	}
	plain := stageTwoPeriod(t, s, a.ID, parent.ID, "2027-02-01", nil)
	if plain.Terms.Revision != 1 || plain.Terms.Mode != "custom" {
		t.Fatalf("normalized default %+v", plain.Terms)
	}
	if err = st.DB.QueryRowContext(ctx, "SELECT count(*) FROM period_terms WHERE period_id=?", plain.ID).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("default row count=%d err=%v", rows, err)
	}
	one := int64(1)
	if _, err = s.UpdatePeriod(ctx, a.ID, plain.ID, PeriodPatch{ExpectedCycle: 1, ExpectedTermsRevision: &revision, Terms: Optional[TermsInput]{Set: true, Value: &TermsInput{Mode: "custom"}}}); err != ErrConflict {
		t.Fatalf("stale implicit default no-op: %v", err)
	}
	configured, err := s.UpdatePeriod(ctx, a.ID, plain.ID, PeriodPatch{ExpectedCycle: 1, ExpectedTermsRevision: &one, Terms: Optional[TermsInput]{Set: true, Value: &terms}})
	if err != nil || configured.Terms.Revision != 2 || configured.Cycle != 1 {
		t.Fatalf("first config %+v %v", configured, err)
	}
}

func TestPeriodTermsAtomicityValidationAndLifecycle(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	parent := stageTwoService(t, s, a.ID, stageTwoCategory(t, st, "subscription"), "terms")
	one, zero, negative := int64(1), int64(0), int64(-1)
	unit, invalidUnit, currency, lower := "month", "hour", "USD", "usd"
	for _, terms := range []TermsInput{
		{}, {Mode: "unknown"}, {Mode: "recurring"},
		{Mode: "recurring", IntervalCount: &zero, IntervalUnit: &unit},
		{Mode: "recurring", IntervalCount: &one, IntervalUnit: &invalidUnit},
		{Mode: "once", IntervalCount: &one}, {Mode: "custom", IntervalUnit: &unit},
		{Mode: "once", Amount: &one}, {Mode: "once", Currency: &currency},
		{Mode: "once", Amount: &negative, Currency: &currency},
		{Mode: "once", Amount: &one, Currency: &lower},
	} {
		_, err := s.CreatePeriod(ctx, a.ID, parent.ID, PeriodInput{DatePrecision: "date", ExpiresAt: "2027-02-01", Terms: &terms})
		if err != ErrInvalid {
			t.Fatalf("invalid terms %+v: %v", terms, err)
		}
	}
	maximum := int64(math.MaxInt64)
	terms := TermsInput{Mode: "once", Amount: &maximum, Currency: &currency}
	p := stageTwoPeriod(t, s, a.ID, parent.ID, "2027-02-01", &terms)
	rule, err := s.CreateRule(ctx, a.ID, p.ID, RuleInput{DaysBefore: 1, ReminderTime: "09:00"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB.ExecContext(ctx, `INSERT INTO reminder_jobs(period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,created_at,updated_at) VALUES(?,?,1,1,1,?,'pending',5,1,1)`, p.ID, rule.ID, p.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB.ExecContext(ctx, `CREATE TRIGGER reject_terms_insert BEFORE INSERT ON period_terms BEGIN SELECT RAISE(ABORT,'terms failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB.ExecContext(ctx, `CREATE TRIGGER reject_terms_update BEFORE UPDATE ON period_terms BEGIN SELECT RAISE(ABORT,'terms failure'); END`); err != nil {
		t.Fatal(err)
	}
	newDate, revision := "2027-03-01", int64(1)
	patch := PeriodPatch{ExpectedCycle: 1, ExpiresAt: &newDate, ExpectedTermsRevision: &revision, Terms: Optional[TermsInput]{Set: true, Value: &TermsInput{Mode: "custom"}}}
	if _, err = s.UpdatePeriod(ctx, a.ID, p.ID, patch); err != ErrUnavailable {
		t.Fatalf("terms write failure: %v", err)
	}
	current, err := s.GetPeriod(ctx, a.ID, p.ID)
	if err != nil || current.Cycle != 1 || current.ExpiresAt != p.ExpiresAt || !reflect.DeepEqual(current.Terms, p.Terms) {
		t.Fatalf("rollback period %+v %v", current, err)
	}
	var state string
	if err = st.DB.QueryRowContext(ctx, "SELECT status FROM reminder_jobs WHERE period_id=?", p.ID).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("rollback job %s %v", state, err)
	}
	if _, err = s.CreatePeriod(ctx, a.ID, parent.ID, PeriodInput{DatePrecision: "date", ExpiresAt: "2027-02-01", Terms: &terms}); err != ErrUnavailable {
		t.Fatalf("create atomicity failure: %v", err)
	}
	var count int
	if err = st.DB.QueryRowContext(ctx, "SELECT count(*) FROM service_periods WHERE service_id=?", parent.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("create rollback count=%d err=%v", count, err)
	}
	if _, err = st.DB.ExecContext(ctx, "DROP TRIGGER reject_terms_insert"); err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB.ExecContext(ctx, "DROP TRIGGER reject_terms_update"); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdatePeriod(ctx, a.ID, p.ID, patch)
	if err != nil || updated.Cycle != 2 || updated.Terms.Revision != 2 || updated.Terms.Mode != "custom" {
		t.Fatalf("combined update %+v %v", updated, err)
	}
	if err = st.DB.QueryRowContext(ctx, "SELECT status FROM reminder_jobs WHERE period_id=?", p.ID).Scan(&state); err != nil || state != "cancelled" {
		t.Fatalf("expiry job cancellation %s %v", state, err)
	}
	preserved := stageTwoPeriod(t, s, a.ID, parent.ID, "2027-02-01", &terms)
	if _, err = s.Renew(ctx, a.ID, preserved.ID, RenewInput{ExpectedCycle: 1, DatePrecision: "date", ExpiresAt: "2027-04-01"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetTimezone(ctx, a.ID, "UTC"); err != nil {
		t.Fatal(err)
	}
	current, err = s.GetPeriod(ctx, a.ID, preserved.ID)
	if err != nil || !reflect.DeepEqual(current.Terms, preserved.Terms) {
		t.Fatalf("renew/timezone terms %+v %v", current, err)
	}
	if err = s.DeletePeriod(ctx, a.ID, current.ID, current.Cycle); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetPeriod(ctx, a.ID, preserved.ID); err != ErrNotFound {
		t.Fatalf("deleted period remains: %v", err)
	}
}
