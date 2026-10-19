package storage

import (
	"context"
	"strings"
	"testing"
)

func TestCleanBaselineInitializesOnlyCurrentTables(t *testing.T) {
	s := openTestStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"users", "service_categories", "service_items", "sim_details",
		"service_periods", "period_terms", "reminder_rules", "renewals",
		"reminder_jobs", "bot_dialogs",
	} {
		var count int
		if err := s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s missing: count=%d err=%v", table, count, err)
		}
	}
	for _, table := range []string{"sims", "validity_periods"} {
		var count int
		if err := s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("legacy table %s survived: count=%d err=%v", table, count, err)
		}
	}
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM service_categories WHERE user_id IS NULL").Scan(&count); err != nil || count != 5 {
		t.Fatalf("built-in categories=%d err=%v", count, err)
	}
	assertStorageIntegrity(t, s)
}

// A legacy database does not match the clean baseline checksum. Migration must
// fail without modifying the source schema.
func TestLegacySchemaIsRejectedWithoutMutation(t *testing.T) {
	s := openTestStore(t)
	storageExec(t, s,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, telegram_user_id INTEGER NOT NULL UNIQUE, timezone TEXT NOT NULL, private_chat_id INTEGER, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE sims (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, name TEXT NOT NULL, card_type TEXT NOT NULL, status TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE validity_periods (id INTEGER PRIMARY KEY, sim_id INTEGER NOT NULL, period_type TEXT NOT NULL, name TEXT NOT NULL, expires_at INTEGER NOT NULL, date_precision TEXT NOT NULL, timezone TEXT NOT NULL, status TEXT NOT NULL, cycle INTEGER NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at INTEGER NOT NULL)`,
		`INSERT INTO schema_migrations(version,checksum,applied_at) VALUES(1,'legacy-baseline',1)`,
		`INSERT INTO users(id,telegram_user_id,timezone,created_at,updated_at) VALUES(1,101,'UTC',1,1)`,
	)
	if err := s.Migrate(context.Background()); err == nil {
		t.Fatal("legacy schema was accepted")
	}
	for _, table := range []string{"users", "sims", "validity_periods"} {
		var count int
		if err := s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name=?", table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("legacy %s was modified: count=%d err=%v", table, count, err)
		}
	}
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='service_periods'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("clean schema partially applied: count=%d err=%v", count, err)
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("ledger changed: count=%d err=%v", count, err)
	}
}

func TestServiceCategoryConstraints(t *testing.T) {
	s := openTestStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,created_at,updated_at) VALUES(7,101,'UTC',1,1),(8,102,'UTC',1,1)`,
		`INSERT INTO service_categories(id,user_id,name,kind,created_at,updated_at) VALUES(11,7,'Private','generic',1,1),(12,8,'Private','generic',1,1)`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(1,7,'rowid parent',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(21,7,'SIM',(SELECT id FROM service_categories WHERE code='sim'),'active',1,1),(22,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1),(23,7,'Private',11,'active',1,1)`,
		`INSERT INTO sim_details(service_id,card_type) VALUES(21,'esim')`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(24,7,'owner',(SELECT id FROM service_categories WHERE code='sim'),'active',1,1)`,
	)
	invalid := []string{
		`INSERT INTO service_categories(user_id,name,kind,created_at,updated_at) VALUES(7,'Private','generic',1,1)`,
		`INSERT INTO service_categories(user_id,name,kind,created_at,updated_at) VALUES(999,'Orphan','generic',1,1)`,
		`INSERT INTO service_categories(user_id,name,kind,created_at,updated_at) VALUES(7,'Bad','sim',1,1)`,
		`INSERT INTO service_categories(name,kind,created_at,updated_at) VALUES('Global','generic',1,1)`,
		`INSERT INTO service_categories(code,name,kind,created_at,updated_at) VALUES('invented','Global','generic',1,1)`,
		`UPDATE service_categories SET name='Renamed' WHERE code='sim'`,
		`UPDATE service_categories SET updated_at=2 WHERE code='vps'`,
		`DELETE FROM service_categories WHERE code='sim'`,
		`UPDATE service_categories SET user_id=8 WHERE id=11`,
		`UPDATE service_categories SET code='other' WHERE id=11`,
		`UPDATE service_categories SET kind='sim' WHERE id=11`,
		`UPDATE service_categories SET id=99 WHERE id=11`,
		`UPDATE service_categories SET created_at=2 WHERE id=11`,
		`UPDATE service_categories SET name='' WHERE id=11`,
		`UPDATE service_categories SET name=' padded ' WHERE id=11`,
		`UPDATE service_categories SET name='` + strings.Repeat("字", 201) + `' WHERE id=11`,
		`INSERT INTO service_items(user_id,name,status,created_at,updated_at) VALUES(7,'missing','active',1,1)`,
		`INSERT INTO service_items(user_id,name,category_id,status,created_at,updated_at) VALUES(7,'foreign',12,'active',1,1)`,
		`INSERT INTO service_items(user_id,name,category_id,status,created_at,updated_at) VALUES(7,'orphan',999,'active',1,1)`,
		`UPDATE service_items SET category_id=NULL WHERE id=21`,
		`UPDATE service_items SET category_id=12 WHERE id=22`,
		`UPDATE service_items SET category_id=(SELECT id FROM service_categories WHERE code='vps') WHERE id=21`,
		`UPDATE service_items SET category_id=(SELECT id FROM service_categories WHERE code='sim') WHERE id=22`,
		`UPDATE service_items SET user_id=8 WHERE id=23`,
		`DELETE FROM service_categories WHERE id=11`,
		`INSERT INTO sim_details(service_id,card_type) VALUES(NULL,'esim')`,
		`INSERT INTO sim_details(service_id,card_type) VALUES(999,'esim')`,
		`INSERT INTO sim_details(service_id,card_type) VALUES(22,NULL)`,
		`UPDATE sim_details SET card_type='bad' WHERE service_id=21`,
		`UPDATE service_items SET category_id=11,user_id=8 WHERE id=24`,
	}
	for _, statement := range invalid {
		if _, err := s.DB.Exec(statement); err == nil {
			t.Fatalf("accepted invalid category mutation: %s", statement)
		}
	}
	storageExec(t, s,
		`UPDATE service_categories SET name='Renamed',updated_at=2 WHERE id=11`,
		`UPDATE service_items SET category_id=11 WHERE id=22`,
		`UPDATE service_items SET category_id=(SELECT id FROM service_categories WHERE code='domain') WHERE id=22`,
		`UPDATE service_items SET category_id=(SELECT id FROM service_categories WHERE code='sim') WHERE id=21`,
	)
	assertStorageIntegrity(t, s)
}

func TestPeriodTermsConstraints(t *testing.T) {
	s := openTestStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,created_at,updated_at) VALUES(7,101,'UTC',1,1)`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(11,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`,
		`INSERT INTO service_periods(id,service_id,name,expires_at,date_precision,timezone,created_at,updated_at) VALUES(21,11,'',200,'datetime','UTC',1,1)`,
	)
	invalid := []string{
		`NULL,'custom',NULL,NULL,NULL,NULL,1`,
		`999,'custom',NULL,NULL,NULL,NULL,1`,
		`21,NULL,NULL,NULL,NULL,NULL,1`,
		`21,'bad',NULL,NULL,NULL,NULL,1`,
		`21,'custom',NULL,NULL,NULL,NULL,NULL`,
		`21,'custom',NULL,NULL,NULL,NULL,0`,
		`21,'custom',NULL,NULL,NULL,NULL,1.5`,
		`21,'recurring',NULL,NULL,NULL,NULL,1`,
		`21,'recurring',1,NULL,NULL,NULL,1`,
		`21,'recurring',NULL,'month',NULL,NULL,1`,
		`21,'recurring',0,'month',NULL,NULL,1`,
		`21,'recurring',-1,'month',NULL,NULL,1`,
		`21,'recurring',1.5,'month',NULL,NULL,1`,
		`21,'recurring',1,'hour',NULL,NULL,1`,
		`21,'recurring',9223372036854775808,'month',NULL,NULL,1`,
		`21,'once',1,'month',NULL,NULL,1`,
		`21,'custom',NULL,'month',NULL,NULL,1`,
		`21,'custom',1,NULL,NULL,NULL,1`,
		`21,'custom',NULL,NULL,0,NULL,1`,
		`21,'custom',NULL,NULL,NULL,'USD',1`,
		`21,'custom',NULL,NULL,-1,'USD',1`,
		`21,'custom',NULL,NULL,1.5,'USD',1`,
		`21,'custom',NULL,NULL,9223372036854775808,'USD',1`,
		`21,'custom',NULL,NULL,1,'usd',1`,
		`21,'custom',NULL,NULL,1,'US',1`,
		`21,'custom',NULL,NULL,1,'USDD',1`,
	}
	for _, values := range invalid {
		if _, err := s.DB.Exec(`INSERT INTO period_terms(period_id,mode,interval_count,interval_unit,amount,currency,revision) VALUES(` + values + `)`); err == nil {
			t.Fatalf("accepted invalid terms: %s", values)
		}
	}
	valid := []string{
		`21,'custom',NULL,NULL,NULL,NULL,1`,
		`21,'once',NULL,NULL,0,'USD',2`,
		`21,'recurring',1,'day',NULL,NULL,3`,
		`21,'recurring',2,'week',100,'TWD',4`,
		`21,'recurring',3,'month',9223372036854775807,'USD',5`,
		`21,'recurring',4,'year',0,'EUR',6`,
	}
	for _, values := range valid {
		storageExec(t, s, `DELETE FROM period_terms`, `INSERT INTO period_terms(period_id,mode,interval_count,interval_unit,amount,currency,revision) VALUES(`+values+`)`)
	}
	for _, assignment := range []string{"interval_count=NULL", "interval_unit=NULL", "revision=NULL", "amount=NULL", "currency=NULL"} {
		if _, err := s.DB.Exec("UPDATE period_terms SET " + assignment + " WHERE period_id=21"); err == nil {
			t.Fatalf("accepted invalid terms update: %s", assignment)
		}
	}
	storageExec(t, s, `DELETE FROM service_periods WHERE id=21`)
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM period_terms").Scan(&count); err != nil || count != 0 {
		t.Fatalf("terms cascade: count=%d err=%v", count, err)
	}
	assertStorageIntegrity(t, s)
}

func TestServicePeriodPrecisionConstraint(t *testing.T) {
	s := openTestStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,created_at,updated_at) VALUES(7,101,'UTC',1,1)`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(11,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`,
	)
	invalid := []string{
		`INSERT INTO service_periods(service_id,name,expires_at,date_precision,local_expires_date,timezone,created_at,updated_at) VALUES(11,'',200,'date',NULL,'UTC',1,1)`,
		`INSERT INTO service_periods(service_id,name,expires_at,date_precision,local_expires_date,timezone,created_at,updated_at) VALUES(11,'',200,'datetime','2026-01-01','UTC',1,1)`,
		`INSERT INTO service_periods(service_id,name,expires_at,date_precision,local_expires_date,timezone,created_at,updated_at) VALUES(11,'',200,'date','2026-1-1','UTC',1,1)`,
		`INSERT INTO service_periods(service_id,name,expires_at,date_precision,timezone,cycle,created_at,updated_at) VALUES(11,'',200,'datetime','UTC',0,1,1)`,
	}
	for _, statement := range invalid {
		if _, err := s.DB.Exec(statement); err == nil {
			t.Fatalf("accepted invalid period: %s", statement)
		}
	}
	// A missing terms row is a service-layer invariant; the schema only
	// constrains rows that exist.
	storageExec(t, s, `INSERT INTO service_periods(service_id,name,expires_at,date_precision,local_expires_date,timezone,created_at,updated_at) VALUES(11,'',200,'date','2026-01-01','UTC',1,1)`)
	if _, err := s.DB.Exec(`INSERT INTO service_items(user_id,name,category_id,status,created_at,updated_at) VALUES(999,'x',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`); err == nil {
		t.Fatal("accepted foreign-key violation")
	}
	assertStorageIntegrity(t, s)
}
