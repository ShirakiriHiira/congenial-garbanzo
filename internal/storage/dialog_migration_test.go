package storage

import (
	"context"
	"fmt"
	"testing"
)

func TestDialogConstraintsAndCascade(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,private_chat_id,created_at,updated_at) VALUES(7,101,'Asia/Taipei',101,10,20),(8,102,'UTC',NULL,11,21)`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(11,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`,
		`INSERT INTO service_periods(id,service_id,name,expires_at,date_precision,timezone,cycle,created_at,updated_at) VALUES(21,11,'p',200,'datetime','Asia/Taipei',3,1,1)`,
		`INSERT INTO period_terms(period_id,mode,revision) VALUES(21,'custom',2)`,
		`INSERT INTO reminder_rules(id,period_id,days_before,reminder_time,enabled,revision,created_at,updated_at) VALUES(31,21,1,'09:00',1,2,1,1)`,
	)
	valid := []string{
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,service_id,expires_at,created_at) VALUES(7,101,'service_add','add_name','{"v":{"name":"x"}}',11,999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,service_id,period_id,expected_cycle,expected_terms_revision,expires_at,created_at) VALUES(7,101,'period_edit','edit_field','{}',11,21,3,2,999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,rule_id,expected_revision,expires_at,created_at) VALUES(8,102,'rule_edit','rule_days','{}',31,2,999,1)`,
	}
	for _, statement := range valid {
		storageExec(t, s, `DELETE FROM bot_dialogs`, statement)
	}
	invalid := []string{
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,expires_at,created_at) VALUES(7,101,'unknown','add_name','{}',999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,expires_at,created_at) VALUES(7,101,'terms_edit','terms_mode','{}',999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,expires_at,created_at) VALUES(7,101,'renew',' padded ','{}',999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,expires_at,created_at) VALUES(9,103,'renew','ok','{}',999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,service_id,expires_at,created_at) VALUES(7,101,'service_add','add_name','{}',999,999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,period_id,expected_cycle,expires_at,created_at) VALUES(7,101,'renew','ok','{}',21,0,999,1)`,
	}
	for _, statement := range invalid {
		if _, err := s.DB.Exec(statement); err == nil {
			t.Fatalf("accepted invalid dialog: %s", statement)
		}
	}
	storageExec(t, s, `DELETE FROM bot_dialogs`)
	large := fmt.Sprintf(`{"v":{"notes":"%s"}}`, make([]byte, 16384))
	if _, err := s.DB.Exec(`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,expires_at,created_at) VALUES(7,101,'timezone','tz_value',?,999,1)`, large); err == nil {
		t.Fatal("accepted oversized draft")
	}

	cases := []struct {
		statement string
		insert    string
	}{
		{`DELETE FROM service_items WHERE id=11`, `INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,service_id,expires_at,created_at) VALUES(7,101,'service_edit','edit_field','{}',11,999,1)`},
		{`DELETE FROM service_periods WHERE id=21`, `INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,period_id,expires_at,created_at) VALUES(7,101,'renew','ok','{}',21,999,1)`},
		{`DELETE FROM reminder_rules WHERE id=31`, `INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,rule_id,expires_at,created_at) VALUES(7,101,'rule_edit','rule_days','{}',31,999,1)`},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("cascade%d", i), func(t *testing.T) {
			fresh := openTestStore(t)
			if err := fresh.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			storageExec(t, fresh,
				`INSERT INTO users(id,telegram_user_id,timezone,private_chat_id,created_at,updated_at) VALUES(7,101,'Asia/Taipei',101,10,20)`,
				`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(11,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`,
				`INSERT INTO service_periods(id,service_id,name,expires_at,date_precision,timezone,cycle,created_at,updated_at) VALUES(21,11,'p',200,'datetime','Asia/Taipei',3,1,1)`,
				`INSERT INTO period_terms(period_id,mode,revision) VALUES(21,'custom',2)`,
				`INSERT INTO reminder_rules(id,period_id,days_before,reminder_time,enabled,revision,created_at,updated_at) VALUES(31,21,1,'09:00',1,2,1,1)`,
				tc.insert, tc.statement,
			)
			var count int
			if err := fresh.DB.QueryRow("SELECT count(*) FROM bot_dialogs").Scan(&count); err != nil || count != 0 {
				t.Fatalf("%s left dialog: count=%d err=%v", tc.statement, count, err)
			}
			assertStorageIntegrity(t, fresh)
		})
	}
}
