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
	if !strings.Contains(text, "服务与订阅") {
		f.t.Fatalf("start response: %q", text)
	}
}

func (f *fixture) serviceUser(uid int64) service.User {
	f.t.Helper()
	u, err := f.svc.Authenticate(context.Background(), uid, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	return u
}

func (f *fixture) createService(name, categoryCode string) service.ServiceItem {
	f.t.Helper()
	u := f.serviceUser(userA)
	var categoryID int64
	if err := f.store.DB.QueryRow("SELECT id FROM service_categories WHERE code=?", categoryCode).Scan(&categoryID); err != nil {
		f.t.Fatal(err)
	}
	item, err := f.svc.CreateService(context.Background(), u.ID, service.ServiceInput{Name: name, CategoryID: categoryID})
	if err != nil {
		f.t.Fatal(err)
	}
	return item
}

func (f *fixture) createSIM(name string) service.ServiceItem {
	f.t.Helper()
	u := f.serviceUser(userA)
	item, err := f.svc.CreateService(context.Background(), u.ID, service.ServiceInput{
		Name: name, CategoryID: serviceCategoryID(f.t, f, "sim"),
		SIMDetails: &service.SIMDetails{CardType: "esim"},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return item
}

func (f *fixture) createPeriod(serviceID int64, periodType, name, precision, expires string) service.Period {
	f.t.Helper()
	u := f.serviceUser(userA)
	p, err := f.svc.CreatePeriod(context.Background(), u.ID, serviceID, service.PeriodInput{
		PeriodType: periodType, Name: name, DatePrecision: precision, ExpiresAt: expires,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) createRule(periodID int64, days int, at string) service.Rule {
	f.t.Helper()
	u := f.serviceUser(userA)
	r, err := f.svc.CreateRule(context.Background(), u.ID, periodID, service.RuleInput{DaysBefore: days, ReminderTime: at})
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func TestDeniedAndGroupChatsNeverReadOrWrite(t *testing.T) {
	f := newFixture(t)
	if text := f.send(stranger, "/start"); !strings.Contains(text, "无权限") {
		t.Fatalf("stranger: %q", text)
	}
	if n := f.count("SELECT count(*) FROM users"); n != 0 {
		t.Fatalf("stranger created user %d", n)
	}

	f.bot.ProcessUpdate(context.Background(), groupUpdate(userA, "/service add"))
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
	f.createSIM("B卡")
	f.callback(userB, cb("service", "view", "1"), nil)
	if got := serviceLastAnswer(t, f); !strings.Contains(got, "不存在") {
		t.Fatalf("cross-user view: %q", got)
	}
	f.callback(userB, cb("period", "view", "1"), nil)
	if got := serviceLastAnswer(t, f); !strings.Contains(got, "不存在") {
		t.Fatalf("cross-user period: %q", got)
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
	if text := f.send(userA, "/start@"+testBotName); !strings.Contains(text, "服务与订阅") {
		t.Fatalf("targeted start: %q", text)
	}
}

func TestRenewalWizardPreviewConfirmAndExpiry(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createSIM("卡")
	f.createPeriod(1, "number_validity", "项目", "date", "2027-01-31")

	f.callback(userA, cb("period", "renew", "1"), nil)
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
	f.send(userA, "/service list")
	f.callback(userA, cb("period", "view", "1"), nil)
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

	// A new renew wizard starts from the advanced cycle and can be cancelled.
	f.callback(userA, cb("period", "renew", "1"), nil)
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 1 {
		t.Fatalf("repeat renew did not open a dialog: %d", n)
	}
	f.send(userA, "/cancel")

	// Expired wizard: advancing the injected clock invalidates it.
	f.callback(userA, cb("period", "renew", "1"), nil)
	f.clock.advance(16 * time.Minute)
	text := f.send(userA, "2027-05-01")
	if !strings.Contains(text, "对话已过期") {
		t.Fatalf("expired dialog: %q", text)
	}
	if n := f.count("SELECT count(*) FROM renewals"); n != 1 {
		t.Fatalf("expired dialog wrote renewal %d", n)
	}

	// A cancelled period is rejected before opening a wizard.
	uid := f.serviceUser(userA).ID
	if err := f.svc.CancelPeriod(context.Background(), uid, 1, 2); err != nil {
		t.Fatal(err)
	}
	f.callback(userA, cb("period", "renew", "1"), nil)
	if got := serviceLastAnswer(t, f); !strings.Contains(got, "已取消") {
		t.Fatalf("cancelled period answer %q", got)
	}
	if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
		t.Fatalf("cancelled period opened dialog %d", n)
	}
}

func TestDialogSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	f.createSIM("卡")
	f.createPeriod(1, "number_validity", "项目", "date", "2027-01-31")
	f.callback(userA, cb("period", "renew", "1"), nil)
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
	f.createSIM("卡")
	f.createPeriod(1, "number_validity", "项目", "date", "2027-01-31")

	f.callback(userA, cb("period", "renew", "1"), nil)
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
	f.createSIM("卡")
	f.createPeriod(1, "number_validity", "项目", "date", "2027-01-31")

	cases := []struct {
		name string
		uid  int64
		data string
		msg  *models.Message
		want string
	}{
		{"malformed", userA, "v2:period:renew:abc", nil, "未知操作"},
		{"unknown scope", userA, "v2:delete:view:1", nil, "未知操作"},
		{"inline only", userA, "v2:period:view:1", nil, "请在私聊中使用"},
		{"unauthorized", stranger, "v2:period:view:1", nil, "无权限"},
		{"missing period", userA, "v2:period:view:999", nil, "不存在"},
		{"group message", userA, "v2:period:view:1", &models.Message{ID: 60, Chat: models.Chat{ID: -100, Type: models.ChatTypeGroup}, Date: 1}, "请在私聊中使用"},
		{"view", userA, "v2:period:view:1", nil, "详情已发送"},
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
	for _, data := range []string{"v2:period:view:1", "v2:period:renew:9223372036854775807", "v2:dialog:set:precision:2"} {
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

	messageID, err := sender.SendReminder(context.Background(), userA, "提醒正文", 7, 9)
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
	if !strings.Contains(markup, "v2:period:renew:9") || !strings.Contains(markup, "v2:period:view:9") {
		t.Fatalf("markup %q", markup)
	}

	stub.override("sendMessage", func(url.Values) string {
		return `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7}}`
	})
	_, err = sender.SendReminder(context.Background(), userA, "x", 1, 2)
	var delivery *reminder.DeliveryError
	if !errors.As(err, &delivery) || delivery.Code != "rate_limited" || delivery.RetryAfter != 7*time.Second || delivery.Permanent {
		t.Fatalf("429 classification %#v", delivery)
	}

	stub.override("sendMessage", func(url.Values) string {
		return `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	})
	_, err = sender.SendReminder(context.Background(), userA, "x", 1, 2)
	if !errors.As(err, &delivery) || delivery.Code != "remote_rejected" || !delivery.Permanent {
		t.Fatalf("403 classification %#v", delivery)
	}

	stub.override("sendMessage", func(url.Values) string {
		return `{"ok":false,"error_code":500,"description":"Internal Server Error"}`
	})
	_, err = sender.SendReminder(context.Background(), userA, "x", 1, 2)
	if !errors.As(err, &delivery) || delivery.Code != "remote_unavailable" || delivery.Permanent {
		t.Fatalf("500 classification %#v", delivery)
	}

	if got := delivery.Error(); strings.Contains(got, testToken) || strings.Contains(got, "api.telegram.org") {
		t.Fatalf("leaked detail: %q", got)
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
	f.bot.ProcessUpdate(context.Background(), privateUpdate(userA, "/service list"))
	if len(f.stub.callsOf("sendMessage")) != sends {
		t.Fatal("stopped handler still sent a reply")
	}

	before := len(f.stub.callsOf("answerCallbackQuery"))
	f.callback(userA, cb("period", "view", "1"), nil)
	answers := f.stub.callsOf("answerCallbackQuery")
	if len(answers) != before+1 || !strings.Contains(answers[len(answers)-1].form.Get("text"), "重启") {
		t.Fatalf("stopped callback was not acknowledged: %+v", answers)
	}
}
