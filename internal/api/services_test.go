package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// These tests deliberately exercise both HTTP surfaces against the same graph.
// In particular, generic resources must never acquire a legacy SIM identity.
type resourceTestPage struct {
	Data []map[string]any `json:"data"`
	Next *string          `json:"next_cursor"`
}

type resourceTestOverview struct {
	Data struct {
		Counts map[string]int `json:"counts"`
		Items  []struct {
			Service       map[string]any `json:"service"`
			PrimaryPeriod map[string]any `json:"primary_period"`
		} `json:"items"`
	} `json:"data"`
	Next *string `json:"next_cursor"`
}

func resourceTestJSON(t *testing.T, w *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v: %s", err, w.Body.String())
	}
	var tree any
	if err := json.Unmarshal(w.Body.Bytes(), &tree); err != nil {
		t.Fatal(err)
	}
	var checkIDs func(any)
	checkIDs = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if (key == "id" || strings.HasSuffix(key, "_id")) && child != nil {
					id, ok := child.(string)
					if !ok {
						t.Fatalf("%s must be a JSON string: %s", key, w.Body.String())
					}
					n, err := strconv.ParseInt(id, 10, 64)
					if err != nil || n < 1 {
						t.Fatalf("invalid %s=%q: %s", key, id, w.Body.String())
					}
				}
				checkIDs(child)
			}
		case []any:
			for _, child := range value {
				checkIDs(child)
			}
		}
	}
	checkIDs(tree)
}

func resourceTestObject(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	resourceTestJSON(t, w, &envelope)
	if envelope.Data == nil {
		t.Fatalf("missing object data: %s", w.Body.String())
	}
	return envelope.Data
}

func resourceTestFields(t *testing.T, got, want map[string]any) {
	t.Helper()
	for key, value := range want {
		actual, exists := got[key]
		if !exists || !reflect.DeepEqual(actual, value) {
			t.Fatalf("field %s=%#v want=%#v; object=%v", key, actual, value, got)
		}
	}
}

func resourceTestID(t *testing.T, object map[string]any) string {
	t.Helper()
	id, ok := object["id"].(string)
	if !ok || id == "" {
		t.Fatalf("missing string id: %v", object)
	}
	return id
}

func resourceTestEnvelope(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var envelope map[string]json.RawMessage
	resourceTestJSON(t, w, &envelope)
	if len(envelope) != 2 || envelope["data"] == nil || envelope["next_cursor"] == nil {
		t.Fatalf("expected data and top-level next_cursor only: %s", w.Body.String())
	}
	if string(envelope["next_cursor"]) != "null" {
		var cursor string
		if err := json.Unmarshal(envelope["next_cursor"], &cursor); err != nil || cursor == "" {
			t.Fatalf("cursor must be string or null: %s", w.Body.String())
		}
	}
}

func resourceTestList(t *testing.T, h http.Handler, path string, user int64) resourceTestPage {
	t.Helper()
	w := request(t, h, "GET", path, "", user, 200)
	resourceTestEnvelope(t, w)
	var result resourceTestPage
	resourceTestJSON(t, w, &result)
	if result.Data == nil {
		t.Fatalf("data must be an array, including empty lists: %s", w.Body.String())
	}
	return result
}

func resourceTestSummary(t *testing.T, h http.Handler, query string, user int64) resourceTestOverview {
	t.Helper()
	w := request(t, h, "GET", "/api/v1/services/overview"+query, "", user, 200)
	resourceTestEnvelope(t, w)
	var result resourceTestOverview
	resourceTestJSON(t, w, &result)
	if result.Data.Counts == nil || result.Data.Items == nil {
		t.Fatalf("missing overview counts/items: %s", w.Body.String())
	}
	for _, item := range result.Data.Items {
		if item.Service == nil {
			t.Fatalf("overview item must contain service, not sim: %s", w.Body.String())
		}
	}
	return result
}

func resourceTestIDs(t *testing.T, rows []map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, resourceTestID(t, row))
	}
	want = append([]string{}, want...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs=%v want=%v; rows=%v", got, want, rows)
	}
}

func resourceTestCounts(t *testing.T, got map[string]int, total, due, expired, bare int) {
	t.Helper()
	want := map[string]int{"total": total, "due_soon": due, "expired": expired, "no_period": bare}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts=%v want=%v", got, want)
	}
}

func resourceTestCategories(t *testing.T, h http.Handler, user int64) map[string]string {
	t.Helper()
	page := resourceTestList(t, h, "/api/v1/categories?limit=100", user)
	if page.Next != nil {
		t.Fatal("unexpected builtin category pagination")
	}
	ids := map[string]string{}
	for _, category := range page.Data {
		code, builtin := category["code"].(string)
		if !builtin {
			continue
		}
		kind := "generic"
		if code == "sim" {
			kind = "sim"
		}
		resourceTestFields(t, category, map[string]any{"user_id": nil, "kind": kind})
		ids[code] = resourceTestID(t, category)
	}
	for _, code := range []string{"sim", "vps", "subscription", "domain", "other"} {
		if ids[code] == "" {
			t.Fatalf("missing builtin category %s: %v", code, page.Data)
		}
	}
	if len(ids) != 5 {
		t.Fatalf("unexpected builtin categories: %v", ids)
	}
	return ids
}

func resourceTestCreate(t *testing.T, h http.Handler, category, name string, user int64) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]string{"category_id": category, "name": name})
	if err != nil {
		t.Fatal(err)
	}
	result := resourceTestObject(t, request(t, h, "POST", "/api/v1/services", string(body), user, 201))
	resourceTestFields(t, result, map[string]any{"category_id": category, "name": strings.TrimSpace(name), "status": "active", "sim_details": nil})
	return result
}

func resourceTestPeriod(t *testing.T, h http.Handler, serviceID, name, expires string, user int64) map[string]any {
	t.Helper()
	body := fmt.Sprintf(`{"period_type":"custom","name":%q,"date_precision":"date","expires_at":%q}`, name, expires)
	result := resourceTestObject(t, request(t, h, "POST", "/api/v1/services/"+serviceID+"/periods", body, user, 201))
	resourceTestFields(t, result, map[string]any{"service_id": serviceID, "name": name, "local_expires_date": expires, "timezone": "Asia/Taipei", "cycle": float64(1)})
	return result
}

func resourceTestError(t *testing.T, h http.Handler, method, path, body string, user int64, status int, code string) {
	t.Helper()
	w := request(t, h, method, path, body, user, status)
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	resourceTestJSON(t, w, &envelope)
	if envelope.Error.Code != code || envelope.Error.Message == "" || strings.Contains(w.Body.String(), testToken) {
		t.Fatalf("unexpected error response: %s", w.Body.String())
	}
}

func TestServicesMixedLegacyAndGenericResources(t *testing.T) {
	h, _, _ := setupAPI(t)
	categories := resourceTestCategories(t, h, 101)
	legacy := resourceTestObject(t, request(t, h, "POST", "/api/v1/sims", `{"name":"legacy SIM","card_type":"physical_sim","operator":"legacy carrier","phone_number":"+886101","country_or_region":"TW","iccid":"legacy-secret","notes":"legacy notes"}`, 101, 201))
	legacyID := resourceTestID(t, legacy)
	bridged := resourceTestObject(t, request(t, h, "GET", "/api/v1/services/"+legacyID, "", 101, 200))
	resourceTestFields(t, bridged, map[string]any{"id": legacyID, "user_id": legacy["user_id"], "name": "legacy SIM", "category_id": categories["sim"], "provider": "legacy carrier", "identifier": nil, "management_url": nil, "notes": "legacy notes", "status": "active", "created_at": legacy["created_at"], "updated_at": legacy["updated_at"], "sim_details": map[string]any{"card_type": "physical_sim", "phone_number": "+886101", "country_or_region": "TW", "iccid": "legacy-secret"}})
	newSIM := resourceTestObject(t, request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"new SIM","category_id":%q,"provider":"new carrier","sim_details":{"card_type":"esim","phone_number":"+886202","country_or_region":"JP","iccid":"new-secret"}}`, categories["sim"]), 101, 201))
	newID := resourceTestID(t, newSIM)
	legacyNew := resourceTestObject(t, request(t, h, "GET", "/api/v1/sims/"+newID, "", 101, 200))
	resourceTestFields(t, legacyNew, map[string]any{"id": newID, "name": "new SIM", "operator": "new carrier", "card_type": "esim", "phone_number": "+886202", "country_or_region": "JP", "iccid": "new-secret"})
	custom := resourceTestObject(t, request(t, h, "POST", "/api/v1/categories", `{"name":"Infrastructure"}`, 101, 201))
	allIDs := []string{legacyID, newID}
	for _, entry := range []struct{ category, name string }{{categories["vps"], "VPS"}, {categories["subscription"], "AI subscription"}, {categories["domain"], "example domain"}, {resourceTestID(t, custom), "custom backup"}} {
		item := resourceTestCreate(t, h, entry.category, entry.name, 101)
		id := resourceTestID(t, item)
		allIDs = append(allIDs, id)
		got := resourceTestObject(t, request(t, h, "GET", "/api/v1/services/"+id, "", 101, 200))
		if !reflect.DeepEqual(got, item) {
			t.Fatalf("create/get mismatch: got=%v want=%v", got, item)
		}
		resourceTestError(t, h, "GET", "/api/v1/sims/"+id, "", 101, 404, "not_found")
		resourceTestError(t, h, "GET", "/api/v1/sims/"+id+"/periods", "", 101, 404, "not_found")
	}
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services", 101).Data, allIDs...)
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/sims", 101).Data, legacyID, newID)
	summary := resourceTestSummary(t, h, "", 101)
	resourceTestCounts(t, summary.Data.Counts, 6, 0, 0, 6)
	if len(summary.Data.Items) != 6 || summary.Next != nil {
		t.Fatalf("generic overview=%+v", summary)
	}
	for _, item := range summary.Data.Items {
		if item.PrimaryPeriod != nil {
			t.Fatalf("unexpected primary period: %v", item)
		}
	}
	legacyOverview := data(t, request(t, h, "GET", "/api/v1/overview", "", 101, 200))
	counts := legacyOverview["counts"].(map[string]any)
	resourceTestFields(t, counts, map[string]any{"total": float64(2), "due_soon": float64(0), "expired": float64(0), "no_period": float64(2)})
	legacyItems := legacyOverview["items"].([]any)
	legacyRows := make([]map[string]any, 0, len(legacyItems))
	for _, raw := range legacyItems {
		legacyRows = append(legacyRows, raw.(map[string]any)["sim"].(map[string]any))
	}
	resourceTestIDs(t, legacyRows, legacyID, newID)
	patched := resourceTestObject(t, request(t, h, "PATCH", "/api/v1/services/"+legacyID, `{"provider":"updated carrier","sim_details":{"phone_number":null}}`, 101, 200))
	resourceTestFields(t, patched, map[string]any{"provider": "updated carrier", "sim_details": map[string]any{"card_type": "physical_sim", "phone_number": nil, "country_or_region": "TW", "iccid": "legacy-secret"}})
	legacyRead := resourceTestObject(t, request(t, h, "GET", "/api/v1/sims/"+legacyID, "", 101, 200))
	resourceTestFields(t, legacyRead, map[string]any{"operator": "updated carrier", "phone_number": nil, "iccid": "legacy-secret"})
	legacyPeriod := resourceTestObject(t, request(t, h, "POST", "/api/v1/sims/"+legacyID+"/periods", `{"period_type":"number_validity","name":"legacy validity","date_precision":"date","expires_at":"2027-02-01"}`, 101, 201))
	resourceTestFields(t, legacyPeriod, map[string]any{"service_id": legacyID, "sim_id": legacyID})
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services/"+legacyID+"/periods", 101).Data, resourceTestID(t, legacyPeriod))
	newPeriod := resourceTestObject(t, request(t, h, "POST", "/api/v1/services/"+newID+"/periods", `{"period_type":"plan_validity","name":"new validity","date_precision":"date","expires_at":"2027-02-02"}`, 101, 201))
	resourceTestFields(t, newPeriod, map[string]any{"service_id": newID, "sim_id": newID})
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/sims/"+newID+"/periods", 101).Data, resourceTestID(t, newPeriod))
}

func TestServicesCategoryOwnershipAndKinds(t *testing.T) {
	h, _, _ := setupAPI(t)
	builtins := resourceTestCategories(t, h, 101)
	for _, id := range builtins {
		resourceTestError(t, h, "PATCH", "/api/v1/categories/"+id, `{"name":"renamed builtin"}`, 101, 409, "conflict")
	}
	category := resourceTestObject(t, request(t, h, "POST", "/api/v1/categories", `{"name":" Custom category "}`, 101, 201))
	cid := resourceTestID(t, category)
	resourceTestFields(t, category, map[string]any{"name": "Custom category", "kind": "generic", "code": nil})
	me := data(t, request(t, h, "GET", "/api/v1/me", "", 101, 200))
	resourceTestFields(t, category, map[string]any{"user_id": me["id"]})
	resourceTestError(t, h, "POST", "/api/v1/categories", `{"name":"Custom category"}`, 101, 409, "conflict")
	second := resourceTestObject(t, request(t, h, "POST", "/api/v1/categories", `{"name":"Second"}`, 101, 201))
	resourceTestError(t, h, "PATCH", "/api/v1/categories/"+resourceTestID(t, second), `{"name":"Custom category"}`, 101, 409, "conflict")
	other := resourceTestObject(t, request(t, h, "POST", "/api/v1/categories", `{"name":"Custom category"}`, 202, 201))
	if other["user_id"] == category["user_id"] {
		t.Fatal("custom category owner leaked across users")
	}
	foreignID := resourceTestID(t, other)
	resourceTestError(t, h, "PATCH", "/api/v1/categories/"+foreignID, `{"name":"stolen"}`, 101, 404, "not_found")
	resourceTestError(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"foreign category","category_id":%q}`, foreignID), 101, 404, "not_found")
	for _, endpoint := range []string{"/api/v1/services", "/api/v1/services/overview"} {
		for _, categoryID := range []string{foreignID, "999999999"} {
			resourceTestError(t, h, "GET", endpoint+"?category_id="+categoryID, "", 101, 404, "not_found")
		}
	}
	item := resourceTestCreate(t, h, cid, "custom resource", 101)
	id := resourceTestID(t, item)
	renamed := resourceTestObject(t, request(t, h, "PATCH", "/api/v1/categories/"+cid, `{"name":" Renamed category "}`, 101, 200))
	resourceTestFields(t, renamed, map[string]any{"id": cid, "name": "Renamed category", "user_id": category["user_id"], "kind": "generic", "code": nil, "created_at": category["created_at"]})
	listed := resourceTestList(t, h, "/api/v1/categories?limit=100", 101)
	found := false
	for _, row := range listed.Data {
		if row["id"] == foreignID {
			t.Fatalf("foreign category exposed: %v", row)
		}
		if row["id"] == cid {
			found = true
			resourceTestFields(t, row, renamed)
		}
	}
	if !found {
		t.Fatalf("renamed category not listed: %v", listed.Data)
	}
	resourceTestError(t, h, "PATCH", "/api/v1/services/"+id, fmt.Sprintf(`{"category_id":%q}`, foreignID), 101, 404, "not_found")
	resourceTestError(t, h, "PATCH", "/api/v1/services/"+id, fmt.Sprintf(`{"category_id":%q}`, builtins["sim"]), 101, 409, "conflict")
	sim := resourceTestObject(t, request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"SIM","category_id":%q,"sim_details":{"card_type":"esim"}}`, builtins["sim"]), 101, 201))
	resourceTestError(t, h, "PATCH", "/api/v1/services/"+resourceTestID(t, sim), fmt.Sprintf(`{"category_id":%q}`, cid), 101, 409, "conflict")
	moved := resourceTestObject(t, request(t, h, "PATCH", "/api/v1/services/"+id, fmt.Sprintf(`{"category_id":%q}`, builtins["domain"]), 101, 200))
	resourceTestFields(t, moved, map[string]any{"id": id, "category_id": builtins["domain"], "sim_details": nil, "name": "custom resource"})
	unchangedSIM := resourceTestObject(t, request(t, h, "GET", "/api/v1/services/"+resourceTestID(t, sim), "", 101, 200))
	if !reflect.DeepEqual(unchangedSIM, sim) {
		t.Fatalf("conflict mutated SIM: %v", unchangedSIM)
	}
}

func TestServicesLifecycleWithIndependentPeriods(t *testing.T) {
	h, _, _ := setupAPI(t)
	categories := resourceTestCategories(t, h, 101)
	item := resourceTestCreate(t, h, categories["vps"], " server ", 101)
	id := resourceTestID(t, item)
	path := "/api/v1/services/" + id
	patched := resourceTestObject(t, request(t, h, "PATCH", path, `{"name":"renamed server","provider":"Cloud provider","identifier":"host-01","management_url":"https://console.example.com/server/1","notes":"private notes"}`, 101, 200))
	resourceTestFields(t, patched, map[string]any{"id": id, "name": "renamed server", "provider": "Cloud provider", "identifier": "host-01", "management_url": "https://console.example.com/server/1", "notes": "private notes", "sim_details": nil, "created_at": item["created_at"]})
	cleared := resourceTestObject(t, request(t, h, "PATCH", path, `{"provider":null,"notes":null}`, 101, 200))
	resourceTestFields(t, cleared, map[string]any{"provider": nil, "notes": nil, "identifier": "host-01", "management_url": "https://console.example.com/server/1"})
	first := resourceTestPeriod(t, h, id, "server billing", "2027-01-04", 101)
	second := resourceTestPeriod(t, h, id, "backup storage", "2027-02-20", 101)
	p1, p2 := resourceTestID(t, first), resourceTestID(t, second)
	resourceTestFields(t, first, map[string]any{"sim_id": nil, "expires_at": "2027-01-04T16:00:00Z", "status": "active"})
	resourceTestFields(t, second, map[string]any{"sim_id": nil, "expires_at": "2027-02-20T16:00:00Z", "status": "active"})
	r1 := resourceTestObject(t, request(t, h, "POST", "/api/v1/periods/"+p1+"/rules", `{"days_before":1,"reminder_time":"09:00"}`, 101, 201))
	r2 := resourceTestObject(t, request(t, h, "POST", "/api/v1/periods/"+p2+"/rules", `{"days_before":3,"reminder_time":"10:30"}`, 101, 201))
	resourceTestFields(t, r1, map[string]any{"validity_period_id": p1, "days_before": float64(1), "enabled": true, "revision": float64(1)})
	resourceTestFields(t, r2, map[string]any{"validity_period_id": p2, "days_before": float64(3), "enabled": true, "revision": float64(1)})
	r1id, r2id := resourceTestID(t, r1), resourceTestID(t, r2)
	rulePatch := resourceTestObject(t, request(t, h, "PATCH", "/api/v1/rules/"+r1id, `{"expected_revision":1,"days_before":2}`, 101, 200))
	resourceTestFields(t, rulePatch, map[string]any{"validity_period_id": p1, "days_before": float64(2), "revision": float64(2)})
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/periods/"+p1+"/rules", 101).Data, r1id)
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/periods/"+p2+"/rules", 101).Data, r2id)
	before := resourceTestSummary(t, h, "", 101)
	if len(before.Data.Items) != 1 {
		t.Fatalf("overview=%+v", before)
	}
	resourceTestFields(t, before.Data.Items[0].PrimaryPeriod, map[string]any{"id": p1, "service_id": id})
	body := `{"expected_cycle":1,"date_precision":"date","expires_at":"2027-04-01","amount":1200,"currency":"TWD","notes":"server renewed"}`
	renewed := resourceTestObject(t, request(t, h, "POST", "/api/v1/periods/"+p1+"/renewals", body, 101, 201))
	resourceTestFields(t, renewed, map[string]any{"validity_period_id": p1, "previous_cycle": float64(1), "previous_expires_at": "2027-01-04T16:00:00Z", "new_expires_at": "2027-04-01T16:00:00Z", "amount": float64(1200), "currency": "TWD", "notes": "server renewed"})
	replayed := resourceTestObject(t, request(t, h, "POST", "/api/v1/periods/"+p1+"/renewals", body, 101, 200))
	if !reflect.DeepEqual(replayed, renewed) {
		t.Fatalf("renewal replay changed response: got=%v want=%v", replayed, renewed)
	}
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/periods/"+p1+"/renewals", 101).Data, resourceTestID(t, renewed))
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/periods/"+p2+"/renewals", 101).Data)
	firstNow := resourceTestObject(t, request(t, h, "GET", "/api/v1/periods/"+p1, "", 101, 200))
	resourceTestFields(t, firstNow, map[string]any{"cycle": float64(2), "expires_at": "2027-04-01T16:00:00Z"})
	secondNow := resourceTestObject(t, request(t, h, "GET", "/api/v1/periods/"+p2, "", 101, 200))
	if !reflect.DeepEqual(secondNow, second) {
		t.Fatalf("renewing sibling changed period: got=%v want=%v", secondNow, second)
	}
	secondRule := resourceTestObject(t, request(t, h, "GET", "/api/v1/rules/"+r2id, "", 101, 200))
	if !reflect.DeepEqual(secondRule, r2) {
		t.Fatalf("updating sibling changed rule: %v", secondRule)
	}
	after := resourceTestSummary(t, h, "", 101)
	resourceTestFields(t, after.Data.Items[0].PrimaryPeriod, map[string]any{"id": p2})
	secondRenewal := resourceTestObject(t, request(t, h, "POST", "/api/v1/periods/"+p2+"/renewals", `{"expected_cycle":1,"date_precision":"date","expires_at":"2027-03-01","amount":300,"currency":"TWD"}`, 101, 201))
	resourceTestFields(t, secondRenewal, map[string]any{"validity_period_id": p2, "previous_cycle": float64(1), "previous_expires_at": "2027-02-20T16:00:00Z", "new_expires_at": "2027-03-01T16:00:00Z", "amount": float64(300)})
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/periods/"+p1+"/renewals", 101).Data, resourceTestID(t, renewed))
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/periods/"+p2+"/renewals", 101).Data, resourceTestID(t, secondRenewal))
	firstAfterSibling := resourceTestObject(t, request(t, h, "GET", "/api/v1/periods/"+p1, "", 101, 200))
	if !reflect.DeepEqual(firstAfterSibling, firstNow) {
		t.Fatalf("second renewal mutated first period: got=%v want=%v", firstAfterSibling, firstNow)
	}
	resourceTestIDs(t, resourceTestList(t, h, path+"/periods", 101).Data, p1, p2)
	request(t, h, "DELETE", path, "", 101, 204)
	archived := resourceTestObject(t, request(t, h, "GET", path, "", 101, 200))
	resourceTestFields(t, archived, map[string]any{"id": id, "status": "archived", "name": "renamed server"})
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services", 101).Data)
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services?status=archived", 101).Data, id)
	request(t, h, "DELETE", path, "", 101, 204)
	restored := resourceTestObject(t, request(t, h, "PATCH", path, `{"status":"active"}`, 101, 200))
	resourceTestFields(t, restored, map[string]any{"id": id, "status": "active", "identifier": "host-01"})
	for _, period := range []struct {
		id    string
		cycle float64
	}{{p1, 3}, {p2, 3}} {
		got := resourceTestObject(t, request(t, h, "GET", "/api/v1/periods/"+period.id, "", 101, 200))
		resourceTestFields(t, got, map[string]any{"cycle": period.cycle, "service_id": id})
	}
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services", 101).Data, id)
	request(t, h, "POST", path+"/delete", "", 101, 204)
	for _, endpoint := range []string{path, path + "/periods", "/api/v1/periods/" + p1, "/api/v1/periods/" + p2, "/api/v1/periods/" + p1 + "/renewals", "/api/v1/rules/" + r1id, "/api/v1/rules/" + r2id} {
		resourceTestError(t, h, "GET", endpoint, "", 101, 404, "not_found")
	}
	resourceTestError(t, h, "POST", path+"/delete", "", 101, 404, "not_found")
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services?status=all", 101).Data)
	resourceTestCounts(t, resourceTestSummary(t, h, "?status=all", 101).Data.Counts, 0, 0, 0, 0)
}

func TestServicesAuthenticationAndOwnership(t *testing.T) {
	h, _, _ := setupAPI(t)
	categories := resourceTestCategories(t, h, 101)
	item := resourceTestCreate(t, h, categories["domain"], "private domain", 101)
	id := resourceTestID(t, item)
	category := resourceTestObject(t, request(t, h, "POST", "/api/v1/categories", `{"name":"Private category"}`, 101, 201))
	cid := resourceTestID(t, category)
	period := resourceTestPeriod(t, h, id, "private period", "2027-06-01", 101)
	pid := resourceTestID(t, period)
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "/api/v1/categories", ""},
		{"POST", "/api/v1/categories", `{"name":"new category"}`},
		{"PATCH", "/api/v1/categories/" + cid, `{"name":"changed"}`},
		{"GET", "/api/v1/services", ""},
		{"GET", "/api/v1/services/overview", ""},
		{"POST", "/api/v1/services", fmt.Sprintf(`{"name":"new service","category_id":%q}`, categories["domain"])},
		{"GET", "/api/v1/services/" + id, ""},
		{"PATCH", "/api/v1/services/" + id, `{"name":"stolen"}`},
		{"DELETE", "/api/v1/services/" + id, ""},
		{"POST", "/api/v1/services/" + id + "/delete", ""},
		{"GET", "/api/v1/services/" + id + "/periods", ""},
		{"POST", "/api/v1/services/" + id + "/periods", `{"name":"private","period_type":"custom","date_precision":"date","expires_at":"2027-07-01"}`},
	} {
		resourceTestError(t, h, endpoint.method, endpoint.path, endpoint.body, 0, 401, "unauthorized")
		resourceTestError(t, h, endpoint.method, endpoint.path, endpoint.body, 303, 403, "forbidden")
		if strings.Contains(endpoint.path, "/services/"+id) || endpoint.method == "PATCH" && strings.Contains(endpoint.path, "/categories/") {
			resourceTestError(t, h, endpoint.method, endpoint.path, endpoint.body, 202, 404, "not_found")
		}
	}
	resourceTestError(t, h, "GET", "/api/v1/periods/"+pid, "", 202, 404, "not_found")
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services?status=all", 202).Data)
	resourceTestCounts(t, resourceTestSummary(t, h, "?status=all", 202).Data.Counts, 0, 0, 0, 0)
	got := resourceTestObject(t, request(t, h, "GET", "/api/v1/services/"+id, "", 101, 200))
	if !reflect.DeepEqual(got, item) {
		t.Fatalf("forbidden mutations changed resource: %v", got)
	}
}

func TestServicesFilteringBeforePaginationAndOverviewCounts(t *testing.T) {
	h, _, _ := setupAPI(t)
	categories := resourceTestCategories(t, h, 101)
	var vpsIDs []string
	var dueID, expiredID, bareID, futureID, archivedID string
	for _, entry := range []struct {
		name, expires string
		archived      bool
	}{
		{"due server", "2027-01-03", false},
		{"expired server", "2026-12-30", false},
		{"bare server", "", false},
		{"future server", "2027-03-01", false},
		{"archived server", "", true},
	} {
		item := resourceTestCreate(t, h, categories["vps"], entry.name, 101)
		id := resourceTestID(t, item)
		vpsIDs = append(vpsIDs, id)
		if entry.expires != "" {
			resourceTestPeriod(t, h, id, entry.name, entry.expires, 101)
		}
		if entry.archived {
			request(t, h, "DELETE", "/api/v1/services/"+id, "", 101, 204)
		}
		switch entry.name {
		case "due server":
			dueID = id
		case "expired server":
			expiredID = id
		case "bare server":
			bareID = id
		case "future server":
			futureID = id
		case "archived server":
			archivedID = id
		}
	}
	domain := resourceTestCreate(t, h, categories["domain"], "due domain", 101)
	resourceTestPeriod(t, h, resourceTestID(t, domain), "domain registration", "2027-01-04", 101)
	foreign := resourceTestCreate(t, h, categories["vps"], "due server foreign", 202)
	resourceTestPeriod(t, h, resourceTestID(t, foreign), "foreign", "2027-01-02", 202)
	for _, tc := range []struct {
		status             string
		ids                []string
		due, expired, bare int
	}{
		{"active", []string{dueID, expiredID, bareID, futureID}, 1, 1, 1},
		{"due_soon", []string{dueID}, 1, 0, 0},
		{"expired", []string{expiredID}, 0, 1, 0},
		{"no_period", []string{bareID}, 0, 0, 1},
		{"archived", []string{archivedID}, 0, 0, 1},
		{"all", vpsIDs, 1, 1, 2},
	} {
		t.Run(tc.status, func(t *testing.T) {
			query := "?category_id=" + categories["vps"] + "&status=" + tc.status
			page := resourceTestList(t, h, "/api/v1/services"+query, 101)
			resourceTestIDs(t, page.Data, tc.ids...)
			if page.Next != nil {
				t.Fatalf("unexpected list cursor=%v", page.Next)
			}
			full := resourceTestSummary(t, h, query, 101)
			resourceTestCounts(t, full.Data.Counts, len(tc.ids), tc.due, tc.expired, tc.bare)
			var rows []map[string]any
			for _, item := range full.Data.Items {
				rows = append(rows, item.Service)
			}
			resourceTestIDs(t, rows, tc.ids...)
			first := resourceTestSummary(t, h, query+"&limit=1", 101)
			resourceTestCounts(t, first.Data.Counts, len(tc.ids), tc.due, tc.expired, tc.bare)
			if len(first.Data.Items) != 1 {
				t.Fatalf("first overview=%+v", first)
			}
			if len(tc.ids) > 1 {
				if first.Next == nil || *first.Next != resourceTestID(t, first.Data.Items[0].Service) {
					t.Fatalf("first cursor=%v items=%v", first.Next, first.Data.Items)
				}
				second := resourceTestSummary(t, h, query+"&limit=1&after_id="+*first.Next, 101)
				resourceTestCounts(t, second.Data.Counts, len(tc.ids), tc.due, tc.expired, tc.bare)
				if len(second.Data.Items) != 1 || second.Data.Items[0].Service["id"] == first.Data.Items[0].Service["id"] {
					t.Fatalf("second page=%+v", second)
				}
			} else if first.Next != nil {
				t.Fatalf("terminal cursor=%v", first.Next)
			}
			empty := resourceTestSummary(t, h, query+"&limit=1&after_id=999999999", 101)
			resourceTestCounts(t, empty.Data.Counts, len(tc.ids), tc.due, tc.expired, tc.bare)
			if len(empty.Data.Items) != 0 || empty.Next != nil {
				t.Fatalf("after end=%+v", empty)
			}
		})
	}
	defaultPage := resourceTestList(t, h, "/api/v1/services?category_id="+categories["vps"], 101)
	resourceTestIDs(t, defaultPage.Data, dueID, expiredID, bareID, futureID)
}

func TestServicesSearchFindsLaterRowsAndTreatsWildcardsLiterally(t *testing.T) {
	h, _, _ := setupAPI(t)
	categories := resourceTestCategories(t, h, 101)
	for i := range 21 {
		resourceTestCreate(t, h, categories["subscription"], fmt.Sprintf("ordinary subscription %02d", i), 101)
	}
	target := resourceTestCreate(t, h, categories["domain"], "needle after the first page", 101)
	id := resourceTestID(t, target)
	initial := resourceTestList(t, h, "/api/v1/services", 101)
	if len(initial.Data) != 20 || initial.Next == nil {
		t.Fatalf("initial page=%+v", initial)
	}
	for _, row := range initial.Data {
		if row["id"] == id {
			t.Fatal("search fixture must be beyond initial page")
		}
	}
	filtered := resourceTestList(t, h, "/api/v1/services?q=needle&limit=1", 101)
	resourceTestIDs(t, filtered.Data, id)
	if filtered.Next != nil {
		t.Fatalf("filtered terminal cursor=%v", filtered.Next)
	}
	summary := resourceTestSummary(t, h, "?q=needle&limit=1&category_id="+categories["domain"]+"&status=no_period", 101)
	resourceTestCounts(t, summary.Data.Counts, 1, 0, 0, 1)
	if len(summary.Data.Items) != 1 || summary.Data.Items[0].Service["id"] != id || summary.Data.Items[0].PrimaryPeriod != nil {
		t.Fatalf("search overview=%+v", summary)
	}
	empty := resourceTestSummary(t, h, "?q=needle&after_id="+id+"&limit=1", 101)
	resourceTestCounts(t, empty.Data.Counts, 1, 0, 0, 1)
	if len(empty.Data.Items) != 0 || empty.Next != nil {
		t.Fatalf("filtered after end=%+v", empty)
	}
	for _, tc := range []struct{ name, query string }{
		{"literal 100% complete", "%"},
		{"literal under_score", "_"},
		{`literal back\slash`, `\`},
	} {
		item := resourceTestCreate(t, h, categories["domain"], tc.name, 101)
		query := "?q=" + url.QueryEscape(tc.query)
		page := resourceTestList(t, h, "/api/v1/services"+query, 101)
		resourceTestIDs(t, page.Data, resourceTestID(t, item))
		if page.Next != nil {
			t.Fatalf("literal search cursor=%v", page.Next)
		}
		overview := resourceTestSummary(t, h, query, 101)
		resourceTestCounts(t, overview.Data.Counts, 1, 0, 0, 1)
		if len(overview.Data.Items) != 1 || overview.Data.Items[0].Service["id"] != item["id"] {
			t.Fatalf("literal search overview=%+v", overview)
		}
	}
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services?q=does-not-exist", 101).Data)
	resourceTestCounts(t, resourceTestSummary(t, h, "?q=does-not-exist", 101).Data.Counts, 0, 0, 0, 0)
}

func TestServicesRejectUnknownRepeatedAndInvalidQueries(t *testing.T) {
	h, _, _ := setupAPI(t)
	for _, path := range []string{"/api/v1/services", "/api/v1/services/overview"} {
		for _, query := range []string{
			"unknown=1", "q=x&q=y", "category_id=1&category_id=2", "status=active&status=all", "limit=1&limit=2", "after_id=0&after_id=1",
			"limit=0", "limit=101", "limit=-1", "limit=1.5", "limit=", "limit", "after_id=-1", "after_id=1.2", "after_id=", "after_id=9223372036854775808",
			"category_id=0", "category_id=-1", "category_id=1.2", "category_id=", "category_id=9223372036854775808", "status=", "status=not-a-status", "q=%zz", "q=" + url.QueryEscape(strings.Repeat("中", 201)),
		} {
			t.Run(path+"?"+query, func(t *testing.T) {
				resourceTestError(t, h, "GET", path+"?"+query, "", 101, 400, "invalid_input")
			})
		}
	}
	for _, path := range []string{"/api/v1/categories", "/api/v1/services/1", "/api/v1/services/1/periods"} {
		resourceTestError(t, h, "GET", path+"?unknown=1", "", 101, 400, "invalid_input")
	}
}

func TestServicesFieldValidationAndSearchableFields(t *testing.T) {
	h, _, _ := setupAPI(t)
	categories := resourceTestCategories(t, h, 101)
	valid := map[string]any{
		"name": "cloud node", "category_id": categories["vps"], "provider": "Nebula Cloud",
		"identifier": "host-alpha", "management_url": "https://console.example.com/host-alpha",
		"notes": "ordinary notes",
	}
	item := resourceTestObject(t, request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"cloud node","category_id":%q,"provider":"Nebula Cloud","identifier":"host-alpha","management_url":"https://console.example.com/host-alpha","notes":"ordinary notes"}`, categories["vps"]), 101, 201))
	resourceTestFields(t, item, valid)
	for _, query := range []string{"Nebula", "host-alpha"} {
		page := resourceTestList(t, h, "/api/v1/services?q="+url.QueryEscape(query), 101)
		resourceTestIDs(t, page.Data, resourceTestID(t, item))
	}
	titleOnly := resourceTestCreate(t, h, categories["vps"], "secret-bearing service", 101)
	resourceTestObject(t, request(t, h, "PATCH", "/api/v1/services/"+resourceTestID(t, titleOnly), `{"notes":"needle in notes"}`, 101, 200))
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services?q=needle", 101).Data)
	resourceTestIDs(t, resourceTestList(t, h, "/api/v1/services?q=console.example.com", 101).Data)
	long := strings.Repeat("x", 2049)
	for _, body := range []string{
		fmt.Sprintf(`{"name":"","category_id":%q}`, categories["vps"]),
		fmt.Sprintf(`{"name":"  ","category_id":%q}`, categories["vps"]),
		fmt.Sprintf(`{"name":%q,"category_id":%q}`, strings.Repeat("中", 201), categories["vps"]),
		fmt.Sprintf(`{"name":"x","category_id":%q,"provider":%q}`, categories["vps"], strings.Repeat("p", 201)),
		fmt.Sprintf(`{"name":"x","category_id":%q,"identifier":%q}`, categories["vps"], strings.Repeat("i", 201)),
		fmt.Sprintf(`{"name":"x","category_id":%q,"notes":%q}`, categories["vps"], strings.Repeat("n", 4001)),
		fmt.Sprintf(`{"name":"x","category_id":%q,"management_url":"/relative"}`, categories["vps"]),
		fmt.Sprintf(`{"name":"x","category_id":%q,"management_url":"ftp://example.com/file"}`, categories["vps"]),
		fmt.Sprintf(`{"name":"x","category_id":%q,"management_url":"https://user@example.com/"}`, categories["vps"]),
		fmt.Sprintf(`{"name":"x","category_id":%q,"management_url":"https://example.com/%s"}`, categories["vps"], long),
		fmt.Sprintf(`{"name":"x","category_id":%q,"sim_details":{"card_type":"esim"}}`, categories["vps"]),
		fmt.Sprintf(`{"name":"x","category_id":%q}`, categories["sim"]),
		fmt.Sprintf(`{"name":"x","category_id":%q,"sim_details":{"card_type":"not-a-card"}}`, categories["sim"]),
		`{"name":"x"}`,
		`{"name":"x","category_id":"0"}`,
		`{"category_id":"` + categories["vps"] + `"}`,
	} {
		resourceTestError(t, h, "POST", "/api/v1/services", body, 101, 400, "invalid_input")
	}
	id := resourceTestID(t, item)
	for _, body := range []string{
		fmt.Sprintf(`{"name":%q}`, strings.Repeat("中", 201)),
		`{"management_url":"javascript:alert(1)"}`,
		`{"management_url":"https://user@example.com/"}`,
		`{"status":"deleted"}`,
		`{"sim_details":{"card_type":"esim"}}`,
	} {
		resourceTestError(t, h, "PATCH", "/api/v1/services/"+id, body, 101, 400, "invalid_input")
	}
	resourceTestError(t, h, "PATCH", "/api/v1/services/"+id, fmt.Sprintf(`{"category_id":%q,"sim_details":{"card_type":"esim"}}`, categories["sim"]), 101, 409, "conflict")
	unchanged := resourceTestObject(t, request(t, h, "GET", "/api/v1/services/"+id, "", 101, 200))
	resourceTestFields(t, unchanged, valid)
	periodPath := "/api/v1/services/" + id + "/periods"
	for _, periodType := range []string{"number_validity", "plan_validity", "data_package_validity"} {
		resourceTestError(t, h, "POST", periodPath, fmt.Sprintf(`{"period_type":%q,"date_precision":"date","expires_at":"2027-08-01"}`, periodType), 101, 400, "invalid_input")
	}
	sim := resourceTestObject(t, request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"sim","category_id":%q,"sim_details":{"card_type":"esim"}}`, categories["sim"]), 101, 201))
	simPeriod := resourceTestObject(t, request(t, h, "POST", "/api/v1/services/"+resourceTestID(t, sim)+"/periods", `{"period_type":"plan_validity","name":"sim plan","date_precision":"date","expires_at":"2027-08-01"}`, 101, 201))
	resourceTestFields(t, simPeriod, map[string]any{"sim_id": resourceTestID(t, sim)})
	changed := resourceTestObject(t, request(t, h, "PATCH", "/api/v1/periods/"+resourceTestID(t, simPeriod), `{"expected_cycle":1,"period_type":"custom"}`, 101, 200))
	resourceTestFields(t, changed, map[string]any{"period_type": "custom", "sim_id": resourceTestID(t, sim)})
}
