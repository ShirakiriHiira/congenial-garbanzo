package api

import "testing"

func TestPermanentDeleteEndpoints(t *testing.T) {
	h, st, _ := setupAPI(t)
	categories := resourceTestCategories(t, h, 101)
	service := data(t, request(t, h, "POST", "/api/v1/services", `{"name":"card","category_id":"`+categories["vps"]+`"}`, 101, 201))
	servicePath := "/api/v1/services/" + service["id"].(string)
	period := data(t, request(t, h, "POST", servicePath+"/periods", `{"date_precision":"date","expires_at":"2027-02-01"}`, 101, 201))
	periodPath := "/api/v1/periods/" + period["id"].(string)
	rule := data(t, request(t, h, "POST", periodPath+"/rules", `{"days_before":1,"reminder_time":"09:00"}`, 101, 201))
	rulePath := "/api/v1/rules/" + rule["id"].(string)

	// Rule deletion uses expected_revision in the query string.
	request(t, h, "DELETE", rulePath+"?expected_revision=1", "", 202, 404)
	request(t, h, "DELETE", rulePath, "", 101, 400)
	request(t, h, "DELETE", rulePath+"?expected_revision=0", "", 101, 400)
	request(t, h, "DELETE", rulePath+"?expected_revision=2", "", 101, 409)
	request(t, h, "DELETE", rulePath+"?expected_revision=1", "", 101, 204)
	request(t, h, "GET", rulePath, "", 101, 404)
	request(t, h, "GET", periodPath, "", 101, 200)
	rule = data(t, request(t, h, "POST", periodPath+"/rules", `{"days_before":1,"reminder_time":"09:00"}`, 101, 201))
	rulePath = "/api/v1/rules/" + rule["id"].(string)

	// Period deletion uses expected_cycle and cascades rules/jobs/history.
	request(t, h, "DELETE", periodPath+"?expected_cycle=1", "", 202, 404)
	for _, query := range []string{"", "?expected_cycle=0", "?expected_cycle=1&expected_cycle=1", "?expected_cycle=1&unknown=1"} {
		request(t, h, "DELETE", periodPath+query, "", 101, 400)
	}
	request(t, h, "POST", periodPath+"/renewals", `{"expected_cycle":1,"date_precision":"date","expires_at":"2027-03-01"}`, 101, 201)
	request(t, h, "DELETE", periodPath+"?expected_cycle=1", "", 101, 409)
	request(t, h, "DELETE", periodPath+"?expected_cycle=2", "", 101, 204)
	request(t, h, "GET", periodPath, "", 101, 404)
	request(t, h, "GET", rulePath, "", 101, 404)
	request(t, h, "DELETE", periodPath+"?expected_cycle=2", "", 101, 404)
	request(t, h, "GET", servicePath, "", 101, 200)

	// Archive is a PATCH status transition and keeps child rows.
	period = data(t, request(t, h, "POST", servicePath+"/periods", `{"name":"child","date_precision":"date","expires_at":"2027-02-01"}`, 101, 201))
	periodPath = "/api/v1/periods/" + period["id"].(string)
	archived := data(t, request(t, h, "PATCH", servicePath, `{"status":"archived"}`, 101, 200))
	if archived["status"] != "archived" {
		t.Fatalf("archive status=%v", archived)
	}
	request(t, h, "GET", periodPath, "", 101, 200)
	restored := data(t, request(t, h, "PATCH", servicePath, `{"status":"active"}`, 101, 200))
	if restored["status"] != "active" {
		t.Fatalf("restore status=%v", restored)
	}

	// Permanent service deletion removes every child and is not retryable.
	request(t, h, "DELETE", servicePath, "", 0, 401)
	request(t, h, "DELETE", servicePath, "", 202, 404)
	request(t, h, "DELETE", servicePath, "", 101, 204)
	request(t, h, "GET", servicePath, "", 101, 404)
	request(t, h, "GET", periodPath, "", 101, 404)
	request(t, h, "DELETE", servicePath, "", 101, 404)
	var count int
	if err := st.DB.QueryRow("SELECT (SELECT count(*) FROM service_periods) + (SELECT count(*) FROM period_terms) + (SELECT count(*) FROM reminder_rules) + (SELECT count(*) FROM renewals) + (SELECT count(*) FROM reminder_jobs)").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade count=%d err=%v", count, err)
	}

	// Removed legacy delete paths are not routed.
	request(t, h, "POST", servicePath+"/delete", "", 101, 404)
	request(t, h, "POST", periodPath+"/delete", "", 101, 404)
	request(t, h, "POST", rulePath+"/delete", "", 101, 404)
}

func TestOptionalPeriodTitleAPI(t *testing.T) {
	h, _, _ := setupAPI(t)
	categories := resourceTestCategories(t, h, 101)
	service := data(t, request(t, h, "POST", "/api/v1/services", `{"name":"card","category_id":"`+categories["vps"]+`"}`, 101, 201))
	servicePath := "/api/v1/services/" + service["id"].(string)
	for _, field := range []string{"", `,"name":""`, `,"name":"  "`} {
		p := data(t, request(t, h, "POST", servicePath+"/periods", `{"date_precision":"date","expires_at":"2027-02-01"`+field+`}`, 101, 201))
		if p["name"] != "" {
			t.Fatalf("stored fallback instead of optional title: %v", p)
		}
		path := "/api/v1/periods/" + p["id"].(string)
		request(t, h, "PATCH", path, `{"expected_cycle":1,"name":"title"}`, 101, 200)
		cleared := data(t, request(t, h, "PATCH", path, `{"expected_cycle":1,"name":" "}`, 101, 200))
		if cleared["name"] != "" {
			t.Fatalf("clear title: %v", cleared)
		}
	}
}
