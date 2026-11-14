package bot

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"telegram.bot.remind/internal/service"

	"github.com/go-telegram/bot/models"
)

func keyboardOf(t *testing.T, f *fixture) models.InlineKeyboardMarkup {
	t.Helper()
	var keyboard models.InlineKeyboardMarkup
	if err := json.Unmarshal([]byte(f.stub.lastMarkup()), &keyboard); err != nil {
		t.Fatal(err)
	}
	return keyboard
}

func buttonByText(t *testing.T, keyboard models.InlineKeyboardMarkup, label string) models.InlineKeyboardButton {
	t.Helper()
	for _, row := range keyboard.InlineKeyboard {
		for _, b := range row {
			if b.Text == label {
				return b
			}
		}
	}
	t.Fatalf("button %q missing", label)
	return models.InlineKeyboardButton{}
}

func hasButton(keyboard models.InlineKeyboardMarkup, label string) bool {
	for _, row := range keyboard.InlineKeyboard {
		for _, b := range row {
			if b.Text == label {
				return true
			}
		}
	}
	return false
}

// TestBackKeepsListPageThroughPeriodPage is the regression for a detail page
// that lost the list it came from as soon as the user opened a period.
func TestBackKeepsListPageThroughPeriodPage(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	var last service.ServiceItem
	for i := range 6 {
		item := f.createService(fmt.Sprintf("Service %d", i), "vps")
		f.createPeriod(item.ID, "", "date", fmt.Sprintf("2027-02-%02d", i+2))
		last = item
	}

	state := listState{code: "6", page: 2}
	f.callback(userA, cb("service", "view", id(last.ID), state.token()), nil)
	period := buttonByText(t, keyboardOf(t, f), "修改有效期 · 2027-02-07")
	f.callback(userA, period.CallbackData, nil)

	back := buttonByText(t, keyboardOf(t, f), "← 返回")
	if want := cb("nav", "go", serviceRef(last.ID, state).token()); back.CallbackData != want {
		t.Fatalf("period back = %q, want %q", back.CallbackData, want)
	}
	f.callback(userA, back.CallbackData, nil)
	if text := f.stub.lastText(); !strings.Contains(text, "Service 5") {
		t.Fatalf("service page %q", text)
	}

	back = buttonByText(t, keyboardOf(t, f), "← 返回")
	if want := cb("nav", "go", serviceList(state).token()); back.CallbackData != want {
		t.Fatalf("service back = %q, want %q", back.CallbackData, want)
	}
	f.callback(userA, back.CallbackData, nil)
	keyboard := keyboardOf(t, f)
	if !hasButton(keyboard, "• 全部服务 •") {
		t.Fatalf("filter not restored: %+v", keyboard.InlineKeyboard[0])
	}
	for _, row := range keyboard.InlineKeyboard {
		if len(row) == 3 && strings.Contains(row[1].Text, "2 / 2 页") {
			return
		}
	}
	t.Fatalf("page not restored: %+v", keyboard.InlineKeyboard)
}

// TestMenuRendersTheSamePageAsStart keeps the menu a rendered page instead of a
// message that a single tap consumes.
func TestMenuRendersTheSamePageAsStart(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("VPS", "vps")
	startText, startMarkup := f.stub.lastText(), f.stub.lastMarkup()

	f.callback(userA, cb("service", "view", id(item.ID)), nil)
	menu := buttonByText(t, keyboardOf(t, f), "主菜单")
	if menu.CallbackData != cb("nav", "home") {
		t.Fatalf("menu button %q", menu.CallbackData)
	}
	f.callback(userA, menu.CallbackData, nil)
	if text := f.stub.lastText(); text != startText {
		t.Fatalf("menu text %q, want %q", text, startText)
	}
	if markup := f.stub.lastMarkup(); markup != startMarkup {
		t.Fatalf("menu keyboard %q, want %q", markup, startMarkup)
	}
}

// TestCancelTaskReturnsToItsFilteredList is the regression for a task card that
// always dropped back to the pending list.
func TestCancelTaskReturnsToItsFilteredList(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Host", "vps")
	p := f.createPeriod(item.ID, "", "date", "2027-06-30")
	_ = f.createRule(p.ID, 1, "09:00")
	f.exec(`INSERT INTO reminder_jobs(period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,created_at,updated_at) VALUES(?,?,1,1,?,?,'pending',1,1,1)`, p.ID, 1, f.clock.t.Unix(), p.ExpiresAt)

	f.callback(userA, cb("job", "list", "3", "0"), nil)
	cancel := buttonByText(t, keyboardOf(t, f), "取消 · Host / 有效期")
	if cancel.CallbackData != cb("job", "cancel_ask", "1", "3", "0") {
		t.Fatalf("cancel button %q", cancel.CallbackData)
	}
	f.callback(userA, cancel.CallbackData, nil)
	keyboard := keyboardOf(t, f)
	if !hasButton(keyboard, "确认取消") {
		t.Fatalf("confirm sheet missing: %+v", keyboard.InlineKeyboard)
	}
	f.callback(userA, buttonByText(t, keyboard, "确认取消").CallbackData, nil)
	if text := f.stub.lastText(); !strings.Contains(text, "任务已取消") {
		t.Fatalf("cancel result %q", text)
	}
	back := buttonByText(t, keyboardOf(t, f), "← 返回")
	if want := cb("nav", "go", jobsPage("3", 0).token()); back.CallbackData != want {
		t.Fatalf("cancel back = %q, want %q", back.CallbackData, want)
	}
	f.callback(userA, back.CallbackData, nil)
	if text := f.stub.lastText(); !strings.Contains(text, "提醒任务（全部）") {
		t.Fatalf("returned list %q", text)
	}
}
