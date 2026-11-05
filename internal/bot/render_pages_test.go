package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"telegram.bot.remind/internal/domain"
	"telegram.bot.remind/internal/service"

	"github.com/go-telegram/bot/models"
)

func TestExpiryDisplayDropsSecondsAndDuplicateZone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2027, 11, 17, 11, 51, 0, 0, time.UTC).Unix()
	p := service.Period{ExpiresAt: at, DatePrecision: domain.PrecisionDateTime, Timezone: "Asia/Taipei"}
	if got := expiryDate(p, loc); got != "2027-11-17 19:51" {
		t.Fatalf("datetime display %q", got)
	}
	if got := expiryDate(p, nil); got != "2027-11-17 19:51（Asia/Taipei）" {
		t.Fatalf("foreign zone display %q", got)
	}
	date := "2027-01-30"
	p = service.Period{ExpiresAt: at, DatePrecision: domain.PrecisionDate, LocalExpiresDate: &date, Timezone: "Asia/Taipei"}
	if got := expiryDate(p, loc); got != "2027-01-30" {
		t.Fatalf("date display %q", got)
	}
	if got := countdownLabel(0); got != "（今日到期）" {
		t.Fatalf("today countdown %q", got)
	}
	if got := countdownLabel(-3); got != "（已过期 3 天）" {
		t.Fatalf("expired countdown %q", got)
	}
}

func TestOverviewPageUsesHTMLFiveRowsAndPagination(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	for i := range 6 {
		item := f.createService(fmt.Sprintf("Service %d", i), "vps")
		f.createPeriod(item.ID, "", "date", fmt.Sprintf("2027-01-%02d", i+2))
	}

	f.callback(userA, cb("service", "list", "1", "0", "1"), nil)
	form := f.stub.lastMessageForm()
	if got := form.Get("parse_mode"); got != "HTML" {
		t.Fatalf("parse mode %q", got)
	}
	if !strings.Contains(form.Get("link_preview_options"), `"is_disabled":true`) {
		t.Fatalf("preview options %q", form.Get("link_preview_options"))
	}
	text := form.Get("text")
	for _, want := range []string{"<b>全部 6</b>", "<b>1. Service 0</b>", "<b>5. Service 4</b>", "<code>2027-01-02</code>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("page text missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "Service 5") {
		t.Fatalf("page one leaked the sixth row: %q", text)
	}
	if strings.Contains(text, "· 有效") {
		t.Fatalf("status repeated on the expiry line: %q", text)
	}

	var keyboard models.InlineKeyboardMarkup
	if err := json.Unmarshal([]byte(form.Get("reply_markup")), &keyboard); err != nil {
		t.Fatal(err)
	}
	if got := keyboard.InlineKeyboard[0][0].Text; got != "• 当前服务 •" {
		t.Fatalf("active filter %q", got)
	}
	if got := keyboard.InlineKeyboard[1][2].Text; got != "全部服务" {
		t.Fatalf("all filter %q", got)
	}
	if got := keyboard.InlineKeyboard[2][0].Text; got != "Service 0" {
		t.Fatalf("service button %q", got)
	}
	pager := keyboard.InlineKeyboard[7]
	if pager[0].Text != "-" || pager[1].Text != "1 / 2 页" || pager[2].Text != "下一页 >" {
		t.Fatalf("pager %+v", pager)
	}
	bottom := keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-1]
	if len(bottom) != 3 || bottom[0].Text != "添加服务" || bottom[1].Text != "按类别" || bottom[2].Text != "设置" {
		t.Fatalf("bottom row %+v", bottom)
	}

	f.callback(userA, cb("service", "list", "1", "0", "2"), nil)
	text = f.stub.lastText()
	if !strings.Contains(text, "<b>6. Service 5</b>") {
		t.Fatalf("second page %q", text)
	}
	if err := json.Unmarshal([]byte(f.stub.lastMarkup()), &keyboard); err != nil {
		t.Fatal(err)
	}
	pager = keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-2]
	if pager[0].Text != "< 上一页" || pager[1].Text != "2 / 2 页" || pager[2].Text != "-" {
		t.Fatalf("last pager %+v", pager)
	}

	// Opening a row on page two and returning must restore the same page.
	f.callback(userA, keyboard.InlineKeyboard[2][0].CallbackData, nil)
	if text := f.stub.lastText(); !strings.Contains(text, "<b>Service 5</b>") {
		t.Fatalf("detail %q", text)
	}
	if err := json.Unmarshal([]byte(f.stub.lastMarkup()), &keyboard); err != nil {
		t.Fatal(err)
	}
	back := keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-1][0]
	if back.Text != "返回服务列表" {
		t.Fatalf("back row %+v", back)
	}
	f.callback(userA, back.CallbackData, nil)
	if text := f.stub.lastText(); !strings.Contains(text, "<b>6. Service 5</b>") || !strings.Contains(text, "全部 6") {
		t.Fatalf("returned page %q", text)
	}
	if err := json.Unmarshal([]byte(f.stub.lastMarkup()), &keyboard); err != nil {
		t.Fatal(err)
	}
	pager = keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-2]
	if pager[1].Text != "2 / 2 页" {
		t.Fatalf("returned pager %+v", pager)
	}
}

func TestOverviewHidesDuplicateAndEmptyProviders(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	u := f.serviceUser(userA)
	category := serviceCategoryID(t, f, "vps")
	same, blank := "SOSIM", ""
	for _, in := range []service.ServiceInput{
		{Name: "SOSIM", CategoryID: category, Provider: &same},
		{Name: "Blank", CategoryID: category, Provider: &blank},
	} {
		if _, err := f.svc.CreateService(context.Background(), u.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	f.callback(userA, cb("service", "list", "1", "0", "1"), nil)
	text := f.stub.lastText()
	if strings.Contains(text, "服务商") || strings.Contains(text, "SOSIM · ") {
		t.Fatalf("duplicate provider rendered: %q", text)
	}
	if !strings.Contains(text, "<b>1. SOSIM</b>") || !strings.Contains(text, "<b>2. Blank</b>") {
		t.Fatalf("titles missing: %q", text)
	}
}

func TestServiceDetailCompactsFieldsAndKeepsListContext(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	u := f.serviceUser(userA)
	name, country := "SOSIM", "HK"
	item, err := f.svc.CreateService(context.Background(), u.ID, service.ServiceInput{
		Name: name, CategoryID: serviceCategoryID(t, f, "sim"), Provider: &name,
		SIMDetails: &service.SIMDetails{CardType: "esim", CountryOrRegion: &country},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.createPeriod(item.ID, "", "datetime", "2027-11-17T19:51:00+08:00")

	state := listState{code: "2", page: 1}
	f.callback(userA, cb("service", "view", id(item.ID), state.token()), nil)
	form := f.stub.lastMessageForm()
	if got := form.Get("parse_mode"); got != "HTML" {
		t.Fatalf("parse mode %q", got)
	}
	text := form.Get("text")
	for _, want := range []string{
		"<b>SOSIM</b> [",
		"类别：SIM 与 eSIM",
		"地区：HK | 卡类型：eSIM",
		"有效期：<code>2027-11-17 19:51</code>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("detail missing %q: %q", want, text)
		}
	}
	for _, unwanted := range []string{"服务商：", "号码", "ICCID", "管理：", "备注：", "有效期：\n"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("empty field %q rendered: %q", unwanted, text)
		}
	}
	if strings.Contains(text, "Asia/Taipei") {
		t.Fatalf("duplicate zone rendered: %q", text)
	}

	var keyboard models.InlineKeyboardMarkup
	if err := json.Unmarshal([]byte(form.Get("reply_markup")), &keyboard); err != nil {
		t.Fatal(err)
	}
	if got := keyboard.InlineKeyboard[0][0].Text; got != "修改有效期 · 2027-11-17 19:51" {
		t.Fatalf("period button %q", got)
	}
	last := keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-1][0]
	if last.Text != "返回服务列表" || last.CallbackData != cb("service", "list", "2", "0", "1") {
		t.Fatalf("back button %+v", last)
	}
}

func TestServiceDetailRendersLinkAndCopyableValues(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	u := f.serviceUser(userA)
	provider, phone, iccid, link := "Apple", "+886 900 000 000", "8986001234", "https://example.test/manage?a=1&b=2"
	item, err := f.svc.CreateService(context.Background(), u.ID, service.ServiceInput{
		Name: "iCloud Family", CategoryID: serviceCategoryID(t, f, "sim"), Provider: &provider,
		ManagementURL: &link,
		SIMDetails:    &service.SIMDetails{CardType: "physical_sim", PhoneNumber: &phone, ICCID: &iccid},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.callback(userA, cb("service", "view", id(item.ID)), nil)
	text := f.stub.lastText()
	for _, want := range []string{
		"服务商：Apple",
		"号码：<code>+886 900 000 000</code>",
		"ICCID：<code>8986001234</code>",
		`<a href="https://example.test/manage?a=1&amp;b=2">打开管理后台</a>`,
		"有效期：未设置",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("detail missing %q: %q", want, text)
		}
	}
}

func TestServiceDetailListsMultiplePeriods(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	item := f.createService("Multi", "vps")
	f.createPeriod(item.ID, "", "date", "2027-02-01")
	f.createPeriod(item.ID, "月付", "date", "2027-03-01")

	f.callback(userA, cb("service", "view", id(item.ID)), nil)
	text := f.stub.lastText()
	if !strings.Contains(text, "1. 有效期 · <code>2027-02-01</code>") || !strings.Contains(text, "2. 月付 · <code>2027-03-01</code>") {
		t.Fatalf("period list %q", text)
	}
	if strings.Contains(text, "有效期：\n") {
		t.Fatalf("nested period heading %q", text)
	}
	var keyboard models.InlineKeyboardMarkup
	if err := json.Unmarshal([]byte(f.stub.lastMarkup()), &keyboard); err != nil {
		t.Fatal(err)
	}
	if got := keyboard.InlineKeyboard[0][0].Text; got != "修改: 有效期 · 2027-02-01" {
		t.Fatalf("first period button %q", got)
	}
	if got := keyboard.InlineKeyboard[1][0].Text; got != "修改: 月付 · 2027-03-01" {
		t.Fatalf("second period button %q", got)
	}
}
