package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "data", "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func assertPragmas(t *testing.T, s *Store) {
	t.Helper()
	for name, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 5000, "synchronous": 2} {
		var got int
		if err := s.DB.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s = %d, want %d", name, got, want)
		}
	}
	var mode string
	if err := s.DB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q", mode)
	}
}

func TestOpenPragmasPermissionsAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "escape ?#& ü.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	assertPragmas(t, s)
	// Force a newly established physical connection, not just another checkout.
	s.DB.SetMaxIdleConns(0)
	assertPragmas(t, s)
	s.DB.SetMaxIdleConns(1)
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='users'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("Open applied migrations implicitly")
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("INSERT INTO users(telegram_user_id,timezone,created_at,updated_at) VALUES(123,'Asia/Taipei',1,1)"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("permissions %o, want %o", info.Mode().Perm(), want)
		}
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertPragmas(t, s)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("persisted users = %d", count)
	}
	if _, err := s.DB.Exec("INSERT INTO sims(user_id,name,card_type,status,created_at,updated_at) VALUES(999,'x','esim','active',1,1)"); err == nil {
		t.Fatal("foreign key violation accepted after reopen")
	}
}

func TestRejectMemoryDatabase(t *testing.T) {
	for _, path := range []string{"", " ", ":memory:", "file::memory:?cache=shared", "file:test.db"} {
		if s, err := Open(context.Background(), path); err == nil {
			_ = s.Close()
			t.Fatalf("accepted %q", path)
		}
	}
}

func TestMigrationsValidateHistoryAndRollback(t *testing.T) {
	ctx := context.Background()
	source := fstest.MapFS{"001_first.sql": {Data: []byte("CREATE TABLE first(id INTEGER PRIMARY KEY);")}}
	s := openTestStore(t)
	if err := s.migrate(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx, source); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migration count = %d", count)
	}
	changed := fstest.MapFS{"001_first.sql": {Data: []byte("CREATE TABLE first(id TEXT);")}}
	if err := s.migrate(ctx, changed); err == nil {
		t.Fatal("accepted altered checksum")
	}
	source["002_broken.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE rolled_back(id INTEGER); INSERT INTO missing_table VALUES(1);")}
	if err := s.migrate(ctx, source); err == nil {
		t.Fatal("accepted broken migration")
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='rolled_back'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("partial migration was committed")
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("failed version recorded")
	}
	source["002_broken.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE second(id INTEGER);")}
	if err := s.migrate(ctx, source); err != nil {
		t.Fatal(err)
	}
	delete(source, "002_broken.sql")
	if err := s.migrate(ctx, source); err == nil {
		t.Fatal("accepted database newer than program")
	}
}

func TestInitialSchemaConstraints(t *testing.T) {
	s := openTestStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixtures := []string{
		"INSERT INTO users(id,telegram_user_id,timezone,created_at,updated_at) VALUES(1,123,'Asia/Taipei',1,1)",
		"INSERT INTO sims(id,user_id,name,card_type,status,created_at,updated_at) VALUES(1,1,'card','esim','active',1,1)",
		"INSERT INTO validity_periods(id,sim_id,period_type,name,expires_at,date_precision,timezone,status,created_at,updated_at) VALUES(1,1,'custom','first',200,'datetime','Asia/Taipei','active',1,1),(2,1,'custom','second',200,'datetime','Asia/Taipei','active',1,1)",
		"INSERT INTO reminder_rules(id,validity_period_id,days_before,reminder_time,enabled,created_at,updated_at) VALUES(1,1,0,'09:00',1,1,1)",
		"INSERT INTO reminder_jobs(id,validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,next_attempt_at,created_at,updated_at) VALUES(1,1,1,1,1,100,200,'pending',100,1,1)",
	}
	for _, statement := range fixtures {
		if _, err := s.DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	invalid := []string{
		"DELETE FROM users WHERE id=1",
		"UPDATE sims SET card_type='bad' WHERE id=1",
		"UPDATE validity_periods SET date_precision='date' WHERE id=1",
		"UPDATE validity_periods SET cycle=0 WHERE id=1",
		"UPDATE reminder_rules SET reminder_time='24:00' WHERE id=1",
		"UPDATE reminder_rules SET reminder_time='9:00' WHERE id=1",
		"UPDATE reminder_rules SET enabled=2 WHERE id=1",
		"UPDATE reminder_jobs SET validity_period_id=2 WHERE id=1",
		"INSERT INTO reminder_jobs(validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,created_at,updated_at) VALUES(1,1,1,1,100,200,'pending',1,1)",
		"INSERT INTO renewals(validity_period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,timezone,renewed_at,amount,created_at) VALUES(1,1,200,300,'datetime','datetime','Asia/Taipei',1,0,1)",
	}
	for _, statement := range invalid {
		if _, err := s.DB.Exec(statement); err == nil {
			t.Fatalf("accepted invalid mutation: %s", statement)
		}
	}
}
