package bot

import (
	"strings"
	"testing"
)

// The wizard step graph drives every flow. This test walks the documented
// scenarios end to end and asserts the stored result, not just the message.
func TestWizardFlowsStoreTheExpectedResult(t *testing.T) {
	t.Run("create a service through the wizard", func(t *testing.T) {
		f := newFixture(t)
		f.start(userA)
		vps := serviceCategoryID(t, f, "vps")
		f.send(userA, "/add")
		f.callback(userA, choiceCallback("category", id(vps)), nil)
		f.send(userA, "东京 VPS")
		f.send(userA, "云服务商")
		f.callback(userA, cb("dialog", "skip_rest"), nil)
		f.callback(userA, cb("dialog", "confirm"), nil)

		if got := f.count("SELECT count(*) FROM service_items"); got != 1 {
			t.Fatalf("service count=%d", got)
		}
		if got := serviceLookupString(t, f, "SELECT name FROM service_items WHERE id=1"); got != "东京 VPS" {
			t.Fatalf("name=%q", got)
		}
		if got := serviceLookupString(t, f, "SELECT provider FROM service_items WHERE id=1"); got != "云服务商" {
			t.Fatalf("provider=%q", got)
		}
	})

	t.Run("edit one service field per edit session", func(t *testing.T) {
		f := newFixture(t)
		f.start(userA)
		item := f.createService("Host", "vps")
		f.callback(userA, cb("service", "edit", id(item.ID)), nil)
		f.callback(userA, choiceCallback("field", "notes"), nil)
		f.send(userA, "第一版备注")
		f.callback(userA, cb("dialog", "confirm"), nil)

		if got := serviceLookupString(t, f, "SELECT notes FROM service_items WHERE id=1"); got != "第一版备注" {
			t.Fatalf("notes=%q", got)
		}
		// The edit session is closed, so the next edit starts from the stored
		// service and writes a single field again.
		f.callback(userA, cb("service", "edit", id(item.ID)), nil)
		f.callback(userA, choiceCallback("field", "name"), nil)
		f.send(userA, "改名后的主机")
		f.callback(userA, cb("dialog", "confirm"), nil)

		if got := serviceLookupString(t, f, "SELECT name FROM service_items WHERE id=1"); got != "改名后的主机" {
			t.Fatalf("name=%q", got)
		}
		if got := serviceLookupString(t, f, "SELECT notes FROM service_items WHERE id=1"); got != "第一版备注" {
			t.Fatalf("notes=%q", got)
		}
	})

	t.Run("edit period terms to recurring and advance the revision", func(t *testing.T) {
		f := newFixture(t)
		f.start(userA)
		item := f.createService("Host", "vps")
		p := f.createPeriod(item.ID, "月付", "date", "2027-03-31")
		f.callback(userA, cb("period", "edit", id(p.ID)), nil)
		f.callback(userA, choiceCallback("pfield", "terms"), nil)
		f.callback(userA, choiceCallback("mode", "recurring"), nil)
		f.send(userA, "2 month")
		f.callback(userA, cb("dialog", "skip"), nil)
		f.callback(userA, cb("dialog", "confirm"), nil)

		if got := serviceLookupString(t, f, "SELECT mode FROM period_terms WHERE period_id=1"); got != "recurring" {
			t.Fatalf("terms mode=%q", got)
		}
		if got := f.scalar("SELECT revision FROM period_terms WHERE period_id=1"); got != 2 {
			t.Fatalf("terms revision=%d", got)
		}
		if got := f.scalar("SELECT interval_count FROM period_terms WHERE period_id=1"); got != 2 {
			t.Fatalf("interval count=%d", got)
		}
		// A terms-only edit must not advance the period cycle.
		if got := f.scalar("SELECT cycle FROM service_periods WHERE id=1"); got != 1 {
			t.Fatalf("cycle=%d", got)
		}
	})

	t.Run("edit the period expiry and advance the cycle", func(t *testing.T) {
		f := newFixture(t)
		f.start(userA)
		item := f.createService("Host", "vps")
		p := f.createPeriod(item.ID, "月付", "date", "2027-03-31")
		f.callback(userA, cb("period", "edit", id(p.ID)), nil)
		f.callback(userA, choiceCallback("pfield", "expires"), nil)
		f.callback(userA, choiceCallback("precision", "date"), nil)
		f.send(userA, "2027-04-30")
		f.callback(userA, cb("dialog", "confirm"), nil)

		if got := f.scalar("SELECT cycle FROM service_periods WHERE id=1"); got != 2 {
			t.Fatalf("cycle=%d", got)
		}
		if got := serviceLookupString(t, f, "SELECT local_expires_date FROM service_periods WHERE id=1"); got != "2027-04-30" {
			t.Fatalf("expiry=%q", got)
		}
	})

	t.Run("the SIM and generic flows ask different questions", func(t *testing.T) {
		f := newFixture(t)
		f.start(userA)
		sim := serviceCategoryID(t, f, "sim")
		f.send(userA, "/add")
		f.callback(userA, choiceCallback("category", id(sim)), nil)
		f.send(userA, "旅行卡")
		f.callback(userA, choiceCallback("card_type", "esim"), nil)
		// The SIM flow asks for the card type and never for the public identifier.
		if text := f.stub.lastText(); !strings.Contains(text, "服务商") {
			t.Fatalf("SIM provider prompt: %q", text)
		}
		markup := markupOf(f)
		if strings.Contains(markup, choiceCallback("field", "identifier")) {
			t.Fatalf("SIM flow offered the public identifier: %q", markup)
		}
	})
}
