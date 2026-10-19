package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func termsCategoryIDs(t *testing.T, h http.Handler, user int64) map[string]string {
	t.Helper()
	w := request(t, h, "GET", "/api/v1/categories?limit=100", "", user, 200)
	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]string, len(page.Data))
	for _, category := range page.Data {
		code, ok := category["code"].(string)
		if !ok {
			continue
		}
		id, ok := category["id"].(string)
		if !ok {
			t.Fatalf("category id is not a string: %v", category)
		}
		ids[code] = id
	}
	return ids
}
func TestPeriodTermsAPILifecycle(t *testing.T) {
	h, st, _ := setupAPI(t)
	categoryIDs := termsCategoryIDs(t, h, 101)
	vps := data(t, request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"vps","category_id":%q}`, categoryIDs["vps"]), 101, 201))
	vpsID := vps["id"].(string)

	createPeriod := func(serviceID, body string) map[string]any {
		t.Helper()
		return data(t, request(t, h, "POST", "/api/v1/services/"+serviceID+"/periods", body, 101, 201))
	}
	pOnce := createPeriod(vpsID, `{"period_type":"custom","name":"once","date_precision":"date","expires_at":"2027-01-31","terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}}`)
	pRecurring := createPeriod(vpsID, `{"period_type":"custom","name":"recurring","date_precision":"date","expires_at":"2027-02-28","terms":{"mode":"recurring","interval_count":1,"interval_unit":"month","amount":1200,"currency":"TWD"}}`)
	pCustom := createPeriod(vpsID, `{"period_type":"custom","name":"custom","date_precision":"date","expires_at":"2027-03-31","terms":{"mode":"custom","interval_count":null,"interval_unit":null,"amount":null,"currency":null}}`)
	legacy := createPeriod(vpsID, `{"period_type":"custom","name":"legacy","date_precision":"date","expires_at":"2027-04-30"}`)
	if termsView := legacy["terms"].(map[string]any); termsView["mode"] != "custom" || termsView["revision"] != float64(1) {
		t.Fatalf("legacy normalized terms=%v", termsView)
	}
	nullTerms := createPeriod(vpsID, `{"period_type":"custom","name":"null terms","date_precision":"date","expires_at":"2027-04-30","terms":null}`)
	if !reflect.DeepEqual(nullTerms["terms"], legacy["terms"]) {
		t.Fatalf("explicit null terms differ from omitted default: null=%v omitted=%v", nullTerms["terms"], legacy["terms"])
	}
	pid := pOnce["id"].(string)
	if got := pRecurring["terms"].(map[string]any)["mode"]; got != "recurring" {
		t.Fatalf("recurring terms=%v", pRecurring["terms"])
	}
	if got := pCustom["terms"].(map[string]any)["mode"]; got != "custom" {
		t.Fatalf("custom terms=%v", pCustom["terms"])
	}

	// Set up a reminder job with every mutable delivery field populated. Terms
	// updates must neither alter its delivery state nor advance the period cycle.
	rule := data(t, request(t, h, "POST", "/api/v1/periods/"+pid+"/rules", `{"days_before":1,"reminder_time":"09:00"}`, 101, 201))
	jobInsert, err := st.DB.Exec(`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,locked_until,attempt_token,sent_at,created_at,updated_at) VALUES(?,?,1,1,1,1801411200,'sending',3,2,3,'attempt-token',4,1,1)`, pOnce["id"], rule["id"])
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := jobInsert.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	var beforeJob struct {
		Status                    string
		Attempts                  int
		Next, Locked, Token, Sent *string
		Cycle                     int
	}
	readJob := func() struct {
		Status                    string
		Attempts                  int
		Next, Locked, Token, Sent *string
		Cycle                     int
	} {
		t.Helper()
		var row struct {
			Status                    string
			Attempts                  int
			Next, Locked, Token, Sent *string
			Cycle                     int
		}
		if err := st.DB.QueryRow(`SELECT j.status,j.attempts,CAST(j.next_attempt_at AS TEXT),CAST(j.locked_until AS TEXT),j.attempt_token,CAST(j.sent_at AS TEXT),j.cycle FROM reminder_jobs j WHERE j.id=?`, jobID).Scan(&row.Status, &row.Attempts, &row.Next, &row.Locked, &row.Token, &row.Sent, &row.Cycle); err != nil {
			t.Fatal(err)
		}
		return row
	}
	beforeJob = readJob()
	assertTermsRevision := func(body string, wantStatus int, wantRevision float64) map[string]any {
		t.Helper()
		w := request(t, h, "PATCH", "/api/v1/periods/"+pid, body, 101, wantStatus)
		if wantStatus != 200 {
			return nil
		}
		period := data(t, w)
		got := period["terms"].(map[string]any)["revision"]
		if got != wantRevision {
			t.Fatalf("revision=%v want=%v", got, wantRevision)
		}
		return period
	}
	assertTermsRevision(`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}}`, 200, 1)
	request(t, h, "PATCH", "/api/v1/periods/"+pid, `{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"custom","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}}`, 101, 200)
	assertTermsRevision(`{"expected_cycle":1,"expected_terms_revision":2,"terms":{"mode":"custom","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}}`, 200, 2)
	request(t, h, "PATCH", "/api/v1/periods/"+pid, `{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}}`, 101, 409)
	jobAfterTerms := readJob()
	if !reflect.DeepEqual(beforeJob, jobAfterTerms) {
		t.Fatalf("terms patch changed reminder job: before=%+v after=%+v", beforeJob, jobAfterTerms)
	}
	if got := data(t, request(t, h, "GET", "/api/v1/periods/"+pid, "", 101, 200))["cycle"]; got != float64(1) {
		t.Fatalf("terms patch changed cycle: %v", got)
	}

	// A legacy row starts at normalized revision one; first customization
	// persists it at revision two. Explicit null resets to the default at +1.
	legacyID := legacy["id"].(string)
	customized := data(t, request(t, h, "PATCH", "/api/v1/periods/"+legacyID, `{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":null,"currency":null}}`, 101, 200))
	if got := customized["terms"].(map[string]any)["revision"]; got != float64(2) {
		t.Fatalf("first customization revision=%v", got)
	}
	reset := data(t, request(t, h, "PATCH", "/api/v1/periods/"+legacyID, `{"expected_cycle":1,"expected_terms_revision":2,"terms":null}`, 101, 200))
	if got := reset["terms"].(map[string]any); got["mode"] != "custom" || got["revision"] != float64(3) {
		t.Fatalf("reset terms=%v", got)
	}

	// Due-date changes advance cycle independently of terms revision; both
	// expected versions are enforced in a single transaction.
	changed := data(t, request(t, h, "PATCH", "/api/v1/periods/"+pid, `{"expected_cycle":1,"expected_terms_revision":2,"terms":{"mode":"recurring","interval_count":2,"interval_unit":"week","amount":0,"currency":"USD"},"expires_at":"2027-02-28"}`, 101, 200))
	if changed["cycle"] != float64(2) || changed["terms"].(map[string]any)["revision"] != float64(3) {
		t.Fatalf("due + terms change=%v", changed)
	}
	request(t, h, "PATCH", "/api/v1/periods/"+pid, `{"expected_cycle":1,"expected_terms_revision":3,"terms":{"mode":"custom","interval_count":null,"interval_unit":null,"amount":null,"currency":null}}`, 101, 409)
	request(t, h, "PATCH", "/api/v1/periods/"+pid, `{"expected_cycle":2,"expected_terms_revision":2,"terms":{"mode":"custom","interval_count":null,"interval_unit":null,"amount":null,"currency":null}}`, 101, 409)
	unchanged := data(t, request(t, h, "GET", "/api/v1/periods/"+pid, "", 101, 200))
	if unchanged["cycle"] != float64(2) || unchanged["terms"].(map[string]any)["revision"] != float64(3) {
		t.Fatalf("dual conflict partially applied: %v", unchanged)
	}

	renewalBody := `{"expected_cycle":2,"date_precision":"date","expires_at":"2027-03-31"}`
	first := data(t, request(t, h, "POST", "/api/v1/periods/"+pid+"/renewals", renewalBody, 101, 201))
	replay := data(t, request(t, h, "POST", "/api/v1/periods/"+pid+"/renewals", renewalBody, 101, 200))
	if first["id"] != replay["id"] || replay["amount"] != nil || replay["currency"] != nil {
		t.Fatalf("renewal replay=%v first=%v", replay, first)
	}
	renewed := data(t, request(t, h, "GET", "/api/v1/periods/"+pid, "", 101, 200))
	explicitBody := `{"expected_cycle":3,"date_precision":"date","expires_at":"2027-04-30","amount":0,"currency":"USD"}`
	explicit := data(t, request(t, h, "POST", "/api/v1/periods/"+pid+"/renewals", explicitBody, 101, 201))
	explicitReplay := data(t, request(t, h, "POST", "/api/v1/periods/"+pid+"/renewals", explicitBody, 101, 200))
	if explicit["id"] != explicitReplay["id"] || explicitReplay["amount"] != float64(0) || explicitReplay["currency"] != "USD" {
		t.Fatalf("explicit renewal replay=%v first=%v", explicitReplay, explicit)
	}
	afterExplicit := data(t, request(t, h, "GET", "/api/v1/periods/"+pid, "", 101, 200))
	if afterExplicit["terms"].(map[string]any)["revision"] != float64(3) {
		t.Fatalf("explicit renewal changed terms: %v", afterExplicit["terms"])
	}
	if renewed["terms"].(map[string]any)["revision"] != float64(3) {
		t.Fatalf("renewal implicitly changed terms: %v", renewed["terms"])
	}
}

func TestTermsStrictAPIValidation(t *testing.T) {
	h, _, _ := setupAPI(t)
	categoryIDs := termsCategoryIDs(t, h, 101)
	service := data(t, request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"strict","category_id":%q}`, categoryIDs["vps"]), 101, 201))
	sid := service["id"].(string)
	path := "/api/v1/services/" + sid + "/periods"
	base := `"period_type":"custom","name":"valid","date_precision":"date","expires_at":"2027-06-30"`
	valid := `{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}`
	for _, fragment := range []string{
		`"terms":` + valid + `,"unexpected":true`,
		`"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD","extra":1}`,
		`"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0}`,
		`"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":null}`,
		`"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":"0","currency":"USD"}`,
		`"terms":{"mode":null,"interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}`,
		`"terms":{"mode":"unknown","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}`,
		`"terms":{"mode":"recurring","interval_count":0,"interval_unit":"day","amount":0,"currency":"USD"}`,
		`"terms":{"mode":"recurring","interval_count":1,"interval_unit":"decade","amount":0,"currency":"USD"}`,
		`"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":-1,"currency":"USD"}`,
		`"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"usd"}`,
		`"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"US"}`,
		`"terms":{"mode":"once","interval_count":1,"interval_unit":"day","amount":0,"currency":"USD"}`,
		`"terms":{"interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}`,
		`"terms":{"mode":"once","interval_unit":null,"amount":0,"currency":"USD"}`,
		`"terms":{"mode":"once","interval_count":null,"amount":0,"currency":"USD"}`,
		`"terms":{"mode":"once","interval_count":null,"interval_unit":null,"currency":"USD"}`,
		`"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0}`,
	} {
		request(t, h, "POST", path, `{`+base+`,`+fragment+`}`, 101, 400)
	}
	for _, body := range []string{
		`{"expected_cycle":1,"terms":` + valid + `}`,
		`{"expected_cycle":1,"expected_terms_revision":1}`,
		`{"expected_cycle":1,"expected_terms_revision":0,"terms":null}`,
		`{"expected_cycle":1,"expected_terms_revision":-1,"terms":null}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_unit":null,"amount":0,"currency":"USD"}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"amount":0,"currency":"USD"}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"currency":"USD"}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD","extra":true}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD","revision":2}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":null,"interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":"1","interval_unit":null,"amount":0,"currency":"USD"}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":9223372036854775808,"currency":"USD"}}`,
		`{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"},"unknown":1}`,
	} {
		request(t, h, "PATCH", "/api/v1/periods/1", body, 101, 400)
	}

	request(t, h, "POST", path, `{`+base+`,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":9223372036854775808,"currency":"USD"}}`, 101, 400)
	p := data(t, request(t, h, "POST", path, `{`+base+`,"terms":`+valid+`}`, 101, 201))
	request(t, h, "PATCH", "/api/v1/periods/"+p["id"].(string), `{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":9223372036854775808,"currency":"USD"}}`, 101, 400)
}

func TestSIMDetailsStrictAPIValidation(t *testing.T) {
	h, _, _ := setupAPI(t)
	categoryIDs := termsCategoryIDs(t, h, 101)
	sim := data(t, request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"sim","category_id":%q,"sim_details":{"card_type":"esim"}}`, categoryIDs["sim"]), 101, 201))
	simID := sim["id"].(string)
	simPeriod := data(t, request(t, h, "POST", "/api/v1/services/"+simID+"/periods", `{"period_type":"number_validity","name":"sim term","date_precision":"date","expires_at":"2027-07-31","terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":0,"currency":"USD"}}`, 101, 201))
	if simPeriod["terms"].(map[string]any)["amount"] != float64(0) {
		t.Fatalf("SIM service terms=%v", simPeriod["terms"])
	}
	generic := data(t, request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"generic","category_id":%q}`, categoryIDs["vps"]), 101, 201))
	gid := generic["id"].(string)
	for _, body := range []string{
		`{"name":"bad","category_id":"` + categoryIDs["sim"] + `","sim_details":{"card_type":"esim","unknown":true}}`,
		`{"name":"bad","category_id":"` + categoryIDs["sim"] + `","sim_details":{}}`,
		`{"name":"bad","category_id":"` + categoryIDs["sim"] + `","sim_details":{"card_type":null}}`,
		`{"name":"bad","category_id":"` + categoryIDs["sim"] + `","sim_details":{"card_type":1}}`,
		`{"name":"bad","category_id":"` + categoryIDs["sim"] + `","sim_details":null}`,
		`{"name":"bad","category_id":"` + categoryIDs["sim"] + `","sim_details":{"card_type":"esim","phone_number":1}}`,
		`{"name":"bad","category_id":"` + categoryIDs["sim"] + `","unexpected":true}`,
		`{"name":"bad","category_id":` + categoryIDs["sim"] + `}`, // category ID must be a JSON string.
		`{"name":"bad","category_id":"not-a-number"}`,
	} {
		request(t, h, "POST", "/api/v1/services", body, 101, 400)
	}
	for _, pair := range []struct{ id, body string }{
		{simID, `{"sim_details":{"unknown":true}}`},
		{simID, `{"sim_details":{"card_type":null}}`},
		{simID, `{"sim_details":{"card_type":1}}`},
		{simID, `{"sim_details":null}`},
		{simID, `{"unknown":true}`},
		{simID, `{"sim_details":{"card_type":"esim"},"category_id":123}`},
		{simID, `{"sim_details":{"card_type":"esim"},"category_id":"not-a-number"}`},
		{gid, `{"sim_details":{"card_type":"esim"}}`},
	} {
		request(t, h, "PATCH", "/api/v1/services/"+pair.id, pair.body, 101, 400)
	}
	genericAfterNull := data(t, request(t, h, "PATCH", "/api/v1/services/"+gid, `{"sim_details":null}`, 101, 200))
	if !reflect.DeepEqual(genericAfterNull, generic) {
		t.Fatalf("generic null details changed service: before=%v after=%v", generic, genericAfterNull)
	}

	// A valid SIM detail mutation and a later category lookup failure in one
	// patch must roll back all preceding service and SIM detail changes.
	request(t, h, "PATCH", "/api/v1/services/"+simID, `{"name":"changed","sim_details":{"card_type":"physical_sim"},"category_id":"999999"}`, 101, 404)
	unchanged := data(t, request(t, h, "GET", "/api/v1/services/"+simID, "", 101, 200))
	if unchanged["name"] != "sim" || unchanged["sim_details"].(map[string]any)["card_type"] != "esim" {
		t.Fatalf("failed patch partially committed: %v", unchanged)
	}
}

func TestPeriodTermsRemainIndependentFromRenewalAndCycle(t *testing.T) {
	h, _, _ := setupAPI(t)
	vpsID := termsCategoryIDs(t, h, 101)["vps"]
	service := data(t, request(t, h, "POST", "/api/v1/services", fmt.Sprintf(`{"name":"vps","category_id":%q}`, vpsID), 101, 201))
	period := data(t, request(t, h, "POST", "/api/v1/services/"+service["id"].(string)+"/periods", `{"period_type":"custom","name":"date change","date_precision":"date","expires_at":"2027-06-30"}`, 101, 201))
	pid := period["id"].(string)
	request(t, h, "PATCH", "/api/v1/periods/"+pid, `{"expected_cycle":1,"expected_terms_revision":1,"terms":{"mode":"once","interval_count":null,"interval_unit":null,"amount":null,"currency":null}}`, 101, 200)
	request(t, h, "PATCH", "/api/v1/periods/"+pid, `{"expected_cycle":1,"expires_at":"2027-07-31"}`, 101, 200)
	updated := data(t, request(t, h, "GET", "/api/v1/periods/"+pid, "", 101, 200))
	if updated["cycle"] != float64(2) || updated["terms"].(map[string]any)["revision"] != float64(2) {
		t.Fatalf("date-only patch changed terms or missed cycle: %v", updated)
	}
}
