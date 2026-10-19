package bot

import (
	"strings"
	"testing"
	"time"
)

// serviceCategoryID resolves a built-in category by its stable migration code.
func serviceCategoryID(t *testing.T, f *fixture, code string) int64 {
	t.Helper()
	return serviceLookupInt(t, f, "SELECT id FROM service_categories WHERE code=?", code)
}

func serviceLookupInt(t *testing.T, f *fixture, query string, args ...any) int64 {
	t.Helper()
	var v int64
	if err := f.store.DB.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func serviceLookupString(t *testing.T, f *fixture, query string, args ...any) string {
	t.Helper()
	var v string
	if err := f.store.DB.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func serviceLastAnswer(t *testing.T, f *fixture) string {
	t.Helper()
	answers := f.stub.callsOf("answerCallbackQuery")
	if len(answers) == 0 {
		t.Fatal("callback was not answered")
	}
	return answers[len(answers)-1].form.Get("text")
}

func serviceMustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("reply %q does not contain %q", text, want)
		}
	}
}

func TestServiceGenericLifecycleAndRenewalDialog(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	vpsCategory := serviceCategoryID(t, f, "vps")
	serviceMustContain(t, f.send(userA, "/help"), "服务与订阅有效期提醒机器人", "/service add", "旧 SIM 命令", "/add name=")

	created := f.send(userA, "/service add name=VPS+Pro&category_id="+id(vpsCategory)+
		"&provider=Host&identifier=vps.example.com&management_url=https%3A%2F%2Fconsole.example.com&notes=family")
	serviceMustContain(t, created, "服务 #", "VPS Pro", "VPS 与云服务器", "Host", "vps.example.com",
		"https://console.example.com", "family", "（未配置；不会发送提醒）")
	serviceID := serviceLookupInt(t, f, "SELECT id FROM service_items WHERE user_id=(SELECT id FROM users WHERE telegram_user_id=?) AND name='VPS Pro'", userA)

	first := f.send(userA, "/service period new "+id(serviceID)+" period_type=custom&name=月付&date_precision=date&expires_at=2027-01-31")
	serviceMustContain(t, first, "有效期 #", "月付", "2027-01-31", "约定条款：custom（版本 1）")
	second := f.send(userA, "/service period new "+id(serviceID)+" period_type=custom&name=年付&date_precision=date&expires_at=2027-12-31")
	serviceMustContain(t, second, "年付", "2027-12-31")
	firstPeriod := serviceLookupInt(t, f, "SELECT id FROM validity_periods WHERE service_id=? AND name='月付'", serviceID)
	secondPeriod := serviceLookupInt(t, f, "SELECT id FROM validity_periods WHERE service_id=? AND name='年付'", serviceID)

	detail := f.send(userA, "/service view "+id(serviceID))
	serviceMustContain(t, detail, "VPS Pro", "月付", "年付", "约定条款：custom（版本 1）",
		"/service period new "+id(serviceID))
	if n := f.count("SELECT count(*) FROM validity_periods WHERE service_id=?", serviceID); n != 2 {
		t.Fatalf("period count %d", n)
	}

	rule := f.send(userA, "/edit rule new "+id(firstPeriod)+" days_before=1&reminder_time=09:00")
	serviceMustContain(t, rule, "规则 #", "提前 1 天", "09:00")
	secondRule := f.send(userA, "/edit rule new "+id(secondPeriod)+" days_before=7&reminder_time=10:00")
	serviceMustContain(t, secondRule, "提前 7 天")
	firstRule := serviceLookupInt(t, f, "SELECT id FROM reminder_rules WHERE validity_period_id=?", firstPeriod)
	secondRuleID := serviceLookupInt(t, f, "SELECT id FROM reminder_rules WHERE validity_period_id=?", secondPeriod)

	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(?,?,1,1,?,?,'sent',1,1,1)`,
		firstPeriod, firstRule, f.clock.t.Add(-time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix())
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(?,?,1,1,?,?,'sent',1,1,1)`,
		secondPeriod, secondRuleID, f.clock.t.Add(-time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix())
	firstJob := serviceLookupInt(t, f, "SELECT id FROM reminder_jobs WHERE validity_period_id=?", firstPeriod)

	// Repeating the view button is idempotent and answers every press.
	sends := len(f.stub.callsOf("sendMessage"))
	for range 2 {
		f.callback(userA, "v1:view:"+id(firstJob), nil)
		if got := serviceLastAnswer(t, f); !strings.Contains(got, "详情已发送") {
			t.Fatalf("repeated view answer %q", got)
		}
	}
	if got := len(f.stub.callsOf("sendMessage")); got != sends+2 {
		t.Fatalf("repeated view sent %d messages", got-sends)
	}

	// Repeating the renew button reuses the one live dialog instead of stacking.
	for range 2 {
		f.callback(userA, "v1:renew:"+id(firstJob), nil)
		if got := serviceLastAnswer(t, f); !strings.Contains(got, "请输入新的截止日期") {
			t.Fatalf("repeated renew answer %q", got)
		}
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("dialog count after repeated renew %d", n)
	}
	if n := f.scalar("SELECT expected_cycle FROM bot_dialogs WHERE user_id=(SELECT id FROM users WHERE telegram_user_id=?)", userA); n != 1 {
		t.Fatalf("dialog expected cycle %d", n)
	}

	invalidTerms := f.send(userA, "/service terms "+id(firstPeriod)+" expected_cycle=1&mode=recurring")
	serviceMustContain(t, invalidTerms, "expected_terms_revision")
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("invalid terms query removed dialog %d", n)
	}

	// Terms-only edits keep the pending renewal dialog and never touch jobs or cycle.
	terms := f.send(userA, "/service terms "+id(firstPeriod)+
		" expected_cycle=1&expected_terms_revision=1&mode=recurring&interval_count=1&interval_unit=month&amount=2999&currency=USD")
	serviceMustContain(t, terms, "约定条款：recurring（版本 2）", "每 1 month", "2999 USD")
	if strings.Contains(terms, "已取消未完成的续期对话") {
		t.Fatalf("terms-only edit cleared the dialog: %q", terms)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("terms-only edit cleared the dialog row: %d", n)
	}
	if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=?", firstPeriod); n != 1 {
		t.Fatalf("terms-only edit changed cycle %d", n)
	}
	if got := serviceLookupString(t, f, "SELECT status FROM reminder_jobs WHERE id=?", firstJob); got != "sent" {
		t.Fatalf("terms-only edit changed job status %q", got)
	}

	f.restart()
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("dialog lost across restart: %d", n)
	}
	missingCutoff := f.send(userA, "amount=2999&currency=USD")
	serviceMustContain(t, missingCutoff, "续期失败", "expires_at")
	if n := f.count("SELECT count(*) FROM renewals"); n != 0 {
		t.Fatalf("missing explicit cutoff wrote renewal %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("missing cutoff removed dialog %d", n)
	}

	renewed := f.send(userA, "date_precision=date&expires_at=2027-02-28")
	serviceMustContain(t, renewed, "续期完成", "当前状态：active，版本 2", "约定条款：recurring（版本 2）")
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("renewal count %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("dialog not cleared after renewal: %d", n)
	}
	if n := f.count("SELECT count(*) FROM renewals WHERE validity_period_id=? AND amount IS NULL AND currency IS NULL", firstPeriod); n != 1 {
		t.Fatalf("terms silently became actual renewal money %d", n)
	}

	// The pre-renewal button is acknowledged but cannot mutate the new cycle.
	for _, action := range []string{"view", "renew"} {
		f.callback(userA, "v1:"+action+":"+id(firstJob), nil)
		if got := serviceLastAnswer(t, f); !strings.Contains(got, "该提醒所属周期已更新") {
			t.Fatalf("stale %s answer %q", action, got)
		}
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("stale button wrote renewal %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("stale button opened dialog %d", n)
	}

	// Explicit renewal of the second generic period keeps its independent history.
	explicit := f.send(userA, "/renew "+id(secondPeriod)+" expected_cycle=1&date_precision=date&expires_at=2028-01-31&amount=199&currency=USD")
	serviceMustContain(t, explicit, "续期完成", "199 USD")
	if n := f.count("SELECT count(*) FROM renewals WHERE validity_period_id=?", secondPeriod); n != 1 {
		t.Fatalf("second period renewals %d", n)
	}
	if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=?", secondPeriod); n != 2 {
		t.Fatalf("second period cycle %d", n)
	}
	if got := serviceLookupString(t, f, "SELECT mode FROM period_terms WHERE validity_period_id=?", firstPeriod); got != "recurring" {
		t.Fatalf("terms mode %q", got)
	}
}

func TestServiceCategoryRenameAndOwnership(t *testing.T) {
	f := newFixture(t)
	f.start(userA)

	list := f.send(userA, "/service categories")
	serviceMustContain(t, list, "SIM 与 eSIM", "VPS 与云服务器", "软件与 AI 订阅", "域名", "其他", "内置只读")

	created := f.send(userA, "/service category name=家庭+订阅")
	serviceMustContain(t, created, "类别 #", "家庭 订阅", "generic")
	categoryID := serviceLookupInt(t, f, "SELECT id FROM service_categories WHERE user_id=(SELECT id FROM users WHERE telegram_user_id=?) AND name='家庭 订阅'", userA)

	renamed := f.send(userA, "/service category-edit "+id(categoryID)+" name=媒体+订阅")
	serviceMustContain(t, renamed, "媒体 订阅", "generic")
	if got := serviceLookupString(t, f, "SELECT name FROM service_categories WHERE id=?", categoryID); got != "媒体 订阅" {
		t.Fatalf("category rename not persisted: %q", got)
	}
	if n := f.count("SELECT count(*) FROM service_categories WHERE user_id=(SELECT id FROM users WHERE telegram_user_id=?) AND name='家庭 订阅'", userA); n != 0 {
		t.Fatalf("old category name retained %d", n)
	}

	// A renamed custom category labels newly created services with the new name.
	service := f.send(userA, "/service add name=Netflix&category_id="+id(categoryID))
	serviceMustContain(t, service, "媒体 订阅", "Netflix")

	// Built-in categories stay read-only.
	simCategory := serviceCategoryID(t, f, "sim")
	builtIn := f.send(userA, "/service category-edit "+id(simCategory)+" name=改SIM")
	if !strings.Contains(builtIn, "冲突") {
		t.Fatalf("built-in rename: %q", builtIn)
	}
	if got := serviceLookupString(t, f, "SELECT name FROM service_categories WHERE id=?", simCategory); got != "SIM 与 eSIM" {
		t.Fatalf("built-in category changed: %q", got)
	}

	// Another allowed user cannot rename a private category.
	f.start(userB)
	foreign := f.send(userB, "/service category-edit "+id(categoryID)+" name=劫持")
	if !strings.Contains(foreign, "不存在") {
		t.Fatalf("foreign rename: %q", foreign)
	}
	if got := serviceLookupString(t, f, "SELECT name FROM service_categories WHERE id=?", categoryID); got != "媒体 订阅" {
		t.Fatalf("foreign rename changed name: %q", got)
	}
	if n := f.count("SELECT count(*) FROM service_categories WHERE user_id=(SELECT id FROM users WHERE telegram_user_id=?) AND name='劫持'", userB); n != 0 {
		t.Fatalf("foreign category written %d", n)
	}
}

func TestServiceFlatSIMFields(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	simCategory := serviceCategoryID(t, f, "sim")
	vpsCategory := serviceCategoryID(t, f, "vps")

	add := "/service add name=eSIM+旅行&category_id=" + id(simCategory) +
		"&card_type=esim&phone_number=%2B886912345678&country_or_region=日本&iccid=89860012345678901234&provider=KDDI&notes=keep+private"
	created := f.send(userA, add)
	serviceMustContain(t, created, "SIM 与 eSIM", "SIM 类型：esim", "电话：+886912345678", "国家/地区：日本",
		"ICCID：89860012345678901234", "服务商：KDDI", "备注：keep private")
	simID := serviceLookupInt(t, f, "SELECT id FROM service_items WHERE user_id=(SELECT id FROM users WHERE telegram_user_id=?) AND name='eSIM 旅行'", userA)
	if got := serviceLookupString(t, f, "SELECT card_type FROM sim_details WHERE service_id=?", simID); got != "esim" {
		t.Fatalf("stored card type %q", got)
	}
	if got := serviceLookupString(t, f, "SELECT phone_number FROM sim_details WHERE service_id=?", simID); got != "+886912345678" {
		t.Fatalf("stored phone %q", got)
	}

	// A service created through the generic command remains visible to legacy readers.
	legacy := f.send(userA, "/view sim "+id(simID))
	serviceMustContain(t, legacy, "eSIM 旅行", "类型：esim", "运营商：KDDI", "+886912345678")
	serviceMustContain(t, f.send(userA, "/list"), "eSIM 旅行")

	// Flat SIM keys patch the nested detail row, including explicit clearing.
	cleared := f.send(userA, "/service edit "+id(simID)+" phone_number=&country_or_region=")
	serviceMustContain(t, cleared, "电话：-", "国家/地区：-")
	if n := f.count("SELECT count(*) FROM sim_details WHERE service_id=? AND phone_number IS NULL AND country_or_region IS NULL", simID); n != 1 {
		t.Fatalf("cleared fields not stored as NULL %d", n)
	}

	// card_type cannot be cleared and cannot be supplied for a generic category.
	invalid := f.send(userA, "/service edit "+id(simID)+" card_type=")
	serviceMustContain(t, invalid, "输入无效")
	if got := serviceLookupString(t, f, "SELECT card_type FROM sim_details WHERE service_id=?", simID); got != "esim" {
		t.Fatalf("card type after invalid edit %q", got)
	}
	before := f.count("SELECT count(*) FROM service_items")
	for _, input := range []string{
		"/service add name=missing&category_id=" + id(simCategory),
		"/service add name=badcard&category_id=" + id(simCategory) + "&card_type=foo",
		"/service add name=wrongkind&category_id=" + id(vpsCategory) + "&card_type=esim",
	} {
		text := f.send(userA, input)
		serviceMustContain(t, text, "输入无效")
	}
	if after := f.count("SELECT count(*) FROM service_items"); after != before {
		t.Fatalf("invalid flat fields created %d services", after-before)
	}
}

func TestServiceTermsParsingErrorsAndVersions(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,locked_until,attempt_token,created_at,updated_at) VALUES(1,1,1,1,?,?,'pending',2,?,?,'lease',1,1)`,
		f.clock.t.Add(24*time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix(), f.clock.t.Add(24*time.Hour).Unix(), f.clock.t.Add(time.Minute).Unix())

	unchanged := func(label string) {
		t.Helper()
		if n := f.count("SELECT count(*) FROM period_terms"); n != 0 {
			t.Fatalf("%s: wrote terms %d", label, n)
		}
		if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=1"); n != 1 {
			t.Fatalf("%s: changed cycle %d", label, n)
		}
		if got := serviceLookupString(t, f, "SELECT status FROM reminder_jobs WHERE id=1"); got != "pending" {
			t.Fatalf("%s: changed job status %q", label, got)
		}
		if got := serviceLookupString(t, f, "SELECT attempt_token FROM reminder_jobs WHERE id=1"); got != "lease" {
			t.Fatalf("%s: changed attempt token %q", label, got)
		}
	}

	cases := []struct {
		name string
		args string
		want string
	}{
		{"missing cycle", "expected_terms_revision=1&mode=custom", "缺少参数 expected_cycle"},
		{"missing revision", "expected_cycle=1&mode=custom", "缺少参数 expected_terms_revision"},
		{"zero cycle", "expected_cycle=0&expected_terms_revision=1&mode=custom", "expected_cycle 必须是"},
		{"zero revision", "expected_cycle=1&expected_terms_revision=0&mode=custom", "expected_terms_revision 必须是"},
		{"bad mode", "expected_cycle=1&expected_terms_revision=1&mode=weekly", "mode 必须是"},
		{"recurring missing count", "expected_cycle=1&expected_terms_revision=1&mode=recurring", "缺少参数 interval_count"},
		{"recurring zero count", "expected_cycle=1&expected_terms_revision=1&mode=recurring&interval_count=0&interval_unit=month", "interval_count 必须是"},
		{"recurring bad unit", "expected_cycle=1&expected_terms_revision=1&mode=recurring&interval_count=1&interval_unit=fortnight", "interval_unit 必须是"},
		{"amount without currency", "expected_cycle=1&expected_terms_revision=1&mode=custom&amount=100", "amount 与 currency 必须同时提供"},
		{"currency without amount", "expected_cycle=1&expected_terms_revision=1&mode=custom&currency=USD", "amount 与 currency 必须同时提供"},
		{"negative amount", "expected_cycle=1&expected_terms_revision=1&mode=custom&amount=-1&currency=USD", "amount 必须是"},
		{"bad currency", "expected_cycle=1&expected_terms_revision=1&mode=custom&amount=100&currency=US", "currency 必须是"},
		{"unknown key", "expected_cycle=1&expected_terms_revision=1&mode=custom&revision=2", "未知的参数"},
		{"overflow amount", "expected_cycle=1&expected_terms_revision=1&mode=custom&amount=9223372036854775808&currency=USD", "amount 必须是"},
		{"invalid escaping", "expected_cycle=1&expected_terms_revision=1&mode=custom&amount=%zz&currency=USD", "编码无效"},
		{"duplicate key", "expected_cycle=1&expected_terms_revision=1&mode=custom&mode=once", "不能重复"},
	}
	for _, tc := range cases {
		sends := len(f.stub.callsOf("sendMessage"))
		text := f.send(userA, "/service terms 1 "+tc.args)
		serviceMustContain(t, text, tc.want)
		if got := len(f.stub.callsOf("sendMessage")); got != sends+1 {
			t.Fatalf("%s: sent %d replies", tc.name, got-sends)
		}
		unchanged(tc.name)
	}

	applied := f.send(userA, "/service terms 1 expected_cycle=1&expected_terms_revision=1&mode=recurring&interval_count=1&interval_unit=month&amount=100&currency=usd")
	serviceMustContain(t, applied, "约定条款：recurring（版本 2）", "每 1 month", "100 USD")
	if n := f.scalar("SELECT revision FROM period_terms WHERE validity_period_id=1"); n != 2 {
		t.Fatalf("terms revision %d", n)
	}
	if got := serviceLookupString(t, f, "SELECT mode FROM period_terms WHERE validity_period_id=1"); got != "recurring" {
		t.Fatalf("terms mode %q", got)
	}
	if n := f.scalar("SELECT interval_count FROM period_terms WHERE validity_period_id=1"); n != 1 {
		t.Fatalf("interval count %d", n)
	}
	if got := serviceLookupString(t, f, "SELECT currency FROM period_terms WHERE validity_period_id=1"); got != "USD" {
		t.Fatalf("currency %q", got)
	}
	if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=1"); n != 1 {
		t.Fatalf("terms-only changed cycle %d", n)
	}
	if n := f.count("SELECT count(*) FROM reminder_jobs WHERE id=1 AND status='pending' AND attempts=2 AND attempt_token='lease'"); n != 1 {
		t.Fatalf("terms-only changed the job %d", n)
	}

	staleRevision := f.send(userA, "/service terms 1 expected_cycle=1&expected_terms_revision=1&mode=once")
	serviceMustContain(t, staleRevision, "冲突")
	staleCycle := f.send(userA, "/service terms 1 expected_cycle=2&expected_terms_revision=2&mode=once")
	serviceMustContain(t, staleCycle, "冲突")
	if got := serviceLookupString(t, f, "SELECT mode FROM period_terms WHERE validity_period_id=1"); got != "recurring" {
		t.Fatalf("stale request changed terms to %q", got)
	}

	reset := f.send(userA, "/service terms 1 expected_cycle=1&expected_terms_revision=2&mode=custom&interval_count=12&interval_unit=month")
	serviceMustContain(t, reset, "约定条款：custom（版本 3）")
	if n := f.scalar("SELECT revision FROM period_terms WHERE validity_period_id=1"); n != 3 {
		t.Fatalf("reset revision %d", n)
	}
	if n := f.count("SELECT count(*) FROM period_terms WHERE validity_period_id=1 AND interval_count IS NULL AND interval_unit IS NULL AND amount IS NULL AND currency IS NULL"); n != 1 {
		t.Fatalf("reset did not null nullable terms %d", n)
	}
	if n := f.count("SELECT count(*) FROM reminder_jobs WHERE id=1 AND status='pending' AND attempts=2"); n != 1 {
		t.Fatalf("reset changed the job %d", n)
	}
}

func TestServiceEditRestoresArchived(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	vpsCategory := serviceCategoryID(t, f, "vps")
	f.send(userA, "/service add name=Host&category_id="+id(vpsCategory))
	serviceID := serviceLookupInt(t, f, "SELECT id FROM service_items WHERE user_id=(SELECT id FROM users WHERE telegram_user_id=?) AND name='Host'", userA)
	f.send(userA, "/service period new "+id(serviceID)+" period_type=custom&name=月付&date_precision=date&expires_at=2027-03-01")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,created_at,updated_at) VALUES(1,1,1,1,?,?,'pending',0,?,1,1)`,
		f.clock.t.Add(24*time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix(), f.clock.t.Add(24*time.Hour).Unix())

	archived := f.send(userA, "/service archive "+id(serviceID))
	serviceMustContain(t, archived, "已归档", "/service edit "+id(serviceID)+" status=active")
	if got := serviceLookupString(t, f, "SELECT status FROM service_items WHERE id=?", serviceID); got != "archived" {
		t.Fatalf("status %q", got)
	}
	if n := f.count("SELECT count(*) FROM reminder_jobs WHERE status='cancelled'"); n != 1 {
		t.Fatalf("archive did not cancel jobs %d", n)
	}

	list := f.send(userA, "/service list")
	if strings.Contains(list, "Host") || !strings.Contains(list, "（无）") {
		t.Fatalf("archived service still listed: %q", list)
	}
	view := f.send(userA, "/service view "+id(serviceID))
	serviceMustContain(t, view, "Host", "状态：archived", "月付")

	restored := f.send(userA, "/service edit "+id(serviceID)+" status=active")
	serviceMustContain(t, restored, "Host", "状态：active")
	if got := serviceLookupString(t, f, "SELECT status FROM service_items WHERE id=?", serviceID); got != "active" {
		t.Fatalf("restore status %q", got)
	}
	if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=1"); n != 2 {
		t.Fatalf("restore should bump cycle to invalidate old buttons, got %d", n)
	}
	serviceMustContain(t, f.send(userA, "/service list"), "Host")

	again := f.send(userA, "/service edit "+id(serviceID)+" status=active")
	serviceMustContain(t, again, "状态：active")
	if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=1"); n != 2 {
		t.Fatalf("no-op restore changed cycle %d", n)
	}
	badStatus := f.send(userA, "/service edit "+id(serviceID)+" status=deleted")
	serviceMustContain(t, badStatus, "输入无效")
	if got := serviceLookupString(t, f, "SELECT status FROM service_items WHERE id=?", serviceID); got != "active" {
		t.Fatalf("invalid status changed service to %q", got)
	}
}

func TestServiceLegacyCommandsRejectGenericService(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	vpsCategory := serviceCategoryID(t, f, "vps")
	f.send(userA, "/service add name=VPS&category_id="+id(vpsCategory))
	genericID := serviceLookupInt(t, f, "SELECT id FROM service_items WHERE user_id=(SELECT id FROM users WHERE telegram_user_id=?) AND name='VPS'", userA)

	legacyCreation := f.send(userA, "/edit period new "+id(genericID)+" period_type=custom&name=越权&date_precision=date&expires_at=2027-01-31")
	serviceMustContain(t, legacyCreation, "不存在")
	if n := f.count("SELECT count(*) FROM validity_periods WHERE service_id=?", genericID); n != 0 {
		t.Fatalf("legacy entry created a generic period %d", n)
	}
	for _, input := range []string{
		"/view sim " + id(genericID),
		"/edit sim " + id(genericID) + " name=changed",
		"/delete sim " + id(genericID),
	} {
		text := f.send(userA, input)
		serviceMustContain(t, text, "不存在")
	}
	if got := serviceLookupString(t, f, "SELECT status FROM service_items WHERE id=?", genericID); got != "active" {
		t.Fatalf("legacy delete changed generic status %q", got)
	}
	if got := serviceLookupString(t, f, "SELECT name FROM service_items WHERE id=?", genericID); got != "VPS" {
		t.Fatalf("legacy edit changed generic name %q", got)
	}

	// The generic entry still creates periods for the same service.
	genericPeriod := f.send(userA, "/service period new "+id(genericID)+" period_type=custom&name=月付&date_precision=date&expires_at=2027-01-31")
	serviceMustContain(t, genericPeriod, "月付")
	genericPeriodID := serviceLookupInt(t, f, "SELECT id FROM validity_periods WHERE service_id=?", genericID)
	emptyName := f.send(userA, "/edit period "+id(genericPeriodID)+" expected_cycle=1&name=")
	serviceMustContain(t, emptyName, "名称：有效期")
	if got := serviceLookupString(t, f, "SELECT name FROM validity_periods WHERE id=?", genericPeriodID); got != "" {
		t.Fatalf("generic period name not cleared: %q", got)
	}

	// Legacy SIM creation and period creation keep working after the guard.
	f.send(userA, "/add name=卡&card_type=esim")
	simID := serviceLookupInt(t, f, "SELECT id FROM service_items WHERE user_id=(SELECT id FROM users WHERE telegram_user_id=?) AND name='卡'", userA)
	legacyPeriod := f.send(userA, "/edit period new "+id(simID)+" period_type=number_validity&name=号码&date_precision=date&expires_at=2027-01-31")
	serviceMustContain(t, legacyPeriod, "有效期 #", "号码")
	if n := f.count("SELECT count(*) FROM validity_periods WHERE service_id=?", simID); n != 1 {
		t.Fatalf("legacy SIM period count %d", n)
	}
}

func TestServiceCallbacksRejectForeignAndStaleJobs(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(1,1,1,1,?,?,'sent',1,1,1)`,
		f.clock.t.Add(-time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix())

	// Foreign callbacks are acknowledged exactly once and never open dialogs.
	sends := len(f.stub.callsOf("sendMessage"))
	for _, action := range []string{"view", "renew"} {
		before := len(f.stub.callsOf("answerCallbackQuery"))
		f.callback(userB, "v1:"+action+":1", nil)
		if got := len(f.stub.callsOf("answerCallbackQuery")); got != before+1 {
			t.Fatalf("foreign %s answered %d times", action, got-before)
		}
		serviceMustContain(t, serviceLastAnswer(t, f), "对象不存在")
	}
	if got := len(f.stub.callsOf("sendMessage")); got != sends {
		t.Fatalf("foreign callbacks sent %d messages", got-sends)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("foreign callbacks opened dialog %d", n)
	}
	if text := f.send(userB, "/service view 1"); !strings.Contains(text, "不存在") {
		t.Fatalf("foreign service view: %q", text)
	}

	// Owner renewal moves the period to cycle 2.
	renewed := f.send(userA, "/renew 1 expected_cycle=1&date_precision=date&expires_at=2027-03-01")
	serviceMustContain(t, renewed, "续期完成")
	oldJob := serviceLookupInt(t, f, "SELECT id FROM reminder_jobs WHERE validity_period_id=1 AND cycle=1")

	for _, action := range []string{"view", "renew"} {
		before := len(f.stub.callsOf("answerCallbackQuery"))
		f.callback(userA, "v1:"+action+":"+id(oldJob), nil)
		if got := len(f.stub.callsOf("answerCallbackQuery")); got != before+1 {
			t.Fatalf("stale %s answered %d times", action, got-before)
		}
		serviceMustContain(t, serviceLastAnswer(t, f), "该提醒所属周期已更新")
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("stale callback wrote renewal %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("stale callback opened dialog %d", n)
	}

	// Archiving the parent also invalidates a still-valid cycle.
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(1,1,2,1,?,?,'sent',1,1,1)`,
		f.clock.t.Add(-time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix())
	currentJob := serviceLookupInt(t, f, "SELECT id FROM reminder_jobs WHERE validity_period_id=1 AND cycle=2")
	f.send(userA, "/service archive 1")
	before := len(f.stub.callsOf("answerCallbackQuery"))
	f.callback(userA, "v1:renew:"+id(currentJob), nil)
	if got := len(f.stub.callsOf("answerCallbackQuery")); got != before+1 {
		t.Fatalf("archived parent answered %d times", got-before)
	}
	serviceMustContain(t, serviceLastAnswer(t, f), "该提醒所属周期已更新")
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("archived parent opened dialog %d", n)
	}
}
