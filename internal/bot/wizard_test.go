package bot

import (
	"strings"
	"testing"
	"time"

	"telegram.bot.remind/internal/service"
)

// pickCategory selects a built-in category by its stable migration code.
func pickCategory(f *fixture, code string) string {
	return choiceCallback("category", id(serviceCategoryID(f.t, f, code)))
}

func TestServiceAddWizardGenericAndSIM(t *testing.T) {
	f := newFixture(t)
	f.start(userA)

	f.send(userA, "/service add")
	if text := f.stub.lastText(); !strings.Contains(text, "选择类别") {
		t.Fatalf("category step: %q", text)
	}
	sends := f.stub.callsOf("sendMessage")
	if got := sends[len(sends)-1].form.Get("reply_markup"); !strings.Contains(got, "v2:dialog:set:category:") {
		t.Fatalf("category keyboard %q", got)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs WHERE action='service_add'"); n != 1 {
		t.Fatalf("add dialog %d", n)
	}

	// Generic: no SIM questions are asked and no sim_details row is written.
	f.callback(userA, pickCategory(f, "vps"), nil)
	f.send(userA, "VPS Pro")
	f.callback(userA, cb("dialog", "skip"), nil) // provider
	f.send(userA, "vps.example.com")
	f.send(userA, "https://console.example.com")
	f.callback(userA, cb("dialog", "skip"), nil) // notes
	if text := f.stub.lastText(); !strings.Contains(text, "请确认新建服务") || strings.Contains(text, "卡类型") {
		t.Fatalf("generic preview: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM service_items WHERE name='VPS Pro'"); n != 1 {
		t.Fatalf("services %d", n)
	}
	if n := f.count("SELECT count(*) FROM sim_details"); n != 0 {
		t.Fatalf("generic service created sim details %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("dialog remained %d", n)
	}
	created := f.stub.lastText()
	if !strings.Contains(created, "VPS Pro") {
		t.Fatalf("created detail %q", created)
	}
	sends = f.stub.callsOf("sendMessage")
	keyboard := sends[len(sends)-1].form.Get("reply_markup")
	if !strings.Contains(keyboard, "v2:period:add:") || !strings.Contains(keyboard, "v2:service:view:") {
		t.Fatalf("result keyboard %q", keyboard)
	}

	// SIM: the wizard collects every SIM field and stores them all.
	f.send(userA, "/service add")
	f.callback(userA, pickCategory(f, "sim"), nil)
	f.send(userA, "eSIM 旅行")
	f.callback(userA, choiceCallback("card_type", "esim"), nil)
	f.send(userA, "KDDI")
	f.send(userA, "+886912345678")
	f.send(userA, "日本")
	f.send(userA, "89860012345678901234")
	f.send(userA, "keep private")
	if text := f.stub.lastText(); !strings.Contains(text, "卡类型：esim") || !strings.Contains(text, "ICCID：89860012345678901234") {
		t.Fatalf("sim preview: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM service_items WHERE name='eSIM 旅行'"); n != 1 {
		t.Fatalf("sim services %d", n)
	}
	simID := serviceLookupInt(t, f, "SELECT id FROM service_items WHERE name='eSIM 旅行'")
	if got := serviceLookupString(t, f, "SELECT card_type FROM sim_details WHERE service_id=?", simID); got != "esim" {
		t.Fatalf("card type %q", got)
	}
	if got := serviceLookupString(t, f, "SELECT phone_number FROM sim_details WHERE service_id=?", simID); got != "+886912345678" {
		t.Fatalf("phone %q", got)
	}
	if got := serviceLookupString(t, f, "SELECT iccid FROM sim_details WHERE service_id=?", simID); got != "89860012345678901234" {
		t.Fatalf("iccid %q", got)
	}
	if got := serviceLookupString(t, f, "SELECT country_or_region FROM sim_details WHERE service_id=?", simID); got != "日本" {
		t.Fatalf("region %q", got)
	}
}

func TestServiceAddWizardKeepsDraftOnInvalidInput(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/service add")
	f.callback(userA, pickCategory(f, "other"), nil)
	f.send(userA, "媒体订阅")
	f.callback(userA, cb("dialog", "skip"), nil) // provider
	f.callback(userA, cb("dialog", "skip"), nil) // identifier
	f.send(userA, "not a url")
	if text := f.stub.lastText(); !strings.Contains(text, "管理链接必须是无内嵌凭据") {
		t.Fatalf("invalid url accepted: %q", text)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("draft lost %d", n)
	}
	if n := f.count("SELECT count(*) FROM service_items"); n != 0 {
		t.Fatalf("invalid input created %d services", n)
	}
	f.send(userA, "https://example.com/portal")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM service_items WHERE name='媒体订阅'"); n != 1 {
		t.Fatalf("valid retry created %d", n)
	}
	if got := serviceLookupString(t, f, "SELECT management_url FROM service_items WHERE name='媒体订阅'"); got != "https://example.com/portal" {
		t.Fatalf("url %q", got)
	}
}

func TestPeriodAddWizardRespectsCategoryKind(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/service add name=Host&category_id="+id(serviceCategoryID(t, f, "vps")))
	f.send(userA, "/add name=卡&card_type=esim")

	genericID := serviceLookupInt(t, f, "SELECT id FROM service_items WHERE name='Host'")
	simID := serviceLookupInt(t, f, "SELECT id FROM service_items WHERE name='卡'")

	f.send(userA, "/service period add "+id(genericID))
	if text := f.stub.lastText(); !strings.Contains(text, "只能创建 custom") {
		t.Fatalf("generic period types: %q", text)
	}
	sends := f.stub.callsOf("sendMessage")
	keyboard := sends[len(sends)-1].form.Get("reply_markup")
	if strings.Contains(keyboard, "period_type:1") || strings.Contains(keyboard, "period_type:2") {
		t.Fatalf("generic keyboard offered SIM types %q", keyboard)
	}
	f.callback(userA, choiceCallback("period_type", "custom"), nil)
	f.callback(userA, cb("dialog", "skip"), nil) // name
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-06-30")
	f.callback(userA, cb("dialog", "skip"), nil) // starts_at
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM validity_periods WHERE service_id=? AND cycle=1 AND status='active'", genericID); n != 1 {
		t.Fatalf("generic period %d", n)
	}
	if n := f.count("SELECT count(*) FROM reminder_rules"); n != 0 {
		t.Fatalf("period auto-created rules %d", n)
	}
	result := f.stub.lastText()
	if !strings.Contains(result, "未自动创建提醒规则") {
		t.Fatalf("result text %q", result)
	}

	f.send(userA, "/service period add "+id(simID))
	sends = f.stub.callsOf("sendMessage")
	keyboard = sends[len(sends)-1].form.Get("reply_markup")
	for _, want := range []string{"v2:dialog:set:period_type:1", "v2:dialog:set:period_type:2", "v2:dialog:set:period_type:3", "v2:dialog:set:period_type:4"} {
		if !strings.Contains(keyboard, want) {
			t.Fatalf("SIM keyboard missing %q: %q", want, keyboard)
		}
	}
	f.callback(userA, choiceCallback("period_type", "plan_validity"), nil)
	f.send(userA, "套餐")
	f.callback(userA, choiceCallback("precision", "datetime"), nil)
	f.send(userA, "2027-07-01")
	if text := f.stub.lastText(); !strings.Contains(text, "带时区的 RFC3339") {
		t.Fatalf("date rejected for datetime: %q", text)
	}
	f.send(userA, "2027-07-01T12:00:00+08:00")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	var periodType, precision string
	if err := f.store.DB.QueryRow("SELECT period_type,date_precision FROM validity_periods WHERE service_id=? ORDER BY id DESC LIMIT 1", simID).Scan(&periodType, &precision); err != nil {
		t.Fatal(err)
	}
	if periodType != "plan_validity" || precision != "datetime" {
		t.Fatalf("period type=%q precision=%q", periodType, precision)
	}
}

func TestTermsWizardOnlyChangesTerms(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,attempt_token,created_at,updated_at) VALUES(1,1,1,1,?,?,'pending',2,?,'lease',1,1)`,
		f.clock.t.Add(24*time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix(), f.clock.t.Add(24*time.Hour).Unix())
	jobBefore := serviceLookupString(t, f, "SELECT status||':'||attempts||':'||attempt_token FROM reminder_jobs WHERE id=1")

	f.send(userA, "/service terms 1")
	if text := f.stub.lastText(); !strings.Contains(text, "请选择条款模式") {
		t.Fatalf("terms mode step: %q", text)
	}
	f.callback(userA, choiceCallback("mode", "recurring"), nil)
	f.send(userA, "1 month")
	f.send(userA, "2999 USD")
	if text := f.stub.lastText(); !strings.Contains(text, "请确认条款") || !strings.Contains(text, "每 1 month") {
		t.Fatalf("terms preview: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	applied := f.stub.lastText()
	if !strings.Contains(applied, "约定条款：recurring（版本 2）") {
		t.Fatalf("terms applied %q", applied)
	}
	if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=1"); n != 1 {
		t.Fatalf("terms changed cycle %d", n)
	}
	if got := serviceLookupString(t, f, "SELECT status||':'||attempts||':'||attempt_token FROM reminder_jobs WHERE id=1"); got != jobBefore {
		t.Fatalf("terms changed job %q -> %q", jobBefore, got)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("dialog remained %d", n)
	}

	// A stale revision is reported without overwriting the newer terms.
	f.send(userA, "/service terms 1")
	f.callback(userA, choiceCallback("mode", "once"), nil)
	f.callback(userA, cb("dialog", "skip"), nil) // amount
	if text := f.stub.lastText(); !strings.Contains(text, "请确认条款") {
		t.Fatalf("stale preview %q", text)
	}
	// Another writer replaces the terms after this preview was built.
	f.exec(`UPDATE period_terms SET revision=5 WHERE validity_period_id=1`)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "已被其他操作更新") {
		t.Fatalf("stale revision accepted: %q", text)
	}
	if got := serviceLookupString(t, f, "SELECT mode FROM period_terms WHERE validity_period_id=1"); got != "recurring" {
		t.Fatalf("stale terms wrote %q", got)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("stale terms lost draft %d", n)
	}
	f.send(userA, "/cancel")
}

func TestRuleWizardAddEditDisableAndIdempotence(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-12-31")

	f.send(userA, "/service rule add 1")
	f.send(userA, "7")
	f.send(userA, "09:00")
	f.callback(userA, choiceCallback("enabled", "true"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if n := f.count("SELECT count(*) FROM reminder_rules WHERE validity_period_id=1"); n != 1 {
		t.Fatalf("rules %d", n)
	}
	ruleID := serviceLookupInt(t, f, "SELECT id FROM reminder_rules WHERE validity_period_id=1")

	f.callback(userA, cb("rule", "edit", id(ruleID)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "当前：提前 7 天 09:00") {
		t.Fatalf("edit screen %q", text)
	}
	f.send(userA, "5")
	f.send(userA, "10:30")
	f.callback(userA, choiceCallback("enabled", "false"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT days_before||':'||reminder_time||':'||enabled||':'||revision FROM reminder_rules WHERE id=?", ruleID); got != "5:10:30:0:2" {
		t.Fatalf("edited rule %q", got)
	}

	// Ask, then disable once; repeating the button reports the current state
	// without creating a second rule or bumping the revision again.
	// The wizard already disabled it; enabling again then disabling exercises
	// the ask/confirm pair against a live rule.
	f.exec(`UPDATE reminder_rules SET enabled=1,revision=revision+1 WHERE id=?`, ruleID)
	f.callback(userA, cb("rule", "disable_ask", id(ruleID)), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "禁用规则 #"+id(ruleID)) {
		t.Fatalf("disable confirmation %q", text)
	}
	f.callback(userA, cb("rule", "disable", id(ruleID)), nil)
	if got := serviceLastAnswer(t, f); got != "规则已禁用" {
		t.Fatalf("disable answer %q", got)
	}
	f.callback(userA, cb("rule", "disable", id(ruleID)), nil)
	if n := f.count("SELECT count(*) FROM reminder_rules"); n != 1 {
		t.Fatalf("repeat disable created rules %d", n)
	}
	if n := f.scalar("SELECT revision FROM reminder_rules WHERE id=?", ruleID); n != 4 {
		t.Fatalf("repeat disable bumped revision %d", n)
	}
}

func TestTimezoneWizardAndDateRecalculation(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=日期&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=时刻&date_precision=datetime&expires_at=2027-01-31T12:00:00%2B08:00")
	dateBefore := f.scalar("SELECT expires_at FROM validity_periods WHERE id=1")
	instantBefore := f.scalar("SELECT expires_at FROM validity_periods WHERE id=2")

	f.send(userA, "/settings")
	if text := f.stub.lastText(); !strings.Contains(text, "Asia/Taipei") {
		t.Fatalf("settings %q", text)
	}
	f.callback(userA, cb("settings", "timezone"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "选择常用时区") {
		t.Fatalf("timezone prompt %q", text)
	}
	f.callback(userA, choiceCallback("tz", "America/New_York"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "date 精度的截止日期会按新时区重算") {
		t.Fatalf("timezone preview %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT timezone FROM users WHERE telegram_user_id=?", userA); got != "America/New_York" {
		t.Fatalf("timezone %q", got)
	}
	if got := f.scalar("SELECT expires_at FROM validity_periods WHERE id=1"); got == dateBefore {
		t.Fatalf("date cutoff not recalculated: %d", got)
	}
	if got := f.scalar("SELECT expires_at FROM validity_periods WHERE id=2"); got != instantBefore {
		t.Fatalf("datetime instant moved: %d -> %d", instantBefore, got)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("dialog remained %d", n)
	}
}

func TestSettingsRejectsInvalidZone(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/settings")
	f.callback(userA, cb("settings", "timezone"), nil)
	f.send(userA, "Mars/Olympus")
	if text := f.stub.lastText(); !strings.Contains(text, "IANA") {
		t.Fatalf("invalid zone accepted: %q", text)
	}
	f.send(userA, "Asia/Tokyo")
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLookupString(t, f, "SELECT timezone FROM users WHERE telegram_user_id=?", userA); got != "Asia/Tokyo" {
		t.Fatalf("zone %q", got)
	}
}

func TestServiceViewCallbackIsReadOnlyAndAnswersOnce(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/service add name=Host&category_id="+id(serviceCategoryID(t, f, "vps")))
	serviceID := serviceLookupInt(t, f, "SELECT id FROM service_items WHERE name='Host'")

	sends := len(f.stub.callsOf("sendMessage"))
	for range 2 {
		answers := len(f.stub.callsOf("answerCallbackQuery"))
		f.callback(userA, cb("service", "view", id(serviceID)), nil)
		if got := len(f.stub.callsOf("answerCallbackQuery")); got != answers+1 {
			t.Fatalf("view answered %d times", got-answers)
		}
		if got := serviceLastAnswer(t, f); got != "详情已发送" {
			t.Fatalf("view answer %q", got)
		}
	}
	if got := len(f.stub.callsOf("sendMessage")); got != sends+2 {
		t.Fatalf("view sent %d messages", got-sends)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("view opened a dialog %d", n)
	}

	// Foreign, deleted, and malformed buttons never mutate state.
	deleted := serviceID + 999
	for _, tc := range []struct {
		uid  int64
		data string
		want string
	}{
		{userB, cb("service", "view", id(serviceID)), "对象不存在"},
		{userA, cb("service", "view", id(deleted)), "对象不存在"},
		{userA, cb("service", "archive", "abc"), "未知操作"},
		{userA, "v2:service:invent:1", "未知操作"},
	} {
		answers := len(f.stub.callsOf("answerCallbackQuery"))
		f.callback(tc.uid, tc.data, nil)
		if got := len(f.stub.callsOf("answerCallbackQuery")); got != answers+1 {
			t.Fatalf("%s answered %d times", tc.data, got-answers)
		}
		if got := serviceLastAnswer(t, f); !strings.Contains(got, tc.want) {
			t.Fatalf("%s answer %q want %q", tc.data, got, tc.want)
		}
	}
	if got := serviceLookupString(t, f, "SELECT status FROM service_items WHERE id=?", serviceID); got != "active" {
		t.Fatalf("invalid callback changed status %q", got)
	}

	// Archive confirmation is idempotent.
	f.callback(userA, cb("service", "archive", id(serviceID)), nil)
	if got := serviceLastAnswer(t, f); got != "服务已归档" {
		t.Fatalf("archive answer %q", got)
	}
	f.callback(userA, cb("service", "archive", id(serviceID)), nil)
	if got := serviceLastAnswer(t, f); got != "服务已归档" {
		t.Fatalf("repeat archive answer %q", got)
	}
	if got := serviceLookupString(t, f, "SELECT status FROM service_items WHERE id=?", serviceID); got != "archived" {
		t.Fatalf("repeat archive status %q", got)
	}
}

func TestServiceListCallbacksPage(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	category := id(serviceCategoryID(t, f, "vps"))
	for i := range 11 {
		f.send(userA, "/service add name=S"+id(int64(i))+"&category_id="+category)
	}
	f.send(userA, "/service list")
	sends := f.stub.callsOf("sendMessage")
	keyboard := sends[len(sends)-1].form.Get("reply_markup")
	if !strings.Contains(keyboard, "v2:service:list:") {
		t.Fatalf("list keyboard %q", keyboard)
	}
	parts := strings.Split(keyboard, "v2:service:list:")
	next := strings.SplitN(parts[1], `"`, 2)[0]
	f.callback(userA, cb("service", "list", next), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "S10") {
		t.Fatalf("second page %q", text)
	}
	// Repeating the same next-page button is a pure read.
	before := len(f.stub.callsOf("sendMessage"))
	f.callback(userA, cb("service", "list", next), nil)
	if got := len(f.stub.callsOf("sendMessage")); got != before+1 {
		t.Fatalf("page button sent %d", got-before)
	}
}

func TestReminderCancelButton(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,created_at,updated_at) VALUES(1,1,1,1,?,?,'pending',0,?,1,1)`,
		f.clock.t.Add(24*time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix(), f.clock.t.Add(24*time.Hour).Unix())
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,sent_at,created_at,updated_at) VALUES(1,1,2,1,?,?,'sent',1,1,1,1)`,
		f.clock.t.Unix(), f.clock.t.Unix())

	f.send(userA, "/reminders")
	keyboard := f.stub.callsOf("sendMessage")[len(f.stub.callsOf("sendMessage"))-1].form.Get("reply_markup")
	if !strings.Contains(keyboard, "v2:job:cancel:1") || strings.Contains(keyboard, "v2:job:cancel:2") {
		t.Fatalf("reminder keyboard %q", keyboard)
	}
	f.callback(userA, cb("job", "cancel", "1"), nil)
	if got := serviceLookupString(t, f, "SELECT status FROM reminder_jobs WHERE id=1"); got != "cancelled" {
		t.Fatalf("job status %q", got)
	}
	f.callback(userA, cb("job", "cancel", "2"), nil)
	if got := serviceLastAnswer(t, f); got != "任务已发送，无法取消" {
		t.Fatalf("sent cancel answer %q", got)
	}
	if got := serviceLookupString(t, f, "SELECT status FROM reminder_jobs WHERE id=2"); got != "sent" {
		t.Fatalf("sent job mutated %q", got)
	}
}

func TestPeriodRenewButtonMatchesWizard(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/service add name=Host&category_id="+id(serviceCategoryID(t, f, "vps")))
	f.send(userA, "/service period add 1")
	f.callback(userA, choiceCallback("period_type", "custom"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-06-30")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)

	f.callback(userA, cb("period", "renew", "1"), nil)
	if got := serviceLastAnswer(t, f); !strings.Contains(got, "请输入新的截止") {
		t.Fatalf("renew button answer %q", got)
	}
	if text := f.stub.lastText(); !strings.Contains(text, "续期：Host") {
		t.Fatalf("renew button screen %q", text)
	}
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-07-31")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	before := f.count("SELECT count(*) FROM renewals")
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := f.count("SELECT count(*) FROM renewals"); got != before+1 {
		t.Fatalf("renewal count %d", got)
	}
	// A repeated confirm cannot replay without a live dialog.
	f.callback(userA, cb("dialog", "confirm"), nil)
	if got := serviceLastAnswer(t, f); !strings.Contains(got, "流程已过期") {
		t.Fatalf("repeat confirm answer %q", got)
	}
	if got := f.count("SELECT count(*) FROM renewals"); got != before+1 {
		t.Fatalf("repeat confirm wrote renewal %d", got)
	}
}

func TestRenewRejectsAdvancedCycleAndKeepsDraft(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")

	f.send(userA, "/renew 1")
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "2027-03-01")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	// Another client advances the cycle after the preview was rendered.
	user, err := f.svc.Authenticate(t.Context(), userA, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Renew(t.Context(), user.ID, 1, service.RenewInput{ExpectedCycle: 1, DatePrecision: "date", ExpiresAt: "2027-02-01"}); err != nil {
		t.Fatal(err)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "周期已被其他操作更新") {
		t.Fatalf("stale cycle confirm: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("stale confirm wrote renewal %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("stale confirm lost draft %d", n)
	}
	f.send(userA, "/cancel")
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("cancel left dialog %d", n)
	}
}

func TestCallbackDataStaysShort(t *testing.T) {
	cases := []string{
		cb("service", "view", "9223372036854775807"), cb("period", "renewals", "9223372036854775807", "9223372036854775807"),
		cb("period", "add", "9223372036854775807"), choiceCallback("precision", "datetime"),
		cb("service", "archive_ask", "9223372036854775807"),
	}
	for _, data := range cases {
		if len(data) >= 64 {
			t.Fatalf("callback data too long: %q (%d)", data, len(data))
		}
	}
	_ = time.Second
}
