package storage

import (
	"context"
	"testing"
)

func insertCleanGraph(t *testing.T, s *Store) {
	t.Helper()
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,private_chat_id,created_at,updated_at) VALUES(7,101,'Asia/Taipei',101,1,2),(8,102,'UTC',NULL,1,2)`,
		`INSERT INTO service_items(id,user_id,name,category_id,provider,identifier,management_url,notes,status,created_at,updated_at) VALUES(11,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'carrier','host','https://example.invalid','notes','active',1,2)`,
		`INSERT INTO service_periods(id,service_id,name,starts_at,expires_at,date_precision,local_expires_date,timezone,cycle,created_at,updated_at) VALUES(21,11,'main',100,200,'date','1970-01-01','Asia/Taipei',3,1,2),(22,11,'',NULL,300,'datetime',NULL,'Asia/Taipei',1,1,2)`,
		`INSERT INTO period_terms(period_id,mode,interval_count,interval_unit,amount,currency,revision) VALUES(21,'recurring',1,'month',1200,'TWD',2),(22,'custom',NULL,NULL,NULL,NULL,1)`,
		`INSERT INTO reminder_rules(id,period_id,days_before,reminder_time,enabled,revision,created_at,updated_at) VALUES(31,21,1,'09:00',1,2,1,2),(32,22,0,'10:00',0,1,1,2)`,
		`INSERT INTO renewals(id,period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,previous_local_expires_date,new_local_expires_date,timezone,renewed_at,amount,currency,notes,created_at) VALUES(41,21,1,100,200,'date','date','1970-01-01','1970-01-02','Asia/Taipei',1,1200,'TWD','renewed',1)`,
		`INSERT INTO reminder_jobs(id,period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,locked_until,attempt_token,sent_at,telegram_message_id,last_error_code,created_at,updated_at) VALUES(51,21,31,3,2,150,200,'sending',1,150,210,'token',NULL,NULL,'timeout',1,2),(52,22,32,1,1,150,300,'sent',1,NULL,NULL,NULL,200,987,NULL,1,2)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,service_id,period_id,rule_id,expected_cycle,expected_terms_revision,expected_revision,expires_at,created_at) VALUES(7,101,'period_edit','edit_field','{"v":{"field":"name"}}',11,21,31,3,2,2,999,1)`,
	)
}

func TestServiceDeleteCascadesAndRetiresIDs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	insertCleanGraph(t, s)
	sequences := map[string]int64{}
	for _, table := range []string{"service_items", "service_periods", "reminder_rules", "renewals", "reminder_jobs"} {
		var seq int64
		if err := s.DB.QueryRow("SELECT seq FROM sqlite_sequence WHERE name=?", table).Scan(&seq); err != nil {
			t.Fatal(err)
		}
		sequences[table] = seq
	}
	if err := func() error {
		_, err := s.DB.ExecContext(ctx, "DELETE FROM service_items WHERE id=11")
		return err
	}(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sim_details", "service_periods", "period_terms", "reminder_rules", "renewals", "reminder_jobs", "bot_dialogs"} {
		var count int
		if err := s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("cascade %s: count=%d err=%v", table, count, err)
		}
	}
	assertStorageIntegrity(t, s)
	// AUTOINCREMENT high-water marks survive the cascade.
	for table, want := range sequences {
		var seq int64
		if err := s.DB.QueryRow("SELECT seq FROM sqlite_sequence WHERE name=?", table).Scan(&seq); err != nil || seq != want {
			t.Fatalf("%s high-water=%d want=%d err=%v", table, seq, want, err)
		}
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO service_items(user_id,name,category_id,status,created_at,updated_at) VALUES(7,'next',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := result.LastInsertId()
	if err != nil || newID <= 11 {
		t.Fatalf("service ID reused: id=%d err=%v", newID, err)
	}
	for _, tc := range []struct {
		name      string
		statement string
		column    string
		id        int
		empty     []string
	}{
		{"period", "DELETE FROM service_periods WHERE id=21", "period_id", 21, []string{"period_terms", "reminder_rules", "renewals", "reminder_jobs"}},
		{"rule", "DELETE FROM reminder_rules WHERE id=31", "reminder_rule_id", 31, []string{"reminder_jobs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			insertCleanGraph(t, s)
			storageExec(t, s, tc.statement)
			for _, table := range tc.empty {
				var count int
				if err := s.DB.QueryRow("SELECT count(*) FROM "+table+" WHERE "+tc.column+"=?", tc.id).Scan(&count); err != nil || count != 0 {
					t.Fatalf("%s: count=%d err=%v", table, count, err)
				}
			}
			assertStorageIntegrity(t, s)
		})
	}
}

func TestReminderJobCompositeForeignKey(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	insertCleanGraph(t, s)
	for _, statement := range []string{
		`INSERT INTO reminder_jobs(period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,created_at,updated_at) VALUES(21,999,1,1,1,200,'pending',1,1)`,
		`UPDATE reminder_jobs SET reminder_rule_id=32 WHERE id=51`,
	} {
		if _, err := s.DB.ExecContext(ctx, statement); err == nil {
			t.Fatalf("accepted mismatched rule/period: %s", statement)
		}
	}
	assertStorageIntegrity(t, s)
}
