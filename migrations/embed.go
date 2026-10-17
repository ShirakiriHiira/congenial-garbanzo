package migrations

import "embed"

// FS contains the immutable, versioned SQLite migrations.
//
//go:embed *.sql
var FS embed.FS
