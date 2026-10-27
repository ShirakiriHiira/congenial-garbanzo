package bot

import (
	"context"
	"strings"
	"testing"

	"telegram.bot.remind/internal/service"
)

// A decimal convention fee must read back as the same decimal after a renewal;
// the stored value is minor units, so a raw integer would show 10.3 as 1030.
func TestRenewalShowsDecimalAmount(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	u := f.serviceUser(userA)
	amount, currency, count, unit := int64(1030), "CNY", int64(1), "month"
	p, err := f.svc.CreatePeriod(context.Background(), u.ID, item.ID, service.PeriodInput{
		Name: "月付", DatePrecision: "date", ExpiresAt: "2027-03-31",
		Terms: &service.TermsInput{Mode: "recurring", IntervalCount: &count, IntervalUnit: &unit, Amount: &amount, Currency: &currency},
	})
	if err != nil {
		t.Fatal(err)
	}

	f.callback(userA, cb("period", "view", id(p.ID)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "约定费用 10.3 CNY") {
		t.Fatalf("period detail: %q", text)
	}

	f.callback(userA, cb("period", "renew", id(p.ID)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "约定费用 10.3 CNY") {
		t.Fatalf("renew head: %q", text)
	}
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-04-30")
	f.callback(userA, cb("dialog", "use_convention"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "实际续期金额：10.3 CNY") {
		t.Fatalf("preview: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "金额：10.3 CNY") {
		t.Fatalf("renewal result: %q", text)
	}
	for _, call := range f.stub.callsOf("sendMessage") {
		if strings.Contains(call.form.Get("text"), "1030") {
			t.Fatalf("minor units leaked: %q", call.form.Get("text"))
		}
	}
	f.callback(userA, cb("period", "renewals", id(p.ID), "0"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "金额 10.3 CNY") {
		t.Fatalf("renewal history: %q", text)
	}
	if got := f.scalar("SELECT amount FROM renewals WHERE period_id=1"); got != 1030 {
		t.Fatalf("stored minor units=%d", got)
	}
}

// The bot accepts the same decimal input as the Mini App and never rounds.
func TestRenewalAcceptsDecimalAmount(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "月付", "date", "2027-03-31")
	f.callback(userA, cb("period", "renew", id(p.ID)), nil)
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-04-30")
	f.send(userA, "10.3 CNY")
	f.callback(userA, cb("dialog", "skip"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "实际续期金额：10.3 CNY") {
		t.Fatalf("preview: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := f.scalar("SELECT amount FROM renewals WHERE period_id=1"); got != 1030 {
		t.Fatalf("stored minor units=%d", got)
	}

	// JPY has no minor units, so a fraction is rejected without writing.
	f.callback(userA, cb("period", "renew", id(p.ID)), nil)
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-05-31")
	if text := f.send(userA, "10.5 JPY"); !strings.Contains(text, "JPY 不支持小数") {
		t.Fatalf("fraction reply: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("invalid amount wrote renewal: %d", n)
	}
}
