package storage

import (
	"context"
	"fmt"
	"testing"
)

// seedDialogGraph creates the post-003 graph without dialogs.
func seedDialogGraph(t *testing.T, s *Store) {
	t.Helper()
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,private_chat_id,created_at,updated_at) VALUES(7,101,'Asia/Taipei',101,10,20),(8,102,'UTC',NULL,11,21)`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(11,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`,
		`INSERT INTO validity_periods(id,service_id,period_type,name,expires_at,date_precision,timezone,status,cycle,created_at,updated_at) VALUES(21,11,'custom','p',200,'datetime','Asia/Taipei','active',3,1,1)`,
		`INSERT INTO reminder_rules(id,validity_period_id,days_before,reminder_time,enabled,revision,created_at,updated_at) VALUES(31,21,1,'09:00',1,2,1,1)`,
	)
}

func TestDialogMigrationPreservesRenewalRows(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.migrate(ctx, storageMigrationSource(t, 1, 2, 3)); err != nil {
		t.Fatal(err)
	}
	seedDialogGraph(t, s)
	storageExec(t, s, `INSERT INTO bot_dialogs(user_id,chat_id,action,validity_period_id,expected_cycle,expires_at,created_at) VALUES(7,101,'renew',21,3,999,18)`)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var action, step, draft string
	var cycle, createdAt, expiresAt int64
	if err := s.DB.QueryRow(`SELECT action,step,draft_json,expected_cycle,created_at,expires_at FROM bot_dialogs WHERE user_id=7`).
		Scan(&action, &step, &draft, &cycle, &createdAt, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if action != "renew" || step != "await_expiration" || draft != "{}" || cycle != 3 || createdAt != 18 || expiresAt != 999 {
		t.Fatalf("mapped row action=%q step=%q draft=%q cycle=%d created=%d expires=%d", action, step, draft, cycle, createdAt, expiresAt)
	}
	assertStorageIntegrity(t, s)
}

func TestDialogMigrationRejectsUnknownAction(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	// The shipped 001 constraint only permits renew, so a permissive copy of
	// the historical table is used to prove the mapping CASE aborts (rather
	// than dropping or rewriting) an action the new CHECK cannot represent.
	if err := s.migrate(ctx, storageMigrationSource(t, 1, 2, 3)); err != nil {
		t.Fatal(err)
	}
	seedDialogGraph(t, s)
	storageExec(t, s,
		`DROP TABLE bot_dialogs`,
		`CREATE TABLE bot_dialogs (
			user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
			chat_id INTEGER NOT NULL,
			action TEXT NOT NULL,
			validity_period_id INTEGER NOT NULL,
			expected_cycle INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,validity_period_id,expected_cycle,expires_at,created_at) VALUES(7,101,'other',21,1,999,1)`,
	)
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("accepted unmappable dialog action")
	}
	var count int
	if err := s.DB.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=4`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed 004 recorded: count=%d err=%v", count, err)
	}
	if err := s.DB.QueryRow(`SELECT count(*) FROM pragma_table_info('bot_dialogs') WHERE name='step'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial 004 schema survived: count=%d err=%v", count, err)
	}
	if err := s.DB.QueryRow(`SELECT count(*) FROM bot_dialogs WHERE action='other'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed migration lost the row: count=%d err=%v", count, err)
	}
}

func TestDialogCascadeAndConstraints(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedDialogGraph(t, s)
	valid := []string{
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,service_id,expires_at,created_at) VALUES(7,101,'service_add','add_name','{"v":{"name":"x"}}',11,999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,validity_period_id,expected_cycle,expected_terms_revision,expires_at,created_at) VALUES(7,101,'terms_edit','terms_mode','{}',21,3,2,999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,rule_id,expected_revision,expires_at,created_at) VALUES(8,102,'rule_edit','rule_days','{}',31,2,999,1)`,
	}
	for _, statement := range valid {
		storageExec(t, s, `DELETE FROM bot_dialogs`, statement)
	}
	invalid := []string{
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,expires_at,created_at) VALUES(7,101,'unknown','add_name','{}',999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,expires_at,created_at) VALUES(7,101,'renew',' padded ','{}',999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,expires_at,created_at) VALUES(9,103,'renew','ok','{}',999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,service_id,expires_at,created_at) VALUES(7,101,'service_add','add_name','{}',999,999,1)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,validity_period_id,expected_cycle,expires_at,created_at) VALUES(7,101,'renew','ok','{}',21,0,999,1)`,
	}
	for _, statement := range invalid {
		if _, err := s.DB.Exec(statement); err == nil {
			t.Fatalf("accepted invalid dialog: %s", statement)
		}
	}
	storageExec(t, s, `DELETE FROM bot_dialogs`)
	// One oversized draft is rejected by the column bound; JSON validity is a
	// Go-side precondition, so the oversized body covers the SQL guard.
	large := fmt.Sprintf(`{"v":{"notes":"%s"}}`, make([]byte, 16384))
	if _, err := s.DB.Exec(`INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,expires_at,created_at) VALUES(7,101,'timezone','tz_value',?,999,1)`, large); err == nil {
		t.Fatal("accepted oversized draft")
	}

	// Deleting a service, period, or rule removes referencing dialogs; each
	// case gets a fresh graph because the first delete cascades the rest.
	cases := []struct {
		statement string
		insert    string
	}{
		{`DELETE FROM service_items WHERE id=11`, `INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,service_id,expires_at,created_at) VALUES(7,101,'service_edit','edit_field','{}',11,999,1)`},
		{`DELETE FROM validity_periods WHERE id=21`, `INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,validity_period_id,expires_at,created_at) VALUES(7,101,'renew','ok','{}',21,999,1)`},
		{`DELETE FROM reminder_rules WHERE id=31`, `INSERT INTO bot_dialogs(user_id,chat_id,action,step,draft_json,rule_id,expires_at,created_at) VALUES(7,101,'rule_edit','rule_days','{}',31,999,1)`},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("cascade%d", i), func(t *testing.T) {
			fresh := openTestStore(t)
			if err := fresh.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			seedDialogGraph(t, fresh)
			storageExec(t, fresh, tc.insert, tc.statement)
			var count int
			if err := fresh.DB.QueryRow("SELECT count(*) FROM bot_dialogs").Scan(&count); err != nil || count != 0 {
				t.Fatalf("%s left dialog: count=%d err=%v", tc.statement, count, err)
			}
		})
	}
}
