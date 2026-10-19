package bot

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram.bot.remind/internal/reminder"
	"telegram.bot.remind/internal/service"
	"telegram.bot.remind/internal/storage"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	testToken   = "123456:test-token"
	testBotName = "test_bot"
	userA       = int64(10001)
	userB       = int64(10002)
	stranger    = int64(99999)
)

type call struct {
	method string
	form   url.Values
}

type telegramStub struct {
	server    *httptest.Server
	token     string
	mu        sync.Mutex
	calls     []call
	overrides map[string]func(url.Values) string
	messageID int
}

func newStub(t *testing.T) *telegramStub {
	t.Helper()
	s := &telegramStub{token: testToken, messageID: 9001, overrides: map[string]func(url.Values) string{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *telegramStub) handle(w http.ResponseWriter, r *http.Request) {
	form := url.Values{}
	if contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil && contentType == "multipart/form-data" {
		if err := r.ParseMultipartForm(2 << 20); err == nil && r.MultipartForm != nil {
			for key, values := range r.MultipartForm.Value {
				form[key] = values
			}
		}
	}
	method := strings.TrimPrefix(r.URL.Path, "/bot"+s.token+"/")
	s.mu.Lock()
	s.calls = append(s.calls, call{method: method, form: form})
	override := s.overrides[method]
	messageID := s.messageID
	s.mu.Unlock()

	body := ""
	switch {
	case override != nil:
		body = override(form)
	case method == "getMe":
		body = `{"ok":true,"result":{"id":42,"is_bot":true,"first_name":"Remind","username":"` + testBotName + `"}}`
	case method == "sendMessage":
		body = `{"ok":true,"result":{"message_id":` + strconv.Itoa(messageID) + `,"date":1,"chat":{"id":10001,"type":"private"},"text":"ok"}}`
	case method == "answerCallbackQuery":
		body = `{"ok":true,"result":true}`
	default:
		body = `{"ok":true,"result":true}`
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func (s *telegramStub) override(method string, fn func(url.Values) string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[method] = fn
}

func (s *telegramStub) callsOf(method string) []call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []call
	for _, c := range s.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func (s *telegramStub) lastText() string {
	calls := s.callsOf("sendMessage")
	if len(calls) == 0 {
		return ""
	}
	return calls[len(calls)-1].form.Get("text")
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

type fixture struct {
	t     *testing.T
	stub  *telegramStub
	store *storage.Store
	svc   *service.Service
	bot   *telegram.Bot
	h     *Handler
	clock *clock
	path  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, stub: newStub(t), clock: &clock{t: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}, path: filepath.Join(t.TempDir(), "bot.db")}
	f.open()
	t.Cleanup(func() {
		if f.store != nil {
			_ = f.store.Close()
		}
	})
	return f
}

func (f *fixture) open() {
	f.t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		store.Close()
		f.t.Fatal(err)
	}
	allowed := func(id int64) bool { return id == userA || id == userB }
	f.store = store
	f.svc = service.New(store, "Asia/Taipei", allowed, f.clock.now)
	f.h = New(f.svc, testBotName, slog.New(slog.DiscardHandler))
	b, err := telegram.New(testToken,
		telegram.WithServerURL(f.stub.server.URL),
		telegram.WithSkipGetMe(),
		telegram.WithNotAsyncHandlers(),
		telegram.WithHTTPClient(2*time.Second, f.stub.server.Client()),
	)
	if err != nil {
		f.t.Fatal(err)
	}
	f.h.Register(b)
	f.bot = b
}

func (f *fixture) restart() {
	f.t.Helper()
	if err := f.store.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.open()
}

func (f *fixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.store.DB.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.store.DB.Exec(query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) scalar(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.store.DB.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func privateUpdate(uid int64, text string) *models.Update {
	return &models.Update{ID: 1, Message: &models.Message{
		ID: 10, From: &models.User{ID: uid, FirstName: "U"}, Chat: models.Chat{ID: uid, Type: models.ChatTypePrivate}, Date: 1, Text: text,
	}}
}

func groupUpdate(uid int64, text string) *models.Update {
	return &models.Update{ID: 2, Message: &models.Message{
		ID: 11, From: &models.User{ID: uid, FirstName: "U"}, Chat: models.Chat{ID: -100, Type: models.ChatTypeGroup}, Date: 1, Text: text,
	}}
}

func (f *fixture) send(uid int64, text string) string {
	f.t.Helper()
	f.bot.ProcessUpdate(context.Background(), privateUpdate(uid, text))
	return f.stub.lastText()
}

func callbackUpdate(uid int64, data string, message *models.Message) *models.Update {
	if message == nil {
		message = &models.Message{ID: 50, Chat: models.Chat{ID: uid, Type: models.ChatTypePrivate}, Date: 1}
	}
	return &models.Update{ID: 3, CallbackQuery: &models.CallbackQuery{
		ID: "cb-1", From: models.User{ID: uid, FirstName: "U"}, Data: data,
		Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: message},
	}}
}

func (f *fixture) callback(uid int64, data string, message *models.Message) {
	f.t.Helper()
	f.bot.ProcessUpdate(context.Background(), callbackUpdate(uid, data, message))
}

func (f *fixture) start(uid int64) {
	f.t.Helper()
	text := f.send(uid, "/start")
	if !strings.Contains(text, "SIM/eSIM") {
		f.t.Fatalf("start response: %q", text)
	}
}

func TestAuthorizedPrivateFlow(t *testing.T) {
	f := newFixture(t)
	f.start(userA)

	if n := f.count("SELECT count(*) FROM users WHERE telegram_user_id=? AND private_chat_id=?", userA, userA); n != 1 {
		t.Fatalf("user not persisted: %d", n)
	}
	if text := f.send(userA, "/add name=日本卡&card_type=esim&operator=NTT&notes=hello+world"); !strings.Contains(text, "日本卡") {
		t.Fatalf("add: %q", text)
	}
	if n := f.count("SELECT count(*) FROM service_items"); n != 1 {
		t.Fatalf("sim count %d", n)
	}
	if text := f.send(userA, "/edit period new 1 period_type=number_validity&name=号码&date_precision=date&expires_at=2027-01-31"); !strings.Contains(text, "有效期 #1") {
		t.Fatalf("period: %q", text)
	}
	if text := f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00"); !strings.Contains(text, "提前 1 天") {
		t.Fatalf("rule: %q", text)
	}
	if text := f.send(userA, "/view sim 1"); !strings.Contains(text, "日本卡") || !strings.Contains(text, "备注：hello world") {
		t.Fatalf("view sim: %q", text)
	}
	if text := f.send(userA, "/view sim 1 periods"); !strings.Contains(text, "号码") {
		t.Fatalf("view periods: %q", text)
	}
	if text := f.send(userA, "/view period 1"); !strings.Contains(text, "2027-01-31") || !strings.Contains(text, "Asia/Taipei") {
		t.Fatalf("view period: %q", text)
	}
	if text := f.send(userA, "/view period 1 rules"); !strings.Contains(text, "09:00") {
		t.Fatalf("view rules: %q", text)
	}
	if text := f.send(userA, "/renew 1 expected_cycle=1&date_precision=date&expires_at=2027-03-01&amount=1000&currency=TWD"); !strings.Contains(text, "续期完成") || !strings.Contains(text, "1000 TWD") {
		t.Fatalf("renew: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("renewal count %d", n)
	}
	if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=1"); n != 2 {
		t.Fatalf("cycle %d", n)
	}
	if text := f.send(userA, "/reminders"); !strings.Contains(text, "提醒任务") {
		t.Fatalf("reminders: %q", text)
	}
	if text := f.send(userA, "/delete sim 1"); !strings.Contains(text, "归档") {
		t.Fatalf("archive: %q", text)
	}
	if n := f.count("SELECT count(*) FROM service_items WHERE status='archived'"); n != 1 {
		t.Fatalf("archived %d", n)
	}

	if text := f.send(userA, "/delete period 1 2"); !strings.Contains(text, "取消") {
		t.Fatalf("cancel period: %q", text)
	}
	if n := f.count("SELECT count(*) FROM validity_periods WHERE status='cancelled'"); n != 1 {
		t.Fatalf("period not cancelled %d", n)
	}
	if text := f.send(userA, "/view period 1"); !strings.Contains(text, "cancelled") {
		t.Fatalf("view cancelled period: %q", text)
	}
	if text := f.send(userA, "/reminders sent"); !strings.Contains(text, "提醒任务（sent）") {
		t.Fatalf("filtered reminders: %q", text)
	}
}

func TestDeniedAndGroupChatsNeverReadOrWrite(t *testing.T) {
	f := newFixture(t)
	if text := f.send(stranger, "/start"); !strings.Contains(text, "无权限") {
		t.Fatalf("stranger: %q", text)
	}
	if n := f.count("SELECT count(*) FROM users"); n != 0 {
		t.Fatalf("stranger created user %d", n)
	}

	f.bot.ProcessUpdate(context.Background(), groupUpdate(userA, "/add name=secret&card_type=esim"))
	if text := f.stub.lastText(); !strings.Contains(text, "私聊") || strings.Contains(text, "secret") {
		t.Fatalf("group: %q", text)
	}
	if n := f.count("SELECT count(*) FROM service_items"); n != 0 {
		t.Fatalf("group wrote data %d", n)
	}
	if n := f.count("SELECT count(*) FROM users"); n != 0 {
		t.Fatalf("group created user %d", n)
	}

	// A private chat whose ID does not match the sender is not trusted either.
	mismatch := privateUpdate(userA, "/start")
	mismatch.Message.Chat.ID = userB
	f.bot.ProcessUpdate(context.Background(), mismatch)
	if text := f.stub.lastText(); !strings.Contains(text, "私聊") {
		t.Fatalf("mismatch: %q", text)
	}
	if n := f.count("SELECT count(*) FROM users"); n != 0 {
		t.Fatalf("mismatch created user %d", n)
	}

	f.start(userA)
	f.send(userA, "/add name=B卡&card_type=esim")
	if text := f.send(userB, "/view sim 1"); !strings.Contains(text, "不存在") {
		t.Fatalf("cross-user view: %q", text)
	}
	if text := f.send(userB, "/view period 1"); !strings.Contains(text, "不存在") {
		t.Fatalf("cross-user period: %q", text)
	}
}

func TestInvalidCommandInputWritesNothing(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	cases := []string{
		"/add name=only",
		"/add name=a&card_type=esim&unknown=1",
		"/add name=a&name=b&card_type=esim",
		"/add name=%zz&card_type=esim",
		"/add name=a&card_type=esim&operator=%",
		"/edit period new 1 period_type=number_validity&name=x&date_precision=date",
		"/edit rule new 1 days_before=-1&reminder_time=09:00",
		"/edit rule new 1 days_before=1&reminder_time=9:00",
		"/edit period 1 name=x",
		"/delete sim not-a-number",
		"/renew 1 expected_cycle=0&date_precision=date&expires_at=2027-03-01",
		"/renew 1 expected_cycle=1&date_precision=date&expires_at=2027-03-01&period_id=1",
		"/notacommand",
	}
	for _, input := range cases {
		before := len(f.stub.callsOf("sendMessage"))
		f.send(userA, input)
		text := f.stub.lastText()
		if text == "" || strings.Contains(text, "echo") {
			t.Fatalf("%q produced %q", input, text)
		}
		if len(f.stub.callsOf("sendMessage")) != before+1 {
			t.Fatalf("%q did not get a single reply", input)
		}
	}
	if n := f.count("SELECT count(*) FROM service_items"); n != 0 {
		t.Fatalf("invalid input created sims: %d", n)
	}
	if n := f.count("SELECT count(*) FROM validity_periods"); n != 0 {
		t.Fatalf("invalid input created periods: %d", n)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 0 {
		t.Fatalf("invalid input created renewals: %d", n)
	}
}

func TestPlainTextWithoutDialogIsNotEchoed(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	text := f.send(userA, "please echo this")
	if strings.Contains(text, "please echo this") {
		t.Fatalf("echo fallback present: %q", text)
	}
	if !strings.Contains(text, "没有进行中的对话") {
		t.Fatalf("unexpected reply: %q", text)
	}
}

func TestBotUsernameFiltering(t *testing.T) {
	f := newFixture(t)
	f.h.SetUsername("")
	f.h.Register(f.bot) // discovers the username via getMe
	if f.h.Username() != testBotName {
		t.Fatalf("username %q", f.h.Username())
	}
	before := len(f.stub.callsOf("sendMessage"))
	f.send(userA, "/start@other_bot")
	if len(f.stub.callsOf("sendMessage")) != before {
		t.Fatal("command for another bot was answered")
	}
	if text := f.send(userA, "/start@"+testBotName); !strings.Contains(text, "SIM/eSIM") {
		t.Fatalf("targeted start: %q", text)
	}
}

func TestViewPagination(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	user, err := f.svc.Authenticate(context.Background(), userA, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 11 {
		if _, err := f.svc.CreateSIM(context.Background(), user.ID, service.SIMInput{Name: "卡" + strconv.Itoa(i), CardType: "esim"}); err != nil {
			t.Fatal(err)
		}
	}
	text := f.send(userA, "/list")
	marker := "下一页：/list "
	index := strings.LastIndex(text, marker)
	if index < 0 {
		t.Fatalf("no next page: %q", text)
	}
	next := strings.TrimSpace(text[index+len(marker):])
	page := f.send(userA, "/list "+next)
	if !strings.Contains(page, "卡10") {
		t.Fatalf("last page missing item: %q", page)
	}
	if strings.Contains(page, "下一页") {
		t.Fatalf("unexpected extra page: %q", page)
	}
}

func TestRenewalWizardPreviewConfirmAndExpiry(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(1,1,1,1,?,?,'sent',1,1,1)`, f.clock.t.Add(-time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix())

	f.callback(userA, "v1:renew:1", nil)
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("dialog count %d", n)
	}
	if answered := f.stub.callsOf("answerCallbackQuery"); len(answered) != 1 || answered[0].form.Get("callback_query_id") != "cb-1" {
		t.Fatalf("answer %+v", answered)
	}
	if text := f.stub.lastText(); !strings.Contains(text, "新截止使用哪种精度") {
		t.Fatalf("wizard first screen: %q", text)
	}

	// Reads never discard an unfinished wizard.
	f.send(userA, "/list")
	f.send(userA, "/view period 1")
	f.send(userA, "/reminders")
	f.send(userA, "/settings")
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("read-only commands removed the dialog: %d", n)
	}

	// A wrong amount keeps the user on the same step without writing.
	f.callback(userA, choiceCallback("precision", "date"), nil)
	f.send(userA, "date_precision=date&expires_at=2027-03-01")
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("raw query text removed the dialog: %d", n)
	}
	if text := f.stub.lastText(); !strings.Contains(text, "日期必须是存在的 YYYY-MM-DD") {
		t.Fatalf("renew wizard did not explain the expected format: %q", text)
	}

	f.send(userA, "2027-03-01")
	if text := f.stub.lastText(); !strings.Contains(text, "本次实际续期金额") {
		t.Fatalf("amount step missing: %q", text)
	}
	f.send(userA, "abc")
	if text := f.stub.lastText(); !strings.Contains(text, "金额 币种") {
		t.Fatalf("invalid amount accepted: %q", text)
	}
	f.send(userA, "1000 TWD")
	if text := f.stub.lastText(); !strings.Contains(text, "续期备注") {
		t.Fatalf("notes step missing: %q", text)
	}
	f.callback(userA, cb("dialog", "skip"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请确认续期") || !strings.Contains(text, "1000 TWD") {
		t.Fatalf("preview missing: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 0 {
		t.Fatalf("preview wrote renewal %d", n)
	}

	// Back then forward keeps the entered values and shows the preview again.
	f.callback(userA, cb("dialog", "back"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "续期备注") {
		t.Fatalf("back landed on %q", text)
	}
	f.callback(userA, cb("dialog", "skip"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "请确认续期") {
		t.Fatalf("skip to preview: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if text := f.stub.lastText(); !strings.Contains(text, "续期完成") {
		t.Fatalf("confirm: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("renewal count %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("dialog not cleared: %d", n)
	}

	// Re-pressing the pre-renewal button must not open a new cycle.
	f.callback(userA, "v1:renew:1", nil)
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("stale callback created renewal %d", n)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("stale callback opened dialog %d", n)
	}
	answers := f.stub.callsOf("answerCallbackQuery")
	if got := answers[len(answers)-1].form.Get("text"); !strings.Contains(got, "周期已更新") {
		t.Fatalf("stale answer %q", got)
	}

	// Expired wizard: advancing the injected clock invalidates it.
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(1,1,2,1,?,?,'sent',1,1,1)`, f.clock.t.Add(-time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix())
	f.callback(userA, "v1:renew:2", nil)
	f.clock.advance(16 * time.Minute)
	text := f.send(userA, "2027-05-01")
	if !strings.Contains(text, "对话已过期") {
		t.Fatalf("expired dialog: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("expired dialog wrote renewal %d", n)
	}
}

func TestDialogSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(1,1,1,1,?,?,'sent',1,1,1)`, f.clock.t.Add(-time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix())
	f.callback(userA, "v1:renew:1", nil)
	f.callback(userA, choiceCallback("precision", "date"), nil)

	f.restart()
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("dialog lost across restart: %d", n)
	}
	f.send(userA, "2027-04-01")
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	f.callback(userA, cb("dialog", "confirm"), nil)
	text := f.stub.lastText()
	if !strings.Contains(text, "续期完成") {
		t.Fatalf("post-restart renew: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("renewals after restart %d", n)
	}
}

func TestWriteWizardRequiresExplicitReplacement(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(1,1,1,1,?,?,'sent',1,1,1)`, f.clock.t.Add(-time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix())

	f.callback(userA, "v1:renew:1", nil)
	before := f.count("SELECT count(*) FROM bot_dialogs")
	f.send(userA, "/service add")
	text := f.stub.lastText()
	if !strings.Contains(text, "继续当前") || !strings.Contains(text, "取消并开始") {
		t.Fatalf("replacement prompt: %q", text)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != before {
		t.Fatalf("replacement prompt changed dialogs: %d", n)
	}
	if got := serviceLookupString(t, f, "SELECT action FROM bot_dialogs"); got != "renew" {
		t.Fatalf("draft replaced without confirmation: %q", got)
	}

	f.callback(userA, cb("dialog", "keep"), nil)
	if got := serviceLookupString(t, f, "SELECT action FROM bot_dialogs"); got != "renew" {
		t.Fatalf("keep changed flow: %q", got)
	}
	if text := f.stub.lastText(); !strings.Contains(text, "新截止使用哪种精度") {
		t.Fatalf("keep did not render renew: %q", text)
	}

	f.send(userA, "/service add")
	f.callback(userA, cb("dialog", "replace"), nil)
	if got := serviceLookupString(t, f, "SELECT action FROM bot_dialogs"); got != "service_add" {
		t.Fatalf("replace kept old flow: %q", got)
	}
	if text := f.stub.lastText(); !strings.Contains(text, "添加服务") {
		t.Fatalf("replace did not start the new flow: %q", text)
	}
}

func TestCallbackAnswersEveryBranch(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,created_at,updated_at) VALUES(1,1,1,1,?,?,'sent',1,1,1)`, f.clock.t.Add(-time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix())

	cases := []struct {
		name string
		uid  int64
		data string
		msg  *models.Message
		want string
	}{
		{"malformed", userA, "v1:renew:abc", nil, "未知操作"},
		{"unknown action", userA, "v1:delete:1", nil, "未知操作"},
		{"inline only", userA, "v1:view:1", nil, "请在私聊中使用"},
		{"unauthorized", stranger, "v1:view:1", nil, "无权限"},
		{"missing job", userA, "v1:view:999", nil, "不存在"},
		{"group message", userA, "v1:view:1", &models.Message{ID: 60, Chat: models.Chat{ID: -100, Type: models.ChatTypeGroup}, Date: 1}, "请在私聊中使用"},
		{"view", userA, "v1:view:1", nil, "详情已发送"},
	}
	for _, tc := range cases {
		update := callbackUpdate(tc.uid, tc.data, tc.msg)
		if tc.name == "inline only" {
			update.CallbackQuery.Message = models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeInaccessibleMessage, InaccessibleMessage: &models.InaccessibleMessage{Chat: models.Chat{ID: userA, Type: models.ChatTypePrivate}}}
		}
		before := len(f.stub.callsOf("answerCallbackQuery"))
		f.bot.ProcessUpdate(context.Background(), update)
		answers := f.stub.callsOf("answerCallbackQuery")
		if len(answers) != before+1 {
			t.Fatalf("%s: answered %d times", tc.name, len(answers)-before)
		}
		if got := answers[len(answers)-1].form.Get("text"); !strings.Contains(got, tc.want) {
			t.Fatalf("%s: answer %q, want %q", tc.name, got, tc.want)
		}
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("callbacks changed dialogs: %d", n)
	}
}

func TestCallbackDataLength(t *testing.T) {
	for _, data := range []string{"v1:view:1", "v1:renew:9223372036854775807"} {
		if len(data) >= 64 {
			t.Fatalf("callback data too long: %q", data)
		}
	}
}

func TestSenderSuccessAndClassification(t *testing.T) {
	stub := newStub(t)
	b, err := telegram.New(testToken, telegram.WithServerURL(stub.server.URL), telegram.WithSkipGetMe(), telegram.WithHTTPClient(2*time.Second, stub.server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	sender := NewSender(b, slog.New(slog.DiscardHandler))

	messageID, err := sender.SendReminder(context.Background(), userA, "提醒正文", 7)
	if err != nil {
		t.Fatal(err)
	}
	if messageID != 9001 {
		t.Fatalf("message id %d", messageID)
	}
	sent := stub.callsOf("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("send calls %d", len(sent))
	}
	if sent[0].form.Get("chat_id") != strconv.FormatInt(userA, 10) || sent[0].form.Get("text") != "提醒正文" {
		t.Fatalf("form %+v", sent[0].form)
	}
	markup := sent[0].form.Get("reply_markup")
	if !strings.Contains(markup, "v1:renew:7") || !strings.Contains(markup, "v1:view:7") {
		t.Fatalf("markup %q", markup)
	}

	stub.override("sendMessage", func(url.Values) string {
		return `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7}}`
	})
	_, err = sender.SendReminder(context.Background(), userA, "x", 1)
	var delivery *reminder.DeliveryError
	if !errors.As(err, &delivery) || delivery.Code != "rate_limited" || delivery.RetryAfter != 7*time.Second || delivery.Permanent {
		t.Fatalf("429 classification %#v", delivery)
	}

	stub.override("sendMessage", func(url.Values) string {
		return `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	})
	_, err = sender.SendReminder(context.Background(), userA, "x", 1)
	if !errors.As(err, &delivery) || delivery.Code != "remote_rejected" || !delivery.Permanent {
		t.Fatalf("403 classification %#v", delivery)
	}

	stub.override("sendMessage", func(url.Values) string {
		return `{"ok":false,"error_code":500,"description":"Internal Server Error"}`
	})
	_, err = sender.SendReminder(context.Background(), userA, "x", 1)
	if !errors.As(err, &delivery) || delivery.Code != "remote_unavailable" || delivery.Permanent {
		t.Fatalf("500 classification %#v", delivery)
	}

	if got := delivery.Error(); strings.Contains(got, testToken) || strings.Contains(got, "api.telegram.org") {
		t.Fatalf("leaked detail: %q", got)
	}
}

func TestEditAndCancelWorkflows(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.send(userA, "/add name=卡&card_type=esim")
	f.send(userA, "/edit period new 1 period_type=number_validity&name=项目&date_precision=date&expires_at=2027-01-31")
	f.send(userA, "/edit rule new 1 days_before=1&reminder_time=09:00")
	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,attempts,created_at,updated_at) VALUES(1,1,1,1,?,?,'pending',?,0,1,1)`, f.clock.t.Add(24*time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix(), f.clock.t.Add(24*time.Hour).Unix())

	if text := f.send(userA, "/edit sim 1 name=新名称&operator=NTT"); !strings.Contains(text, "新名称") || !strings.Contains(text, "NTT") {
		t.Fatalf("edit sim: %q", text)
	}
	if text := f.send(userA, "/edit period 1 expected_cycle=1&name=新项目"); !strings.Contains(text, "新项目") || !strings.Contains(text, "版本：1") {
		t.Fatalf("edit period description: %q", text)
	}
	if text := f.send(userA, "/edit period 1 expected_cycle=1&expires_at=2027-02-28"); !strings.Contains(text, "版本：2") {
		t.Fatalf("edit period expiry: %q", text)
	}
	if n := f.scalar("SELECT cycle FROM validity_periods WHERE id=1"); n != 2 {
		t.Fatalf("cycle %d", n)
	}
	if n := f.count("SELECT count(*) FROM reminder_jobs WHERE status='cancelled'"); n != 1 {
		t.Fatalf("old job not cancelled %d", n)
	}
	if text := f.send(userA, "/edit period 1 expected_cycle=1&expires_at=2027-05-01"); !strings.Contains(text, "冲突") {
		t.Fatalf("stale cycle should conflict: %q", text)
	}

	if text := f.send(userA, "/edit rule 1 expected_revision=1&reminder_time=10:30"); !strings.Contains(text, "10:30") || !strings.Contains(text, "版本 2") {
		t.Fatalf("edit rule: %q", text)
	}
	if text := f.send(userA, "/edit rule 1 expected_revision=1&reminder_time=11:00"); !strings.Contains(text, "冲突") {
		t.Fatalf("stale revision should conflict: %q", text)
	}

	if text := f.send(userA, "/cancel rule 1 2"); !strings.Contains(text, "已禁用") {
		t.Fatalf("cancel rule: %q", text)
	}
	if n := f.scalar("SELECT enabled FROM reminder_rules WHERE id=1"); n != 0 {
		t.Fatalf("rule still enabled %d", n)
	}
	if text := f.send(userA, "/cancel rule 1 2"); !strings.Contains(text, "已禁用") {
		t.Fatalf("repeat disable: %q", text)
	}

	f.exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,attempts,created_at,updated_at) VALUES(1,1,2,2,?,?,'pending',?,0,1,1)`, f.clock.t.Add(24*time.Hour).Unix(), f.clock.t.Add(48*time.Hour).Unix(), f.clock.t.Add(24*time.Hour).Unix())
	if text := f.send(userA, "/cancel job 2"); !strings.Contains(text, "已取消") {
		t.Fatalf("cancel job: %q", text)
	}
	if n := f.count("SELECT count(*) FROM reminder_jobs WHERE id=2 AND status='cancelled'"); n != 1 {
		t.Fatalf("job not cancelled %d", n)
	}
	if text := f.send(userA, "/cancel job 2"); !strings.Contains(text, "已取消") {
		t.Fatalf("repeat cancel: %q", text)
	}

	if text := f.send(userA, "/edit timezone America/New_York"); !strings.Contains(text, "America/New_York") {
		t.Fatalf("timezone: %q", text)
	}
	if text := f.send(userA, "/help"); !strings.Contains(text, "America/New_York") {
		t.Fatalf("help should show new timezone: %q", text)
	}
	if text := f.send(userA, "/edit timezone Not/AZone"); !strings.Contains(text, "IANA") {
		t.Fatalf("invalid timezone: %q", text)
	}
}

func TestChunksRespectsRuneLimit(t *testing.T) {
	text := strings.Repeat("卡", 25)
	parts := chunks(text, 10)
	if len(parts) != 3 || parts[0] != strings.Repeat("卡", 10) || parts[2] != strings.Repeat("卡", 5) {
		t.Fatalf("chunks %q", parts)
	}
}

func TestStoppedHandlerAcknowledgesCallbacksOnly(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.h.Stop()

	sends := len(f.stub.callsOf("sendMessage"))
	f.bot.ProcessUpdate(context.Background(), privateUpdate(userA, "/list"))
	if len(f.stub.callsOf("sendMessage")) != sends {
		t.Fatal("stopped handler still sent a reply")
	}

	before := len(f.stub.callsOf("answerCallbackQuery"))
	f.callback(userA, "v1:view:1", nil)
	answers := f.stub.callsOf("answerCallbackQuery")
	if len(answers) != before+1 || !strings.Contains(answers[len(answers)-1].form.Get("text"), "重启") {
		t.Fatalf("stopped callback was not acknowledged: %+v", answers)
	}
}
