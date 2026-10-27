package bot

import (
	"strings"
	"testing"
)

func TestDialogSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-03-31")
	f.callback(userA, cb("period", "renew", id(p.ID)), nil)
	f.callback(userA, choiceCallback("precision", "date"), nil)

	f.restart()
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("dialog lost across restart: %d", n)
	}
	f.send(userA, "2027-04-30")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "续期完成") {
		t.Fatalf("post-restart renew: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("renewals after restart=%d", n)
	}
}

func TestWizardValidationKeepsDraft(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	f.callback(userA, cb("period", "add", id(item.ID)), nil)
	f.send(userA, "套餐")
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-02-30")
	if text := f.stub.lastText(); !strings.Contains(text, "日期必须是存在的 YYYY-MM-DD") {
		t.Fatalf("invalid date reply: %q", text)
	}
	if got := serviceLookupString(t, f, "SELECT step FROM bot_dialogs"); got != "period_expires" {
		t.Fatalf("invalid date advanced step: %q", got)
	}
	f.send(userA, "2027-03-31")
	if n := f.count("SELECT count(*) FROM service_periods"); n != 0 {
		t.Fatalf("wizard wrote before confirm: %d", n)
	}
	f.send(userA, "/cancel")
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("cancel left draft: %d", n)
	}

	f.send(userA, "/add")
	f.callback(userA, choiceCallback("category", id(serviceCategoryID(t, f, "vps"))), nil)
	f.send(userA, "Host")
	f.send(userA, "Cloud provider")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.send(userA, "ftp://example.com/manage")
	if text := f.stub.lastText(); !strings.Contains(text, "管理链接") {
		t.Fatalf("invalid URL reply: %q", text)
	}
	if n := f.count("SELECT count(*) FROM service_items"); n != 1 {
		t.Fatalf("invalid URL wrote service: %d", n)
	}
}

func TestButtonStartedFlowsHonorReplacementIntent(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-06-30")
	f.callback(userA, cb("period", "renew", id(p.ID)), nil)

	f.callback(userA, cb("settings", "timezone"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "继续不会丢失草稿") {
		t.Fatalf("timezone did not prompt: %q", text)
	}
	f.callback(userA, cb("dialog", "replace"), nil)
	if got := serviceLookupString(t, f, "SELECT action||':'||step FROM bot_dialogs"); got != "timezone:tz_value" {
		t.Fatalf("replacement landed on %q", got)
	}

	f.callback(userA, cb("dialog", "new_service"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "继续不会丢失草稿") {
		t.Fatalf("new service did not prompt: %q", text)
	}
	f.callback(userA, cb("dialog", "replace"), nil)
	if got := serviceLookupString(t, f, "SELECT action FROM bot_dialogs"); got != "service_add" {
		t.Fatalf("new service landed on %q", got)
	}
}

func TestPeriodEditStartsClear(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-06-30")
	f.exec(`UPDATE service_periods SET starts_at=100 WHERE id=?`, p.ID)
	f.callback(userA, cb("period", "edit", id(p.ID)), nil)
	f.callback(userA, choiceCallback("pfield", "starts"), nil)
	if !strings.Contains(markupOf(f), "v3:dialog:clear_value") {
		t.Fatalf("clear button missing: %q", markupOf(f))
	}
	f.callback(userA, cb("dialog", "clear_value"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM service_periods WHERE id=? AND starts_at IS NULL", p.ID); n != 1 {
		t.Fatal("start was not cleared")
	}
}

func TestRenewBackRetainsValuesAndCancelWritesNothing(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-06-30")
	f.callback(userA, cb("period", "renew", id(p.ID)), nil)
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-07-31")
	f.send(userA, "1000 TWD")
	if text := f.stub.lastText(); !strings.Contains(text, "续期备注") {
		t.Fatalf("notes step: %q", text)
	}
	f.callback(userA, cb("dialog", "back"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "本次实际续期金额") {
		t.Fatalf("back landed on: %q", text)
	}
	f.send(userA, "1000 TWD")
	f.callback(userA, cb("dialog", "skip"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请确认续期") || !strings.Contains(text, "1000 TWD") {
		t.Fatalf("preview: %q", text)
	}
	f.callback(userA, cb("dialog", "cancel"), nil)
	if n := f.count("SELECT count(*) FROM renewals"); n != 0 {
		t.Fatalf("cancel wrote renewals: %d", n)
	}
	if got := f.scalar("SELECT cycle FROM service_periods WHERE id=?", p.ID); got != 1 {
		t.Fatalf("cancel changed cycle: %d", got)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("cancel left draft: %d", n)
	}
}
