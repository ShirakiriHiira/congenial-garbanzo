package storage

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"telegram.bot.remind/migrations"
)

func storageMigrationSource(t *testing.T, versions ...int) fstest.MapFS {
	t.Helper()
	names := map[int]string{1: "001_initial.sql", 2: "002_hard_delete.sql", 3: "003_service_items.sql"}
	source := make(fstest.MapFS, len(versions))
	for _, version := range versions {
		name := names[version]
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		source[name] = &fstest.MapFile{Data: body}
	}
	return source
}

func storageExec(t *testing.T, s *Store, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := s.DB.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

// Historical fixtures intentionally retain the schema and field names of 001/002.
func seedServiceUpgradeGraph(t *testing.T, s *Store) {
	t.Helper()
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,private_chat_id,created_at,updated_at) VALUES(7,101,'Asia/Taipei',101,10,20),(8,102,'UTC',NULL,11,21)`,
		`INSERT INTO sims(id,user_id,name,card_type,operator,phone_number,country_or_region,iccid,notes,status,created_at,updated_at) VALUES(11,7,'eSIM','esim','carrier','+886123','TW','iccid-11','notes','active',12,22),(12,8,'physical','physical_sim',NULL,NULL,NULL,NULL,NULL,'archived',13,23)`,
	)
	types := []string{"number_validity", "plan_validity", "data_package_validity", "custom", "custom"}
	statuses := []string{"active", "expired", "active", "cancelled", "active"}
	jobs := []string{"pending", "sending", "sent", "failed", "cancelled"}
	for i := range 5 {
		parent := 11
		if i == 4 {
			parent = 12
		}
		precision, local, starts := "datetime", "NULL", "NULL"
		if i%2 == 0 {
			precision, local, starts = "date", "'2026-10-19'", "100"
		}
		name := fmt.Sprintf("'period-%d'", i)
		if i == 3 {
			name = "''"
		}
		storageExec(t, s,
			fmt.Sprintf(`INSERT INTO validity_periods(id,sim_id,period_type,name,starts_at,expires_at,date_precision,local_expires_date,timezone,status,cycle,created_at,updated_at) VALUES(%d,%d,'%s',%s,%s,%d,'%s',%s,'Asia/Taipei','%s',3,14,24)`, 21+i, parent, types[i], name, starts, 200+i, precision, local, statuses[i]),
			fmt.Sprintf(`INSERT INTO reminder_rules(id,validity_period_id,days_before,reminder_time,enabled,revision,created_at,updated_at) VALUES(%d,%d,%d,'09:30',%d,4,15,25)`, 31+i, 21+i, i, i%2),
			fmt.Sprintf(`INSERT INTO renewals(id,validity_period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,previous_local_expires_date,new_local_expires_date,timezone,renewed_at,amount,currency,notes,created_at) VALUES(%d,%d,2,100,%d,'datetime','%s',NULL,%s,'Asia/Taipei',16,0,'TWD','renewal notes',26)`, 41+i, 21+i, 200+i, precision, local),
		)
		lease, token, sent, message, lastError := "NULL", "NULL", "NULL", "NULL", "NULL"
		if jobs[i] == "sending" {
			lease, token, lastError = "999", "'in-flight-token'", "'timeout'"
		}
		if jobs[i] == "sent" {
			sent, message = "300", "987"
		}
		if jobs[i] == "failed" {
			lastError = "'forbidden'"
		}
		storageExec(t, s, fmt.Sprintf(`INSERT INTO reminder_jobs(id,validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,locked_until,attempt_token,sent_at,telegram_message_id,last_error_code,created_at,updated_at) VALUES(%d,%d,%d,3,4,150,%d,'%s',%d,160,%s,%s,%s,%s,%s,17,27)`, 51+i, 21+i, 31+i, 200+i, jobs[i], i+1, lease, token, sent, message, lastError))
	}
	storageExec(t, s, `INSERT INTO bot_dialogs(user_id,chat_id,action,validity_period_id,expected_cycle,expires_at,created_at) VALUES(7,101,'renew',22,3,999,18),(8,102,'renew',25,3,998,19)`)
}

var serviceUpgradeTables = []string{"users", "sims", "validity_periods", "reminder_rules", "renewals", "reminder_jobs", "bot_dialogs"}

func snapshotServiceUpgradeGraph(t *testing.T, s *Store) {
	t.Helper()
	for _, table := range serviceUpgradeTables {
		storageExec(t, s, "CREATE TEMP TABLE before_"+table+" AS SELECT * FROM "+table)
	}
}

func assertServiceUpgradeGraph(t *testing.T, s *Store, upgraded bool) {
	t.Helper()
	for _, table := range serviceUpgradeTables {
		projection := "SELECT * FROM " + table
		if table == "sims" && upgraded {
			projection = `SELECT s.id,s.user_id,s.name,d.card_type,s.provider,d.phone_number,d.country_or_region,d.iccid,s.notes,s.status,s.created_at,s.updated_at FROM service_items s JOIN sim_details d ON d.service_id=s.id`
		}
		query := "SELECT (SELECT count(*) FROM (" + projection + " EXCEPT SELECT * FROM before_" + table + ")) + (SELECT count(*) FROM (SELECT * FROM before_" + table + " EXCEPT " + projection + "))"
		var differences int
		if err := s.DB.QueryRow(query).Scan(&differences); err != nil || differences != 0 {
			t.Fatalf("%s projection changed: differences=%d err=%v", table, differences, err)
		}
	}
}

func assertStorageIntegrity(t *testing.T, s *Store) {
	t.Helper()
	var integrity string
	if err := s.DB.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check=%q err=%v", integrity, err)
	}
	rows, err := s.DB.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign_key_check found an orphan")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func storageSequences(t *testing.T, s *Store, parent string) map[string]int64 {
	t.Helper()
	sequences := make(map[string]int64)
	for _, table := range []string{parent, "validity_periods", "reminder_rules", "renewals", "reminder_jobs"} {
		var sequence int64
		if err := s.DB.QueryRow("SELECT seq FROM sqlite_sequence WHERE name=?", table).Scan(&sequence); err != nil {
			t.Fatal(err)
		}
		if table == "sims" {
			table = "service_items"
		}
		sequences[table] = sequence
	}
	return sequences
}

func storageMaxIDs(t *testing.T, s *Store) map[string]int64 {
	t.Helper()
	maximums := make(map[string]int64)
	for _, table := range []string{"sims", "validity_periods", "reminder_rules", "renewals", "reminder_jobs"} {
		var maximum int64
		if err := s.DB.QueryRow("SELECT coalesce(max(id),0) FROM " + table).Scan(&maximum); err != nil {
			t.Fatal(err)
		}
		if table == "sims" {
			table = "service_items"
		}
		maximums[table] = maximum
	}
	return maximums
}

func seedRetiredServiceGraph(t *testing.T, s *Store) {
	t.Helper()
	storageExec(t, s,
		`INSERT INTO sims(id,user_id,name,card_type,status,created_at,updated_at) VALUES(111,7,'retired','esim','active',1,1)`,
		`INSERT INTO validity_periods(id,sim_id,period_type,name,expires_at,date_precision,timezone,status,created_at,updated_at) VALUES(121,111,'custom','retired',200,'datetime','UTC','active',1,1)`,
		`INSERT INTO reminder_rules(id,validity_period_id,days_before,reminder_time,enabled,created_at,updated_at) VALUES(131,121,0,'09:00',1,1,1)`,
		`INSERT INTO renewals(id,validity_period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,timezone,renewed_at,created_at) VALUES(141,121,1,100,200,'datetime','datetime','UTC',1,1)`,
		`INSERT INTO reminder_jobs(id,validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,created_at,updated_at) VALUES(151,121,131,1,1,100,200,'sent',1,1)`,
		`DELETE FROM sims WHERE id=111`,
	)
}

func assertRetiredServiceIDs(t *testing.T, s *Store, sequences map[string]int64) {
	t.Helper()
	storageExec(t, s, "DELETE FROM service_items")
	for _, table := range []string{"service_items", "sim_details", "validity_periods", "period_terms", "reminder_rules", "renewals", "reminder_jobs", "bot_dialogs"} {
		var count int
		if err := s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("cascade %s: count=%d err=%v", table, count, err)
		}
	}
	insert := func(table, statement string, args ...any) int64 {
		t.Helper()
		result, err := s.DB.Exec(statement, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil || id <= sequences[table] {
			t.Fatalf("reused %s ID: id=%d highwater=%d err=%v", table, id, sequences[table], err)
		}
		return id
	}
	serviceID := insert("service_items", `INSERT INTO service_items(user_id,name,category_id,status,created_at,updated_at) VALUES(7,'next',(SELECT id FROM service_categories WHERE code='sim'),'active',1,1)`)
	storageExec(t, s, fmt.Sprintf(`INSERT INTO sim_details(service_id,card_type) VALUES(%d,'esim')`, serviceID))
	periodID := insert("validity_periods", `INSERT INTO validity_periods(service_id,period_type,name,expires_at,date_precision,timezone,status,created_at,updated_at) VALUES(?,'custom','next',200,'datetime','UTC','active',1,1)`, serviceID)
	ruleID := insert("reminder_rules", `INSERT INTO reminder_rules(validity_period_id,days_before,reminder_time,enabled,created_at,updated_at) VALUES(?,0,'09:00',1,1,1)`, periodID)
	insert("renewals", `INSERT INTO renewals(validity_period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,timezone,renewed_at,created_at) VALUES(?,1,100,200,'datetime','datetime','UTC',1,1)`, periodID)
	insert("reminder_jobs", `INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,created_at,updated_at) VALUES(?,?,1,1,100,200,'pending',1,1)`, periodID, ruleID)
	assertStorageIntegrity(t, s)
}

func TestServiceItemsMigrationUpgradePaths(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			ctx := context.Background()
			s := openTestStore(t)
			var sequences map[string]int64
			if version != 0 {
				versions := []int{1}
				if version == 2 {
					versions = append(versions, 2)
				}
				if err := s.migrate(ctx, storageMigrationSource(t, versions...)); err != nil {
					t.Fatal(err)
				}
				seedServiceUpgradeGraph(t, s)
				if version == 2 {
					seedRetiredServiceGraph(t, s)
					sequences = storageSequences(t, s, "sims")
				} else {
					// v1 tables are ROWID tables: ALTER keeps the implicit
					// high-water mark, so compare against the largest live ID.
					sequences = storageMaxIDs(t, s)
				}
				snapshotServiceUpgradeGraph(t, s)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := s.DB.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 3 {
				t.Fatalf("migration count=%d err=%v", count, err)
			}
			if err := s.DB.QueryRow("SELECT count(*) FROM period_terms").Scan(&count); err != nil || count != 0 {
				t.Fatalf("legacy terms were inferred: count=%d err=%v", count, err)
			}
			if err := s.DB.QueryRow("SELECT count(*) FROM service_categories").Scan(&count); err != nil || count != 5 {
				t.Fatalf("built-in count=%d err=%v", count, err)
			}
			if err := s.DB.QueryRow(`SELECT (SELECT count(*) FROM sqlite_master WHERE name='sims') + (SELECT count(*) FROM sqlite_sequence WHERE name='sims')`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("old parent schema survived: count=%d err=%v", count, err)
			}
			if err := s.DB.QueryRow(`SELECT (SELECT count(*) FROM pragma_table_info('service_items') WHERE name IN ('operator','card_type','phone_number','country_or_region','iccid')) + (SELECT count(*) FROM pragma_table_info('validity_periods') WHERE name='sim_id')`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("mirror columns survived: count=%d err=%v", count, err)
			}
			assertStorageIntegrity(t, s)
			assertPragmas(t, s)
			if version == 0 {
				return
			}
			assertServiceUpgradeGraph(t, s, true)
			if err := s.DB.QueryRow(`SELECT count(*) FROM service_items s JOIN service_categories c ON c.id=s.category_id WHERE c.code='sim' AND s.identifier IS NULL AND s.management_url IS NULL`).Scan(&count); err != nil || count != 2 {
				t.Fatalf("parent defaults: count=%d err=%v", count, err)
			}
			current := storageSequences(t, s, "service_items")
			for table, sequence := range sequences {
				if current[table] != sequence {
					t.Fatalf("%s highwater=%d want=%d", table, current[table], sequence)
				}
			}
			storageExec(t, s, `INSERT INTO period_terms(validity_period_id,mode,revision) VALUES(21,'custom',1)`)
			assertRetiredServiceIDs(t, s, current)
		})
	}
}

func TestServiceItemsMigrationPreservesDeletedEmptySequences(t *testing.T) {
	s := openTestStore(t)
	if err := s.migrate(context.Background(), storageMigrationSource(t, 1, 2)); err != nil {
		t.Fatal(err)
	}
	storageExec(t, s, `INSERT INTO users(id,telegram_user_id,timezone,created_at,updated_at) VALUES(7,101,'UTC',1,1)`)
	seedRetiredServiceGraph(t, s)
	before := storageSequences(t, s, "sims")
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := storageSequences(t, s, "service_items")
	for table, sequence := range before {
		if after[table] != sequence {
			t.Fatalf("empty %s highwater=%d want=%d", table, after[table], sequence)
		}
	}
	assertRetiredServiceIDs(t, s, before)
}

func TestServiceItemsMigrationRollsBackEntireVersion(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.migrate(ctx, storageMigrationSource(t, 1, 2)); err != nil {
		t.Fatal(err)
	}
	seedServiceUpgradeGraph(t, s)
	seedRetiredServiceGraph(t, s)
	snapshotServiceUpgradeGraph(t, s)
	before := storageSequences(t, s, "sims")
	source := storageMigrationSource(t, 1, 2, 3)
	broken := append([]byte(nil), source["003_service_items.sql"].Data...)
	source["003_service_items.sql"] = &fstest.MapFile{Data: append(broken, []byte("\nINSERT INTO missing_table VALUES(1);")...)}
	if err := s.migrate(ctx, source); err == nil {
		t.Fatal("accepted broken service migration")
	}
	assertServiceUpgradeGraph(t, s, false)
	for table, sequence := range storageSequences(t, s, "sims") {
		if sequence != before[table] {
			t.Fatalf("rollback changed %s sequence", table)
		}
	}
	var count int
	if err := s.DB.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('service_items','service_categories','sim_details','period_terms')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial schema survived: count=%d err=%v", count, err)
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 2 {
		t.Fatalf("failed migration recorded: count=%d err=%v", count, err)
	}
	assertStorageIntegrity(t, s)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	assertServiceUpgradeGraph(t, s, true)
	assertStorageIntegrity(t, s)
}

func TestServiceCategoryConstraints(t *testing.T) {
	s := openTestStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,created_at,updated_at) VALUES(7,101,'UTC',1,1),(8,102,'UTC',1,1)`,
		`INSERT INTO service_categories(id,user_id,name,kind,created_at,updated_at) VALUES(11,7,'Private','generic',1,1),(12,8,'Private','generic',1,1)`,
		// A parent at the rowid SQLite would synthesize for a NULL detail key
		// proves the WITHOUT ROWID primary key rejects NULL instead of silently
		// attaching details to an unrelated service.
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(1,7,'rowid parent',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(21,7,'SIM',(SELECT id FROM service_categories WHERE code='sim'),'active',1,1),(22,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1),(23,7,'Private',11,'active',1,1)`,
		`INSERT INTO sim_details(service_id,card_type) VALUES(21,'esim')`,
	)
	storageExec(t, s, `INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(24,7,'owner',(SELECT id FROM service_categories WHERE code='sim'),'active',1,1)`)
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
	want := map[string]string{"sim": "SIM 与 eSIM/sim", "vps": "VPS 与云服务器/generic", "subscription": "软件与 AI 订阅/generic", "domain": "域名/generic", "other": "其他/generic"}
	for code, expected := range want {
		var actual string
		if err := s.DB.QueryRow(`SELECT name || '/' || kind FROM service_categories WHERE code=? AND user_id IS NULL AND created_at>0 AND updated_at=created_at`, code).Scan(&actual); err != nil || actual != expected {
			t.Fatalf("built-in %s=%q want=%q err=%v", code, actual, expected, err)
		}
	}
	assertStorageIntegrity(t, s)
}

func TestPeriodTermsConstraints(t *testing.T) {
	s := openTestStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,created_at,updated_at) VALUES(7,101,'UTC',1,1)`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(1,7,'rowid parent',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`,
		`INSERT INTO service_items(id,user_id,name,category_id,status,created_at,updated_at) VALUES(11,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'active',1,1)`,
		`INSERT INTO validity_periods(id,service_id,period_type,name,expires_at,date_precision,timezone,status,created_at,updated_at) VALUES(1,1,'custom','',100,'datetime','UTC','active',1,1)`,
		`INSERT INTO validity_periods(id,service_id,period_type,name,expires_at,date_precision,timezone,status,created_at,updated_at) VALUES(21,11,'custom','',200,'datetime','UTC','active',1,1)`,
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
		if _, err := s.DB.Exec(`INSERT INTO period_terms(validity_period_id,mode,interval_count,interval_unit,amount,currency,revision) VALUES(` + values + `)`); err == nil {
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
		storageExec(t, s, `DELETE FROM period_terms`, `INSERT INTO period_terms(validity_period_id,mode,interval_count,interval_unit,amount,currency,revision) VALUES(`+values+`)`)
	}
	for _, assignment := range []string{"interval_count=NULL", "interval_unit=NULL", "revision=NULL", "amount=NULL", "currency=NULL"} {
		if _, err := s.DB.Exec("UPDATE period_terms SET " + assignment + " WHERE validity_period_id=21"); err == nil {
			t.Fatalf("accepted invalid terms update: %s", assignment)
		}
	}
	storageExec(t, s, `DELETE FROM validity_periods WHERE id=21`)
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM period_terms").Scan(&count); err != nil || count != 0 {
		t.Fatalf("terms cascade: count=%d err=%v", count, err)
	}
	assertStorageIntegrity(t, s)
}
