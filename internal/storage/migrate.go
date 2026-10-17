package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"telegram.bot.remind/migrations"
)

type migration struct {
	version  int64
	checksum string
	sql      string
}

// Migrate verifies every applied checksum before applying pending versions.
func (s *Store) Migrate(ctx context.Context) error { return s.migrate(ctx, migrations.FS) }

// migrate accepts an isolated filesystem for corruption and upgrade tests.
func (s *Store) migrate(ctx context.Context, source fs.FS) error {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return errors.New("read migrations")
	}
	versions := make(map[int64]migration)
	ordered := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, found := strings.Cut(entry.Name(), "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if !found || err != nil || version < 1 {
			return errors.New("invalid migration filename")
		}
		if _, exists := versions[version]; exists {
			return errors.New("duplicate migration version")
		}
		body, err := fs.ReadFile(source, entry.Name())
		if err != nil {
			return errors.New("read migration")
		}
		sum := sha256.Sum256(body)
		item := migration{version: version, checksum: hex.EncodeToString(sum[:]), sql: string(body)}
		versions[version] = item
		ordered = append(ordered, item)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].version < ordered[j].version })
	if len(ordered) == 0 {
		return errors.New("no migrations")
	}
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at INTEGER NOT NULL
    )`); err != nil {
		return errors.New("initialize migration ledger")
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return errors.New("read migration ledger")
	}
	applied := make(map[int64]bool)
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			_ = rows.Close()
			return errors.New("read migration ledger")
		}
		expected, exists := versions[version]
		if !exists {
			_ = rows.Close()
			return errors.New("database migration version unsupported")
		}
		if checksum != expected.checksum {
			_ = rows.Close()
			return errors.New("migration checksum mismatch")
		}
		applied[version] = true
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil || closeErr != nil {
		return errors.New("read migration ledger")
	}
	// A hole below an applied version indicates an incompatible history, not an
	// invitation to apply an older migration against a newer schema.
	missing := false
	for _, item := range ordered {
		if !applied[item.version] {
			missing = true
		} else if missing {
			return errors.New("noncontiguous migration history")
		}
	}
	for _, item := range ordered {
		if applied[item.version] {
			continue
		}
		if err := s.applyMigration(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, item migration) error {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return errors.New("begin migration")
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, item.sql); err != nil {
		return errors.New("apply migration")
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,checksum,applied_at) VALUES(?,?,?)", item.version, item.checksum, time.Now().UTC().Unix()); err != nil {
		return errors.New("record migration")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit migration")
	}
	return nil
}
