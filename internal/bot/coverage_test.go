package bot

import (
	"context"
	"strings"
	"testing"
)

func TestPeriodAndRuleCallbackSurfaces(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createService("Host", "vps")
	f.createPeriod(1, "custom", "月付", "date", "2027-06-30")
	f.createRule(1, 3, "09:00")

	f.callback(userA, cb("period", "view", "1"), nil)
	if got := serviceLastAnswer(t, f); got != "详情已发送" {
		t.Fatalf("period view answer %q", got)
	}
	text := f.stub.lastText()
	if !strings.Contains(text, "月付") || !strings.Contains(text, "1 条规则") {
		t.Fatalf("period detail %q", text)
	}

	f.callback(userA, cb("period", "rules", "1"), nil)
	if got := serviceLastAnswer(t, f); got != "规则已发送" {
		t.Fatalf("rules answer %q", got)
	}
	keyboard := f.stub.callsOf("sendMessage")
	if got := keyboard[len(keyboard)-1].form.Get("reply_markup"); !strings.Contains(got, "v2:rule:edit:1") || !strings.Contains(got, "v2:rule:add:1") {
		t.Fatalf("rules keyboard %q", got)
	}

	f.callback(userA, cb("period", "renewals", "1"), nil)
	if got := serviceLastAnswer(t, f); got != "续期历史已发送" {
		t.Fatalf("renewals answer %q", got)
	}
	if got := f.stub.lastText(); !strings.Contains(got, "续期历史") {
		t.Fatalf("renewals text %q", got)
	}

	f.callback(userA, cb("service", "periods", "1"), nil)
	if got := serviceLastAnswer(t, f); got != "有效期已发送" {
		t.Fatalf("periods answer %q", got)
	}

	f.callback(userA, cb("period", "terms", "1"), nil)
	if got := serviceLastAnswer(t, f); got != "请选择条款模式" {
		t.Fatalf("terms button answer %q", got)
	}
	f.callback(userA, cb("dialog", "cancel"), nil)
	if n := f.count("SELECT count(*) FROM period_terms WHERE validity_period_id=1"); n != 0 {
		t.Fatalf("canceled terms wrote %d rows", n)
	}

	// An archived service rejects new periods without writing.
	f.callback(userA, cb("service", "archive", "1"), nil)
	if got := serviceLookupString(t, f, "SELECT status FROM service_items WHERE id=1"); got != "archived" {
		t.Fatalf("archive status %q", got)
	}
	before := f.count("SELECT count(*) FROM validity_periods") + f.count("SELECT count(*) FROM bot_dialogs")
	f.callback(userA, cb("period", "add", "1"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "已归档") {
		t.Fatalf("archived add text %q", text)
	}
	if got := serviceLastAnswer(t, f); !strings.Contains(got, "有效期类型") {
		t.Fatalf("archived add answer %q", got)
	}
	if got := f.count("SELECT count(*) FROM validity_periods") + f.count("SELECT count(*) FROM bot_dialogs"); got != before {
		t.Fatalf("archived service gained state %d", got-before)
	}
}

func TestCancelledPeriodCallbacks(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createSIM("卡")
	f.createPeriod(1, "number_validity", "项目", "date", "2027-01-31")
	{
		u := f.serviceUser(userA)
		if err := f.svc.CancelPeriod(context.Background(), u.ID, 1, 1); err != nil {
			t.Fatal(err)
		}
	}

	f.callback(userA, cb("period", "renew", "1"), nil)
	if got := serviceLastAnswer(t, f); !strings.Contains(got, "已取消") {
		t.Fatalf("cancelled renew answer %q", got)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("cancelled period opened dialog %d", n)
	}
	f.callback(userA, cb("period", "terms", "1"), nil)
	if got := serviceLastAnswer(t, f); !strings.Contains(got, "已取消") {
		t.Fatalf("cancelled terms answer %q", got)
	}
	f.callback(userA, cb("rule", "add", "1"), nil)
	if got := serviceLastAnswer(t, f); !strings.Contains(got, "已取消") && !strings.Contains(got, "不存在") {
		t.Fatalf("cancelled rule add answer %q", got)
	}
	if n := f.count("SELECT count(*) FROM reminder_rules"); n != 0 {
		t.Fatalf("cancelled period gained rules %d", n)
	}
}

func TestServiceEditWizardUpdatesSIMField(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createSIM("卡")
	f.send(userA, "/service edit 1")
	if text := f.stub.lastText(); !strings.Contains(text, "请选择要修改的字段") {
		t.Fatalf("edit field prompt %q", text)
	}
	keyboard := f.stub.callsOf("sendMessage")
	if got := keyboard[len(keyboard)-1].form.Get("reply_markup"); !strings.Contains(got, "v2:dialog:set:field:8") {
		t.Fatalf("edit keyboard %q", got)
	}
	f.callback(userA, choiceCallback("field", "phone"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "当前值：-") {
		t.Fatalf("edit value prompt %q", text)
	}
	f.send(userA, "+886900000000")
	if text := f.stub.lastText(); !strings.Contains(text, "请确认修改") {
		t.Fatalf("edit preview %q", text)
	}
	f.callback(userA, cb("dialog", "change_field"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请选择要修改的字段") {
		t.Fatalf("change field %q", text)
	}
	f.callback(userA, choiceCallback("field", "phone"), nil)
	f.callback(userA, cb("dialog", "back"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请选择要修改的字段") {
		t.Fatalf("edit back %q", text)
	}
	f.callback(userA, choiceCallback("field", "phone"), nil)
	f.send(userA, "  ")
	if got := serviceLookupString(t, f, "SELECT coalesce(phone_number,'NULL') FROM sim_details WHERE service_id=1"); got != "NULL" {
		t.Fatalf("blank clear did not persist: %q", got)
	}
	f.callback(userA, cb("dialog", "cancel"), nil)
	if n := f.count("SELECT count(*) FROM sim_details WHERE service_id=1 AND phone_number IS NOT NULL"); n != 0 {
		t.Fatalf("cancel wrote a phone %d", n)
	}
}

func TestButtonStartedFlowsHonorReplacementIntent(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createSIM("卡")
	f.createPeriod(1, "number_validity", "项目", "date", "2027-01-31")
	f.createRule(1, 1, "09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(1,1,1,1,?,?,'sent',1,1,1)`, f.clock.t.Unix(), f.clock.t.Unix())
	_ = f.count("SELECT count(*) FROM reminder_jobs") // the job is only here to make the v1 button legal

	// A second period renew button must offer replacement, and clicking it
	// starts the requested flow rather than merely cancelling.
	f.callback(userA, cb("period", "renew", "1"), nil)
	f.send(userA, "/settings")
	f.callback(userA, cb("settings", "timezone"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "继续当前") {
		t.Fatalf("timezone button did not prompt: %q", text)
	}
	f.callback(userA, cb("dialog", "replace"), nil)
	if got := serviceLookupString(t, f, "SELECT action||':'||step FROM bot_dialogs"); got != "timezone:tz_value" {
		t.Fatalf("replacement intent landed on %q", got)
	}
	if text := f.stub.lastText(); !strings.Contains(text, "当前时区") {
		t.Fatalf("timezone wizard not rendered: %q", text)
	}

	// The menu's "add service" button carries the same start intent.
	f.callback(userA, cb("dialog", "new_service"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "继续当前") {
		t.Fatalf("new service button did not prompt: %q", text)
	}
	f.callback(userA, cb("dialog", "replace"), nil)
	if got := serviceLookupString(t, f, "SELECT action FROM bot_dialogs"); got != "service_add" {
		t.Fatalf("new service intent landed on %q", got)
	}
}

func TestRenewPreviewCancelWritesNothing(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createSIM("卡")
	f.createPeriod(1, "number_validity", "项目", "date", "2027-01-31")
	f.send(userA, "/renew 1")
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-03-01")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请确认续期") {
		t.Fatalf("preview %q", text)
	}
	f.callback(userA, cb("dialog", "cancel"), nil)
	if n := f.count("SELECT count(*) FROM renewals"); n != 0 {
		t.Fatalf("cancel wrote renewals %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("cancel left dialog %d", n)
	}
	if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=1"); n != 1 {
		t.Fatalf("cancel changed cycle %d", n)
	}
}

func TestDialogRejectsAnotherChat(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/service add")
	f.exec(`UPDATE bot_dialogs SET chat_id=20002`)
	f.send(userA, "名称")
	if text := f.stub.lastText(); !strings.Contains(text, "其他会话") {
		t.Fatalf("cross-chat input: %q", text)
	}
	if n := f.count("SELECT count(*) FROM service_items"); n != 0 {
		t.Fatalf("cross-chat wrote services %d", n)
	}
	if got := serviceLookupString(t, f, "SELECT step FROM bot_dialogs"); got != "add_category" {
		t.Fatalf("cross-chat changed step %q", got)
	}
}

func TestServiceEditCategoryStaysWithinKind(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createService("Host", "vps")
	domainCategory := serviceCategoryID(t, f, "domain")
	f.send(userA, "/service edit 1")
	f.callback(userA, choiceCallback("field", "category_id"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "当前类别") || !strings.Contains(text, "只能在同类") {
		t.Fatalf("category picker %q", text)
	}
	keyboard := f.stub.callsOf("sendMessage")
	if got := keyboard[len(keyboard)-1].form.Get("reply_markup"); !strings.Contains(got, "v2:dialog:set:category:"+id(domainCategory)) {
		t.Fatalf("category keyboard %q", got)
	}
	f.callback(userA, choiceCallback("category", id(domainCategory)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请确认修改") {
		t.Fatalf("category preview %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT c.code FROM service_items s JOIN service_categories c ON c.id=s.category_id WHERE s.id=1"); got != "domain" {
		t.Fatalf("category %q", got)
	}
}

func TestServiceDetailShowsPeriodActions(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createService("Host", "vps")
	f.createPeriod(1, "custom", "月付", "date", "2027-06-30")
	f.send(userA, "/service view 1")
	sends := f.stub.callsOf("sendMessage")
	keyboard := sends[len(sends)-1].form.Get("reply_markup")
	for _, want := range []string{"v2:period:view:1", "v2:period:renew:1", "v2:period:rules:1", "v2:period:renewals:1", "v2:period:terms:1"} {
		if !strings.Contains(keyboard, want) {
			t.Fatalf("service detail keyboard missing %q: %q", want, keyboard)
		}
	}
}

func TestCallbacksNeverCarrySensitiveDraftValues(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createSIM("卡")
	f.createPeriod(1, "number_validity", "私密项目", "date", "2027-01-31")
	f.send(userA, "/service edit 1")
	f.callback(userA, cb("dialog", "set", "field", "phone"), nil)
	f.send(userA, "+886900011122")
	f.callback(userA, cb("dialog", "change_field"), nil)
	f.callback(userA, cb("dialog", "set", "field", "iccid"), nil)
	f.send(userA, "89860099998888777766")
	f.callback(userA, cb("dialog", "change_field"), nil)
	f.callback(userA, cb("dialog", "set", "field", "notes"), nil)
	f.send(userA, "secret note text")
	f.send(userA, "/service view 1")
	f.send(userA, "/service period add 1")
	for _, call := range f.stub.callsOf("sendMessage") {
		markup := call.form.Get("reply_markup")
		for _, secret := range []string{"+886900011122", "89860099998888777766", "secret note text"} {
			if strings.Contains(markup, secret) {
				t.Fatalf("callback leaked %q: %q", secret, markup)
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
