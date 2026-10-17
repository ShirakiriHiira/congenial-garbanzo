// Package storage owns SQLite connection configuration and versioned migrations.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Store is shared by the application services. Transactions must use only their
// sql.Tx while active: the pool deliberately permits just one connection.
type Store struct {
	DB *sql.DB
}

// Open opens a file-backed database without applying migrations. Paths are
// filenames, not SQLite URIs; URI-looking input is rejected rather than allowing
// callers to override the connection pragmas or request an in-memory database.
func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil, errors.New("invalid database path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("resolve database path")
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, errors.New("create database directory")
	}
	file, err := os.OpenFile(absolute, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, errors.New("open database file")
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, errors.New("secure database file")
	}
	if err := file.Close(); err != nil {
		return nil, errors.New("close database file")
	}
	params := url.Values{}
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "busy_timeout(5000)")
	params.Add("_pragma", "synchronous(FULL)")
	uri := url.URL{Scheme: "file", Path: absolute, RawQuery: params.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, errors.New("open SQLite")
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func() (*Store, error) {
		_ = db.Close()
		return nil, errors.New("configure SQLite")
	}
	if err := db.PingContext(ctx); err != nil {
		return fail()
	}
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		return fail()
	}
	var synchronous int
	if err := db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil || synchronous != 2 {
		return fail()
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }
