package storage

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"telegram.bot.remind/migrations"
)

func TestHardDeleteMigrationPreservesAndCascades(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	initial, err := fs.ReadFile(migrations.FS, "001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx, fstest.MapFS{"001_initial.sql": {Data: initial}}); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO users(id,telegram_user_id,timezone,private_chat_id,created_at,updated_at) VALUES(7,101,'Asia/Taipei',101,1,2)`,
		`INSERT INTO sims(id,user_id,name,card_type,operator,phone_number,country_or_region,iccid,notes,status,created_at,updated_at) VALUES(11,7,'card','esim','carrier','123','TW','iccid','notes','active',1,2)`,
		`INSERT INTO validity_periods(id,sim_id,period_type,name,starts_at,expires_at,date_precision,local_expires_date,timezone,status,cycle,created_at,updated_at) VALUES(13,11,'custom','',100,200,'date','1970-01-01','Asia/Taipei','active',2,1,2)`,
		`INSERT INTO reminder_rules(id,validity_period_id,days_before,reminder_time,enabled,revision,created_at,updated_at) VALUES(17,13,0,'09:00',1,2,1,2)`,
		`INSERT INTO renewals(id,validity_period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,previous_local_expires_date,new_local_expires_date,timezone,renewed_at,amount,currency,notes,created_at) VALUES(19,13,1,100,200,'date','date','1970-01-01','1970-01-02','Asia/Taipei',1,1200,'TWD','renewed',1)`,
		`INSERT INTO reminder_jobs(id,validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,locked_until,attempt_token,sent_at,telegram_message_id,last_error_code,created_at,updated_at) VALUES(23,13,17,2,2,150,200,'sending',1,150,210,'token',NULL,NULL,'timeout',1,2)`,
		`INSERT INTO bot_dialogs(user_id,chat_id,action,validity_period_id,expected_cycle,expires_at,created_at) VALUES(7,101,'renew',13,2,300,1)`,
	}
	for _, statement := range statements {
		if _, err := s.DB.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	// Compare every column after upgrade, including nullable reminder lease data.
	tables := []string{"users", "sims", "validity_periods", "reminder_rules", "renewals", "reminder_jobs", "bot_dialogs"}
	for _, table := range tables {
		if _, err := s.DB.ExecContext(ctx, "CREATE TEMP TABLE before_"+table+" AS SELECT * FROM "+table); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.migrate(ctx, storageMigrationSource(t, 1, 2)); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx, storageMigrationSource(t, 1, 2)); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		var count int
		query := "SELECT (SELECT count(*) FROM (SELECT * FROM " + table + " EXCEPT SELECT * FROM before_" + table + ")) + (SELECT count(*) FROM (SELECT * FROM before_" + table + " EXCEPT SELECT * FROM " + table + "))"
		if err := s.DB.QueryRowContext(ctx, query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s changed: differences=%d err=%v", table, count, err)
		}
	}
	assertPragmas(t, s)
	for _, statement := range []string{
		`INSERT INTO validity_periods(sim_id,period_type,name,expires_at,date_precision,timezone,status,created_at,updated_at) VALUES(999,'custom','',300,'datetime','UTC','active',1,1)`,
		`INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,created_at,updated_at) VALUES(13,999,2,1,1,200,'pending',1,1)`,
		`UPDATE reminder_jobs SET validity_period_id=999 WHERE id=23`,
	} {
		if _, err := s.DB.ExecContext(ctx, statement); err == nil {
			t.Fatalf("upgrade lost foreign key: %s", statement)
		}
	}
	rows, err := s.DB.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("foreign key check found an orphan")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM sims WHERE id=11"); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables[1:] {
		var count int
		if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("cascade %s: count=%d err=%v", table, count, err)
		}
	}
	// Explicit imported IDs seed AUTOINCREMENT, even after the entire graph goes.
	for _, table := range []string{"sims", "validity_periods", "reminder_rules", "renewals", "reminder_jobs"} {
		var sequence int64
		if err := s.DB.QueryRowContext(ctx, "SELECT seq FROM sqlite_sequence WHERE name=?", table).Scan(&sequence); err != nil || sequence == 0 {
			t.Fatalf("retired identity %s: seq=%d err=%v", table, sequence, err)
		}
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO sims(user_id,name,card_type,status,created_at,updated_at) VALUES(7,'next','esim','active',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 11 {
		t.Fatalf("reused migrated card ID: id=%d err=%v", id, err)
	}
}
