package storage

import "testing"

func storageExec(t *testing.T, s *Store, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := s.DB.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
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
