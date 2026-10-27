package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"telegram.bot.remind/internal/service"
)

func markupOf(f *fixture) string { return f.stub.lastMarkup() }

func TestServiceAddWizardGeneric(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	if text := f.send(userA, "/add"); !strings.Contains(text, "选择类别") {
		t.Fatalf("category prompt: %q", text)
	}
	vps := serviceCategoryID(t, f, "vps")
	f.callback(userA, choiceCallback("category", id(vps)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "输入服务名称") {
		t.Fatalf("name prompt: %q", text)
	}
	f.send(userA, "东京 VPS")
	if text := f.stub.lastText(); !strings.Contains(text, "服务商") {
		t.Fatalf("provider prompt: %q", text)
	}
	f.callback(userA, cb("dialog", "skip_rest"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请确认新建服务") {
		t.Fatalf("preview: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM service_items"); n != 1 {
		t.Fatalf("service count=%d", n)
	}
	if got := serviceLookupString(t, f, "SELECT name FROM service_items WHERE id=1"); got != "东京 VPS" {
		t.Fatalf("name=%q", got)
	}
	if got := f.count("SELECT count(*) FROM service_items WHERE category_id=?", vps); got != 1 {
		t.Fatalf("category not stored: %d", got)
	}
	text := f.stub.lastText()
	for _, forbidden := range []string{"版本", "周期", "internal", "period_type", "custom"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("detail leaked %q: %q", forbidden, text)
		}
	}
}

func TestServiceAddWizardSIMAndCustomCategory(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	sim := serviceCategoryID(t, f, "sim")
	f.send(userA, "/add")
	f.callback(userA, choiceCallback("category", id(sim)), nil)
	f.send(userA, "旅行卡")
	f.callback(userA, choiceCallback("card_type", "esim"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "服务商") {
		t.Fatalf("SIM provider prompt: %q", text)
	}
	f.callback(userA, cb("dialog", "skip_rest"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT card_type FROM sim_details WHERE service_id=1"); got != "esim" {
		t.Fatalf("SIM details=%q", got)
	}

	f.send(userA, "/add")
	f.callback(userA, cb("dialog", "new_category"), nil)
	f.send(userA, "影音订阅")
	if text := f.stub.lastText(); !strings.Contains(text, "选择类别") {
		t.Fatalf("return to categories: %q", text)
	}
	customID := serviceLookupInt(t, f, "SELECT id FROM service_categories WHERE user_id IS NOT NULL AND name='影音订阅'")
	f.callback(userA, choiceCallback("category", id(customID)), nil)
	f.send(userA, "视频会员")
	f.callback(userA, cb("dialog", "skip_rest"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := f.count("SELECT count(*) FROM service_items WHERE category_id=?", customID); got != 1 {
		t.Fatalf("custom category service=%d", got)
	}
}

func TestPeriodAddWizardAndNoImplicitRule(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	f.callback(userA, cb("period", "add", id(item.ID)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "有效期名称") {
		t.Fatalf("period name prompt: %q", text)
	}
	f.send(userA, "年付套餐")
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-03-31")
	if text := f.stub.lastText(); !strings.Contains(text, "开始时间") {
		t.Fatalf("starts prompt: %q", text)
	}
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, choiceCallback("mode", "recurring"), nil)
	f.send(userA, "1 month")
	f.callback(userA, cb("dialog", "skip"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请确认新建有效期") {
		t.Fatalf("preview: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM service_periods"); n != 1 {
		t.Fatalf("period count=%d", n)
	}
	if got := serviceLookupString(t, f, "SELECT mode FROM period_terms WHERE period_id=1"); got != "recurring" {
		t.Fatalf("terms mode=%q", got)
	}
	if n := f.count("SELECT count(*) FROM reminder_rules"); n != 0 {
		t.Fatalf("implicit rule created: %d", n)
	}
	markup := markupOf(f)
	if !strings.Contains(markup, "v3:rule:add:1") || !strings.Contains(markup, "v3:period:renew:1") {
		t.Fatalf("result actions: %q", markup)
	}
	if text := f.stub.lastText(); strings.Contains(text, "版本") || strings.Contains(text, "cycle") {
		t.Fatalf("period detail leaked internals: %q", text)
	}
}

func TestWizardAcceptsLocalWallClockTimes(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	f.callback(userA, cb("period", "add", id(item.ID)), nil)
	f.send(userA, "精确套餐")
	f.callback(userA, choiceCallback("precision", "datetime"), nil)
	f.send(userA, "2027-03-32 23:59")
	if text := f.stub.lastText(); !strings.Contains(text, "不存在") {
		t.Fatalf("invalid local time reply: %q", text)
	}
	f.send(userA, "2027-03-31 23:59")
	f.send(userA, "2027-03-01 00:00")
	if text := f.stub.lastText(); !strings.Contains(text, "续费约定") {
		t.Fatalf("starts accepted but flow stopped: %q", text)
	}
	f.callback(userA, choiceCallback("mode", "custom"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "2027-03-31 23:59") || strings.Contains(text, "+08:00") {
		t.Fatalf("preview did not render local time: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT date_precision||':'||expires_at FROM service_periods WHERE id=1"); got != "datetime:1806508740" {
		t.Fatalf("stored cutoff=%q", got)
	}
	if got := serviceLookupString(t, f, "SELECT CAST(starts_at AS TEXT) FROM service_periods WHERE id=1"); got != "1803830400" {
		t.Fatalf("stored start=%q", got)
	}

	// Renewal uses the same local-time entry and stores the absolute instant.
	f.callback(userA, cb("period", "renew", "1"), nil)
	f.callback(userA, choiceCallback("precision", "datetime"), nil)
	f.send(userA, "2027-05-01 12:30")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT CAST(new_expires_at AS TEXT) FROM renewals WHERE period_id=1"); got != "1809145800" {
		t.Fatalf("renewed cutoff=%q", got)
	}
}

func TestRulePreviewTogglesEnabledBeforeSaving(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-06-30")
	f.callback(userA, cb("rule", "add", id(p.ID)), nil)
	f.send(userA, "1")
	f.send(userA, "09:00")
	f.callback(userA, cb("dialog", "toggle_enabled"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "启用：否") {
		t.Fatalf("toggle did not disable: %q", text)
	}
	if !strings.Contains(markupOf(f), "改为启用") {
		t.Fatalf("toggle label did not flip: %q", markupOf(f))
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := f.scalar("SELECT enabled FROM reminder_rules WHERE id=1"); got != 0 {
		t.Fatalf("disabled rule enabled=%d", got)
	}
}

func TestRuleEditPreservesDisabledState(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-06-30")
	r := f.createRule(p.ID, 2, "08:00")
	f.exec(`UPDATE reminder_rules SET enabled=0 WHERE id=?`, r.ID)

	f.callback(userA, cb("rule", "edit", id(r.ID)), nil)
	f.send(userA, "3")
	f.send(userA, "08:30")
	if text := f.stub.lastText(); !strings.Contains(text, "启用：否") {
		t.Fatalf("edit preview did not preserve disabled state: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT days_before||':'||reminder_time||':'||enabled FROM reminder_rules WHERE id=1"); got != "3:08:30:0" {
		t.Fatalf("edited rule=%q", got)
	}
}

func TestPeriodEditWizardFieldsAndConflict(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "旧名字", "date", "2027-03-31")

	f.callback(userA, cb("period", "edit", id(p.ID)), nil)
	f.callback(userA, choiceCallback("pfield", "name"), nil)
	f.send(userA, "新名字")
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT name FROM service_periods WHERE id=1"); got != "新名字" {
		t.Fatalf("name=%q", got)
	}

	f.callback(userA, cb("period", "edit", id(p.ID)), nil)
	f.callback(userA, choiceCallback("pfield", "expires"), nil)
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-04-30")
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := f.scalar("SELECT cycle FROM service_periods WHERE id=1"); got != 2 {
		t.Fatalf("cycle=%d", got)
	}

	f.callback(userA, cb("period", "edit", id(p.ID)), nil)
	f.callback(userA, choiceCallback("pfield", "terms"), nil)
	f.callback(userA, choiceCallback("mode", "recurring"), nil)
	f.send(userA, "2 month")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT mode FROM period_terms WHERE period_id=1"); got != "recurring" {
		t.Fatalf("terms=%q", got)
	}
	if got := f.scalar("SELECT revision FROM period_terms WHERE period_id=1"); got != 2 {
		t.Fatalf("terms revision=%d", got)
	}

	// A stale cycle is rejected without writing and keeps the draft.
	f.callback(userA, cb("period", "edit", id(p.ID)), nil)
	f.callback(userA, choiceCallback("pfield", "name"), nil)
	f.send(userA, "冲突名字")
	u := f.serviceUser(userA)
	if _, err := f.svc.Renew(context.Background(), u.ID, p.ID, service.RenewInput{ExpectedCycle: 2, DatePrecision: "date", ExpiresAt: "2027-06-01"}); err != nil {
		t.Fatal(err)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "已被其他操作更新") {
		t.Fatalf("conflict reply: %q", text)
	}
	if got := serviceLookupString(t, f, "SELECT name FROM service_periods WHERE id=1"); got == "冲突名字" {
		t.Fatal("conflicting edit was written")
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("conflict cleared draft: %d", n)
	}
}

func TestRuleWizardLifecycle(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-06-30")

	f.callback(userA, cb("rule", "add", id(p.ID)), nil)
	f.send(userA, "3")
	f.send(userA, "09:00")
	if text := f.stub.lastText(); !strings.Contains(text, "请确认提醒规则") || !strings.Contains(text, "启用：是") {
		t.Fatalf("preview: %q", text)
	}
	if !strings.Contains(markupOf(f), "v3:dialog:toggle_enabled") {
		t.Fatalf("enabled toggle missing: %q", markupOf(f))
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM reminder_rules"); n != 1 {
		t.Fatalf("rule count=%d", n)
	}
	if got := f.scalar("SELECT enabled FROM reminder_rules WHERE id=1"); got != 1 {
		t.Fatalf("default enabled=%d", got)
	}
	f.callback(userA, cb("rule", "view", "1"), nil)
	text := f.stub.lastText()
	if !strings.Contains(text, "提前 3 天") || strings.Contains(text, "版本") {
		t.Fatalf("rule detail: %q", text)
	}
	f.callback(userA, cb("rule", "disable_ask", "1"), nil)
	f.callback(userA, cb("rule", "disable", "1"), nil)
	if got := f.scalar("SELECT enabled FROM reminder_rules WHERE id=1"); got != 0 {
		t.Fatalf("disabled=%d", got)
	}
	f.callback(userA, cb("rule", "enable_ask", "1"), nil)
	f.callback(userA, cb("rule", "enable", "1"), nil)
	if got := f.scalar("SELECT enabled FROM reminder_rules WHERE id=1"); got != 1 {
		t.Fatalf("enabled=%d", got)
	}

	f.callback(userA, cb("rule", "edit", "1"), nil)
	f.send(userA, "2")
	f.send(userA, "08:30")
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT days_before||':'||reminder_time FROM reminder_rules WHERE id=1"); got != "2:08:30" {
		t.Fatalf("edited rule=%q", got)
	}
	if got := f.scalar("SELECT revision FROM reminder_rules WHERE id=1"); got != 4 {
		t.Fatalf("revision=%d", got)
	}

	f.callback(userA, cb("rule", "delete_ask", "1"), nil)
	f.callback(userA, cb("rule", "view", "1"), nil)
	if n := f.count("SELECT count(*) FROM reminder_rules"); n != 1 {
		t.Fatalf("cancel deleted rule: %d", n)
	}
	f.callback(userA, cb("rule", "delete_ask", "1"), nil)
	f.callback(userA, cb("rule", "delete", "1"), nil)
	if n := f.count("SELECT count(*) FROM reminder_rules"); n != 0 {
		t.Fatalf("rule remains: %d", n)
	}
}

func TestRenewWizardConventionStaleAndExpiry(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	u := f.serviceUser(userA)
	amount, currency, count, unit := int64(1200), "TWD", int64(1), "month"
	p, err := f.svc.CreatePeriod(context.Background(), u.ID, item.ID, service.PeriodInput{
		Name: "月付", DatePrecision: "date", ExpiresAt: "2027-03-31",
		Terms: &service.TermsInput{Mode: "recurring", IntervalCount: &count, IntervalUnit: &unit, Amount: &amount, Currency: &currency},
	})
	if err != nil {
		t.Fatal(err)
	}

	f.callback(userA, cb("period", "renew", id(p.ID)), nil)
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-04-30")
	if text := f.stub.lastText(); !strings.Contains(text, "本次实际续期金额") || !strings.Contains(markupOf(f), "v3:dialog:use_convention") {
		t.Fatalf("amount step: %q / %q", text, markupOf(f))
	}
	f.callback(userA, cb("dialog", "use_convention"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请确认续期") || !strings.Contains(text, "12 TWD") {
		t.Fatalf("preview: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("renewals=%d", n)
	}
	if got := f.scalar("SELECT cycle FROM service_periods WHERE id=1"); got != 2 {
		t.Fatalf("cycle=%d", got)
	}

	// Stale cycle keeps the draft and writes nothing.
	f.callback(userA, cb("period", "renew", id(p.ID)), nil)
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-06-01")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	if _, err := f.svc.Renew(context.Background(), u.ID, p.ID, service.RenewInput{ExpectedCycle: 2, DatePrecision: "date", ExpiresAt: "2027-07-01"}); err != nil {
		t.Fatal(err)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "已被其他操作更新") {
		t.Fatalf("stale reply: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 2 {
		t.Fatalf("stale wrote renewal: %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("stale cleared draft: %d", n)
	}
	f.send(userA, "/cancel")

	f.callback(userA, cb("period", "renew", id(p.ID)), nil)
	f.clock.advance(16 * time.Minute)
	if text := f.send(userA, "2027-08-01"); !strings.Contains(text, "对话已过期") {
		t.Fatalf("expired dialog: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 2 {
		t.Fatalf("expired dialog wrote renewal: %d", n)
	}
}

func TestReadOnlyKeepsDraftAndReplacementIsExplicit(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-06-30")
	f.callback(userA, cb("period", "renew", id(p.ID)), nil)
	for _, command := range []string{"/services", "/reminders", "/settings", "/help"} {
		f.send(userA, command)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("read-only cleared draft: %d", n)
	}
	f.send(userA, "/add")
	if text := f.stub.lastText(); !strings.Contains(text, "继续当前") || !strings.Contains(text, "取消并开始") {
		t.Fatalf("replacement prompt: %q", text)
	}
	f.callback(userA, cb("dialog", "keep"), nil)
	if got := serviceLookupString(t, f, "SELECT action FROM bot_dialogs"); got != service.DialogRenew {
		t.Fatalf("keep lost flow: %q", got)
	}
	f.send(userA, "/add")
	f.callback(userA, cb("dialog", "replace"), nil)
	if got := serviceLookupString(t, f, "SELECT action FROM bot_dialogs"); got != service.DialogServiceAdd {
		t.Fatalf("replace kept flow: %q", got)
	}
}

func TestServiceArchiveRestoreDeleteConfirmations(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-06-30")

	f.callback(userA, cb("service", "archive_ask", id(item.ID)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "归档") {
		t.Fatalf("archive ask: %q", text)
	}
	f.callback(userA, cb("service", "archive", id(item.ID)), nil)
	if got := serviceLookupString(t, f, "SELECT status FROM service_items WHERE id=1"); got != "archived" {
		t.Fatalf("status=%q", got)
	}
	f.callback(userA, cb("period", "add", id(item.ID)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "已归档") {
		t.Fatalf("archived add: %q", text)
	}
	f.callback(userA, cb("service", "restore", id(item.ID)), nil)
	if got := serviceLookupString(t, f, "SELECT status FROM service_items WHERE id=1"); got != "active" {
		t.Fatalf("restore status=%q", got)
	}
	if got := f.scalar("SELECT cycle FROM service_periods WHERE id=?", p.ID); got != 2 {
		t.Fatalf("restore cycle=%d", got)
	}

	f.callback(userA, cb("service", "delete_ask", id(item.ID)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "永久删除") {
		t.Fatalf("delete ask: %q", text)
	}
	f.callback(userA, cb("service", "view", id(item.ID), "0"), nil)
	if n := f.count("SELECT count(*) FROM service_items"); n != 1 {
		t.Fatalf("cancel deleted service: %d", n)
	}
	f.callback(userA, cb("service", "delete_ask", id(item.ID)), nil)
	f.callback(userA, cb("service", "delete", id(item.ID)), nil)
	if n := f.count("SELECT count(*) FROM service_items"); n != 0 {
		t.Fatalf("service remains: %d", n)
	}
	if n := f.count("SELECT count(*) FROM service_periods"); n != 0 {
		t.Fatalf("period remains: %d", n)
	}
}

func TestServiceEditWizardAndClear(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	sim := f.createSIM("卡")
	f.callback(userA, cb("service", "edit", id(sim.ID)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请选择要修改的字段") {
		t.Fatalf("field prompt: %q", text)
	}
	// Category choices stay within the SIM kind.
	f.callback(userA, choiceCallback("field", "category_id"), nil)
	markup := markupOf(f)
	if strings.Contains(markup, "v3:dialog:set:category:"+id(serviceCategoryID(t, f, "domain"))) {
		t.Fatalf("cross-kind category offered: %q", markup)
	}
	f.callback(userA, cb("dialog", "back"), nil)
	f.callback(userA, choiceCallback("field", "phone"), nil)
	f.send(userA, "+886900000000")
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT phone_number FROM sim_details WHERE service_id=1"); got != "+886900000000" {
		t.Fatalf("phone=%q", got)
	}
	f.callback(userA, cb("service", "edit", id(sim.ID)), nil)
	f.callback(userA, choiceCallback("field", "phone"), nil)
	f.callback(userA, cb("dialog", "clear_value"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM sim_details WHERE service_id=1 AND phone_number IS NULL"); n != 1 {
		t.Fatal("clear did not persist")
	}
}

func TestOverviewFiltersAndCategories(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	vps := serviceCategoryID(t, f, "vps")
	due := f.createService("DueSvc", "vps")
	expired := f.createService("ExpiredSvc", "vps")
	f.createService("BareSvc", "vps")
	archived := f.createService("ArchivedSvc", "vps")
	f.createPeriod(due.ID, "到期", "date", "2027-01-03")
	f.createPeriod(expired.ID, "过期", "date", "2026-12-30")
	u := f.serviceUser(userA)
	status := "archived"
	if _, err := f.svc.UpdateService(context.Background(), u.ID, archived.ID, service.ServicePatch{Status: &status}); err != nil {
		t.Fatal(err)
	}

	f.callback(userA, cb("service", "list", "1", "0", "0"), nil)
	if got := serviceLastAnswer(t, f); got != "" {
		t.Fatalf("overview answered with a toast: %q", got)
	}
	text := f.stub.lastText()
	for _, want := range []string{"我的服务", "DueSvc", "ExpiredSvc", "BareSvc"} {
		if !strings.Contains(text, want) {
			t.Fatalf("overview missing %q: %q", want, text)
		}
	}
	f.callback(userA, cb("service", "list", "2", "0", "0"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "DueSvc") || strings.Contains(text, "BareSvc") || strings.Contains(text, "ExpiredSvc") {
		t.Fatalf("due filter: %q", text)
	}
	f.callback(userA, cb("service", "list", "5", "0", "0"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "ArchivedSvc") || strings.Contains(text, "DueSvc") {
		t.Fatalf("archived filter: %q", text)
	}
	f.callback(userA, cb("service", "cats", "1", "0"), nil)
	if got := serviceLastAnswer(t, f); got != "" {
		t.Fatalf("cats answered with a toast: %q", got)
	}
	f.callback(userA, cb("service", "list", "1", id(vps), "0"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "DueSvc") {
		t.Fatalf("category filter: %q", text)
	}
}

func TestTimezoneSettingsAndJobs(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-06-30")
	_ = f.createRule(p.ID, 1, "09:00")
	f.exec(`INSERT INTO reminder_jobs(period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,created_at,updated_at) VALUES(?,?,1,1,?,?,'pending',1,1,1)`, p.ID, 1, f.clock.t.Unix(), p.ExpiresAt)

	f.send(userA, "/settings")
	if text := f.stub.lastText(); !strings.Contains(text, "当前时区：Asia/Taipei") {
		t.Fatalf("settings: %q", text)
	}
	f.send(userA, "/reminders")
	if text := f.stub.lastText(); !strings.Contains(text, "提醒任务（待发送）") {
		t.Fatalf("jobs: %q", text)
	}
	f.callback(userA, cb("job", "cancel_ask", "1"), nil)
	f.callback(userA, cb("job", "cancel", "1"), nil)
	if got := serviceLookupString(t, f, "SELECT status FROM reminder_jobs WHERE id=1"); got != "cancelled" {
		t.Fatalf("job status=%q", got)
	}

	f.callback(userA, cb("settings", "timezone"), nil)
	f.callback(userA, choiceCallback("tz", "Asia/Tokyo"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT timezone FROM users WHERE telegram_user_id=?", userA); got != "Asia/Tokyo" {
		t.Fatalf("timezone=%q", got)
	}
	if got := f.scalar("SELECT cycle FROM service_periods WHERE id=?", p.ID); got != 2 {
		t.Fatalf("timezone cycle=%d", got)
	}
}

func TestDialogRejectsAnotherChat(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add")
	f.exec(`UPDATE bot_dialogs SET chat_id=20002`)
	f.send(userA, "名称")
	if text := f.stub.lastText(); !strings.Contains(text, "其他会话") {
		t.Fatalf("cross-chat input: %q", text)
	}
	if n := f.count("SELECT count(*) FROM service_items"); n != 0 {
		t.Fatalf("cross-chat wrote services: %d", n)
	}
	if got := serviceLookupString(t, f, "SELECT step FROM bot_dialogs"); got != "add_category" {
		t.Fatalf("cross-chat changed step: %q", got)
	}
}

func TestCallbacksNeverCarrySensitiveDraftValues(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createSIM("卡")
	f.callback(userA, cb("service", "edit", "1"), nil)
	f.callback(userA, choiceCallback("field", "phone"), nil)
	f.send(userA, "+886900011122")
	f.callback(userA, cb("dialog", "change_field"), nil)
	f.callback(userA, choiceCallback("field", "iccid"), nil)
	f.send(userA, "89860099998888777766")
	f.callback(userA, cb("dialog", "change_field"), nil)
	f.callback(userA, choiceCallback("field", "notes"), nil)
	f.send(userA, "secret note text")
	f.callback(userA, cb("dialog", "cancel"), nil)
	for _, method := range []string{"sendMessage", "editMessageText"} {
		for _, call := range f.stub.callsOf(method) {
			markup := call.form.Get("reply_markup")
			for _, secret := range []string{"+886900011122", "89860099998888777766", "secret note text"} {
				if strings.Contains(markup, secret) {
					t.Fatalf("%s leaked %q: %q", method, secret, markup)
				}
			}
		}
	}
	for _, call := range f.stub.callsOf("answerCallbackQuery") {
		text := call.form.Get("text")
		for _, secret := range []string{"+886900011122", "89860099998888777766", "secret note text"} {
			if strings.Contains(text, secret) {
				t.Fatalf("answer leaked %q: %q", secret, text)
			}
		}
	}
}
