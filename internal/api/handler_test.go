package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"telegram.bot.remind/internal/service"
	"telegram.bot.remind/internal/storage"
)

const testToken = "123456:api-test-token"

var testNow = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

func testAuth(id int64) string {
	v := url.Values{"auth_date": {strconv.FormatInt(testNow.Unix(), 10)}, "user": {fmt.Sprintf(`{"id":%d,"first_name":"測試 + %%"}`, id)}}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+v.Get(k))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(testToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return "tma " + v.Encode()
}
func setupAPI(t *testing.T) (*Handler, *storage.Store, *service.Service) {
	t.Helper()
	st, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := service.New(st, "Asia/Taipei", func(id int64) bool { return id == 101 || id == 202 }, func() time.Time { return testNow })
	h := New(s, Config{BotToken: testToken, AuthMaxAge: 5 * time.Minute, Now: func() time.Time { return testNow }, Ready: func() bool { return true }, Ping: st.DB.PingContext})
	return h, st, s
}
func request(t *testing.T, h http.Handler, method, path, body string, id int64, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if id != 0 {
		r.Header.Set("Authorization", testAuth(id))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, w.Code, want, w.Body.String())
	}
	if want == 204 && w.Body.Len() != 0 {
		t.Fatal("204 response has a body")
	}
	return w
}
func data(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}
func TestResourceLifecycle(t *testing.T) {
	h, st, _ := setupAPI(t)
	me := data(t, request(t, h, "GET", "/api/v1/me", "", 101, 200))
	if me["telegram_user_id"] != "101" || me["has_private_chat"] != false {
		t.Fatalf("me=%v", me)
	}
	categories := resourceTestCategories(t, h, 101)
	sim := data(t, request(t, h, "POST", "/api/v1/services", `{"name":" 日本卡 ","category_id":"`+categories["vps"]+`","notes":"private","provider":"carrier"}`, 101, 201))
	sid := sim["id"].(string)
	if sim["name"] != "日本卡" {
		t.Fatalf("service=%v", sim)
	}
	patched := data(t, request(t, h, "PATCH", "/api/v1/services/"+sid, `{"notes":null}`, 101, 200))
	if patched["notes"] != nil || patched["provider"] != "carrier" {
		t.Fatalf("null/absent patch=%v", patched)
	}
	period := data(t, request(t, h, "POST", "/api/v1/services/"+sid+"/periods", `{"name":"number","date_precision":"date","expires_at":"2027-01-31"}`, 101, 201))
	pid := period["id"].(string)
	if period["expires_at"] != "2027-01-31T16:00:00Z" || period["local_expires_date"] != "2027-01-31" {
		t.Fatalf("expiration=%v", period)
	}
	rule := data(t, request(t, h, "POST", "/api/v1/periods/"+pid+"/rules", `{"days_before":1,"reminder_time":"09:00"}`, 101, 201))
	rid := rule["id"].(string)
	if rule["enabled"] != true {
		t.Fatal("rule not enabled by default")
	}
	request(t, h, "GET", "/api/v1/rules/"+rid, "", 101, 200)
	request(t, h, "PATCH", "/api/v1/rules/"+rid, `{"expected_revision":1,"days_before":2}`, 101, 200)
	request(t, h, "PATCH", "/api/v1/rules/"+rid, `{"expected_revision":1,"days_before":3}`, 101, 409)
	request(t, h, "PATCH", "/api/v1/periods/"+pid, `{"expected_cycle":1,"name":"renamed"}`, 101, 200)
	request(t, h, "GET", "/api/v1/services/"+sid+"/periods", "", 101, 200)
	request(t, h, "GET", "/api/v1/periods/"+pid+"/rules", "", 101, 200)

	result, err := st.DB.Exec(`INSERT INTO reminder_jobs(period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,created_at,updated_at) VALUES(?,?,1,2,1,1801411200,'pending',1,1,1)`, pid, rid)
	if err != nil {
		t.Fatal(err)
	}
	jid, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	jobPath := "/api/v1/reminders/" + strconv.FormatInt(jid, 10)
	request(t, h, "GET", jobPath, "", 101, 200)
	request(t, h, "GET", "/api/v1/reminders?status=pending", "", 101, 200)
	request(t, h, "POST", jobPath+"/cancel", "", 101, 204)
	request(t, h, "POST", jobPath+"/cancel", "", 101, 204)
	if _, err := st.DB.Exec("UPDATE reminder_jobs SET status='sent' WHERE id=?", jid); err != nil {
		t.Fatal(err)
	}
	request(t, h, "POST", jobPath+"/cancel", "", 101, 409)

	body := `{"expected_cycle":1,"date_precision":"date","expires_at":"2027-03-01","amount":0,"currency":"TWD"}`
	first := data(t, request(t, h, "POST", "/api/v1/periods/"+pid+"/renewals", body, 101, 201))
	replayed := data(t, request(t, h, "POST", "/api/v1/periods/"+pid+"/renewals", body, 101, 200))
	if first["id"] != replayed["id"] {
		t.Fatal("renewal replay changed identity")
	}
	request(t, h, "GET", "/api/v1/periods/"+pid+"/renewals", "", 101, 200)
	request(t, h, "PATCH", "/api/v1/periods/"+pid, `{"expected_cycle":1,"name":"stale"}`, 101, 409)
	request(t, h, "DELETE", "/api/v1/rules/"+rid+"?expected_revision=2", "", 101, 204)
	request(t, h, "DELETE", "/api/v1/periods/"+pid+"?expected_cycle=2", "", 101, 204)
	request(t, h, "DELETE", "/api/v1/services/"+sid, "", 101, 204)
	request(t, h, "GET", "/api/v1/services/"+sid, "", 101, 404)
	request(t, h, "PATCH", "/api/v1/services/"+sid, `{"status":"active"}`, 101, 404)
}
func TestAuthenticationOwnershipAndErrors(t *testing.T) {
	h, _, _ := setupAPI(t)
	request(t, h, "GET", "/api/v1/me", "", 0, 401)
	request(t, h, "GET", "/api/v1/me", "", 303, 403)
	request(t, h, "GET", "/api/v1/me?initData="+url.QueryEscape(testAuth(101)), "", 0, 401)
	categories := resourceTestCategories(t, h, 101)
	sim := data(t, request(t, h, "POST", "/api/v1/services", `{"name":"private","category_id":"`+categories["sim"]+`","sim_details":{"card_type":"esim","iccid":"secret-iccid"}}`, 101, 201))
	path := "/api/v1/services/" + sim["id"].(string)
	for _, suffix := range []string{"", "/periods"} {
		w := request(t, h, "GET", path+suffix, "", 202, 404)
		if strings.Contains(w.Body.String(), "secret-iccid") || strings.Contains(w.Body.String(), testToken) {
			t.Fatal("sensitive error output")
		}
	}
	request(t, h, "PATCH", path, `{"name":"stolen"}`, 202, 404)
	request(t, h, "DELETE", path, "", 202, 404)
	for _, body := range []string{`{"name":null}`, `{"card_type":null}`, `{"status":null}`, `{"unknown":1}`, `{} {}`, `null`, `[]`, `{"user_id":"202"}`} {
		request(t, h, "PATCH", path, body, 101, 400)
	}
	request(t, h, "POST", "/api/v1/services", `{"name":"x","category_id":"`+categories["vps"]+`","notes":"`+strings.Repeat("x", 65536)+`"}`, 101, 413)
	request(t, h, "POST", "/api/v1/services", `{"name":"`+strings.Repeat("中", 201)+`","category_id":"`+categories["vps"]+`"}`, 101, 400)
	r := httptest.NewRequest("POST", "/api/v1/services", strings.NewReader(`{}`))
	r.Header.Set("Authorization", testAuth(101))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatalf("content type=%d", w.Code)
	}
	request(t, h, "PUT", path, `{}`, 101, 405)
	request(t, h, "GET", "/api/v1/not-a-route", "", 101, 404)
	request(t, h, "GET", "/api/v1/services/1.5", "", 101, 400)
	for _, q := range []string{"limit=0", "limit=101", "limit=1&limit=2", "after_id=-1", "after_id=1.2", "after_id=%zz", "unknown=1", "limit=", "limit"} {
		request(t, h, "GET", "/api/v1/services?"+q, "", 101, 400)
	}
}
func TestPaginationAndReadiness(t *testing.T) {
	h, _, _ := setupAPI(t)
	categories := resourceTestCategories(t, h, 101)
	w := request(t, h, "GET", "/api/v1/services", "", 101, 200)
	if !strings.Contains(w.Body.String(), `"data":[]`) || !strings.Contains(w.Body.String(), `"next_cursor":null`) {
		t.Fatalf("empty page=%s", w.Body.String())
	}
	for i := range 21 {
		request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"card %d","category_id":"%s"}`, i, categories["vps"]), 101, 201)
	}
	w = request(t, h, "GET", "/api/v1/services", "", 101, 200)
	var first struct {
		Data []map[string]any `json:"data"`
		Next *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Data) != 20 || first.Next == nil {
		t.Fatalf("first=%+v", first)
	}
	w = request(t, h, "GET", "/api/v1/services?after_id="+*first.Next, "", 101, 200)
	var second struct {
		Data []map[string]any `json:"data"`
		Next *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Data) != 1 || second.Next != nil {
		t.Fatalf("second=%+v", second)
	}
	request(t, h, "GET", "/healthz", "", 0, 200)
	request(t, h, "GET", "/readyz", "", 0, 200)
	h.config.Ready = func() bool { return false }
	request(t, h, "GET", "/readyz", "", 0, 503)
	h.config.Ready = func() bool { return true }
	h.config.Ping = func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("missing bounded probe")
		}
		return errors.New("secret SQL failure")
	}
	w = request(t, h, "GET", "/readyz", "", 0, 503)
	if strings.Contains(w.Body.String(), "SQL") {
		t.Fatal("probe leaks database details")
	}
}

func TestNestedOwnershipAndPagination(t *testing.T) {
	h, st, s := setupAPI(t)
	ctx := context.Background()
	u, err := s.Authenticate(ctx, 101, nil)
	if err != nil {
		t.Fatal(err)
	}
	categories := resourceTestCategories(t, h, 101)
	var categoryID int64
	if _, err := fmt.Sscan(categories["sim"], &categoryID); err != nil {
		t.Fatal(err)
	}
	sim, err := s.CreateService(ctx, u.ID, service.ServiceInput{Name: "nested", CategoryID: categoryID, SIMDetails: &service.SIMDetails{CardType: "esim"}})
	if err != nil {
		t.Fatal(err)
	}
	var first service.Period
	for i := range 11 {
		p, err := s.CreatePeriod(ctx, u.ID, sim.ID, service.PeriodInput{Name: "period", DatePrecision: "date", ExpiresAt: "2028-01-31"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = p
		}
	}
	var firstRule service.Rule
	for i := range 11 {
		r, err := s.CreateRule(ctx, u.ID, first.ID, service.RuleInput{DaysBefore: i, ReminderTime: "09:00"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstRule = r
		}
	}
	for i := range 11 {
		_, err := s.Renew(ctx, u.ID, first.ID, service.RenewInput{ExpectedCycle: int64(i + 1), DatePrecision: "date", ExpiresAt: time.Date(2028, time.Month(2+i), 28, 0, 0, 0, 0, time.UTC).Format("2006-01-02")})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		"/api/v1/services/" + identifier(sim.ID) + "/periods",
		"/api/v1/periods/" + identifier(first.ID) + "/rules",
		"/api/v1/periods/" + identifier(first.ID) + "/renewals",
	} {
		w := request(t, h, "GET", path+"?limit=10", "", 101, 200)
		var firstPage struct {
			Data []json.RawMessage `json:"data"`
			Next *string           `json:"next_cursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &firstPage); err != nil {
			t.Fatal(err)
		}
		if len(firstPage.Data) != 10 || firstPage.Next == nil {
			t.Fatalf("page %s: %s", path, w.Body.String())
		}
		w = request(t, h, "GET", path+"?limit=10&after_id="+*firstPage.Next, "", 101, 200)
		var lastPage struct {
			Data []json.RawMessage `json:"data"`
			Next *string           `json:"next_cursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &lastPage); err != nil {
			t.Fatal(err)
		}
		if len(lastPage.Data) != 1 || lastPage.Next != nil {
			t.Fatalf("last %s: %s", path, w.Body.String())
		}
		request(t, h, "GET", path+"?after_id=999999", "", 202, 404)
	}
	request(t, h, "GET", "/api/v1/periods/"+identifier(first.ID), "", 202, 404)
	request(t, h, "GET", "/api/v1/rules/"+identifier(firstRule.ID), "", 202, 404)
	request(t, h, "POST", "/api/v1/periods/"+identifier(first.ID)+"/renewals", `{"expected_cycle":12,"date_precision":"date","expires_at":"2030-01-31"}`, 202, 404)
	result, err := st.DB.Exec(`INSERT INTO reminder_jobs(period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,created_at,updated_at) VALUES(?,?,12,1,1,1906502400,'pending',1,1,1)`, first.ID, firstRule.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	request(t, h, "GET", "/api/v1/reminders/"+identifier(jobID), "", 202, 404)
	request(t, h, "POST", "/api/v1/reminders/"+identifier(jobID)+"/cancel", "", 202, 404)
}

func TestAuthenticationNeverTrustsClientIdentity(t *testing.T) {
	h, _, _ := setupAPI(t)
	for _, header := range []string{"tma invalid", "Bearer " + testAuth(101), strings.Replace(testAuth(101), "hash=", "hash=ff", 1)} {
		r := httptest.NewRequest("GET", "/api/v1/me", nil)
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("invalid credentials status=%d", w.Code)
		}
		if strings.Contains(w.Body.String(), header) || strings.Contains(w.Body.String(), testToken) {
			t.Fatal("credential disclosure")
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.Header.Add("Authorization", testAuth(101))
	r.Header.Add("Authorization", testAuth(202))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("duplicate header=%d", w.Code)
	}
	request(t, h, "PATCH", "/api/v1/me", `{"timezone":"UTC","private_chat_id":"101"}`, 101, 400)
}
