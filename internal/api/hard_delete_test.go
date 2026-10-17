package api

import "testing"

func TestHardDeleteEndpoints(t *testing.T) {
	h, _, _ := setupAPI(t)
	sim := data(t, request(t, h, "POST", "/api/v1/sims", `{"name":"card","card_type":"esim"}`, 101, 201))
	simPath := "/api/v1/sims/" + sim["id"].(string)
	period := data(t, request(t, h, "POST", simPath+"/periods", `{"period_type":"custom","date_precision":"date","expires_at":"2027-02-01"}`, 101, 201))
	periodPath := "/api/v1/periods/" + period["id"].(string)
	rule := data(t, request(t, h, "POST", periodPath+"/rules", `{"days_before":1,"reminder_time":"09:00"}`, 101, 201))
	rulePath := "/api/v1/rules/" + rule["id"].(string)
	request(t, h, "POST", simPath+"/delete", "", 0, 401)
	request(t, h, "POST", simPath+"/delete", "", 202, 404)
	request(t, h, "POST", periodPath+"/delete?expected_cycle=1", "", 202, 404)
	for _, query := range []string{"", "?expected_cycle=0", "?expected_cycle=1&expected_cycle=1", "?expected_cycle=1&unknown=1"} {
		request(t, h, "POST", periodPath+"/delete"+query, "", 101, 400)
	}
	request(t, h, "POST", periodPath+"/renewals", `{"expected_cycle":1,"date_precision":"date","expires_at":"2027-03-01"}`, 101, 201)
	request(t, h, "POST", periodPath+"/delete?expected_cycle=1", "", 101, 409)
	request(t, h, "GET", periodPath, "", 101, 200)
	// Legacy DELETE still cancels, and a cancelled period can be hard-deleted.
	request(t, h, "DELETE", periodPath+"?expected_cycle=2", "", 101, 204)
	cancelled := data(t, request(t, h, "GET", periodPath, "", 101, 200))
	if cancelled["status"] != "cancelled" {
		t.Fatalf("legacy period deletion no longer cancels: %v", cancelled)
	}
	request(t, h, "POST", periodPath+"/delete?expected_cycle=2", "", 101, 204)
	request(t, h, "GET", periodPath, "", 101, 404)
	request(t, h, "GET", rulePath, "", 101, 404)
	request(t, h, "POST", periodPath+"/delete?expected_cycle=2", "", 101, 404)
	request(t, h, "GET", simPath, "", 101, 200)
	// Legacy card archive preserves its new period until explicit hard deletion.
	period = data(t, request(t, h, "POST", simPath+"/periods", `{"period_type":"custom","name":"child","date_precision":"date","expires_at":"2027-02-01"}`, 101, 201))
	periodPath = "/api/v1/periods/" + period["id"].(string)
	request(t, h, "DELETE", simPath, "", 101, 204)
	archived := data(t, request(t, h, "GET", simPath, "", 101, 200))
	if archived["status"] != "archived" {
		t.Fatalf("legacy card deletion no longer archives: %v", archived)
	}
	request(t, h, "GET", periodPath, "", 101, 200)
	request(t, h, "POST", simPath+"/delete?unknown=1", "", 101, 400)
	request(t, h, "GET", simPath+"/delete", "", 101, 405)
	request(t, h, "POST", simPath+"/delete", "", 101, 204)
	request(t, h, "GET", simPath, "", 101, 404)
	request(t, h, "GET", periodPath, "", 101, 404)
	request(t, h, "POST", simPath+"/delete", "", 101, 404)
}

func TestOptionalPeriodTitleAPI(t *testing.T) {
	h, _, _ := setupAPI(t)
	sim := data(t, request(t, h, "POST", "/api/v1/sims", `{"name":"card","card_type":"esim"}`, 101, 201))
	simPath := "/api/v1/sims/" + sim["id"].(string)
	for _, field := range []string{"", `,"name":""`, `,"name":"  "`} {
		p := data(t, request(t, h, "POST", simPath+"/periods", `{"period_type":"custom","date_precision":"date","expires_at":"2027-02-01"`+field+`}`, 101, 201))
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
