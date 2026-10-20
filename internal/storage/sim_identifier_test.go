package storage

import (
	"context"
	"io/fs"
	"testing"

	"telegram.bot.remind/migrations"
)

// Migration 004 clears legacy SIM identifiers and blocks new ones at the
// schema level, so a bypassed service layer cannot reintroduce them.
func TestSIMIdentifierIsClearedAndBlocked(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	storageExec(t, s,
		`INSERT INTO users(id,telegram_user_id,timezone,created_at,updated_at) VALUES(7,101,'UTC',1,1)`,
		`INSERT INTO service_items(id,user_id,name,category_id,identifier,status,created_at,updated_at) VALUES(11,7,'VPS',(SELECT id FROM service_categories WHERE code='vps'),'host.example','active',1,1)`,
		// Roll the schema back to its pre-004 shape so the backfill runs against
		// rows that already hold an identifier.
		`DROP TRIGGER service_items_sim_identifier_insert`,
		`DROP TRIGGER service_items_sim_identifier_update`,
		`INSERT INTO service_items(id,user_id,name,category_id,identifier,status,created_at,updated_at) VALUES(12,7,'SIM',(SELECT id FROM service_categories WHERE code='sim'),'legacy','active',1,1)`,
		`INSERT INTO sim_details(service_id,card_type) VALUES(12,'esim')`,
	)
	body, err := fs.ReadFile(migrations.FS, "004_sim_identifier_null.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	var identifier any
	if err := s.DB.QueryRow(`SELECT identifier FROM service_items WHERE id=12`).Scan(&identifier); err != nil || identifier != nil {
		t.Fatalf("legacy SIM identifier survived: %v %v", identifier, err)
	}
	if err := s.DB.QueryRow(`SELECT identifier FROM service_items WHERE id=11`).Scan(&identifier); err != nil || identifier != "host.example" {
		t.Fatalf("generic identifier changed: %v %v", identifier, err)
	}
	for _, statement := range []string{
		`INSERT INTO service_items(id,user_id,name,category_id,identifier,status,created_at,updated_at) VALUES(13,7,'SIM',(SELECT id FROM service_categories WHERE code='sim'),'x','active',1,1)`,
		`UPDATE service_items SET identifier='x' WHERE id=12`,
		`UPDATE service_items SET category_id=(SELECT id FROM service_categories WHERE code='sim') WHERE id=11`,
	} {
		if _, err := s.DB.Exec(statement); err == nil {
			t.Fatalf("accepted SIM identifier mutation: %s", statement)
		}
	}
	assertStorageIntegrity(t, s)
}
