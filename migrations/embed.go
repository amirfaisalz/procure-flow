// Package migrations embeds the SQL schema migrations and applies them with a
// small, dependency-free runner. Migrations are ordered by their zero-padded
// numeric prefix and recorded in the schema_migrations table.
package migrations

import "embed"

// Files contains every migration shipped with the binary.
//
//go:embed *.sql
var Files embed.FS
