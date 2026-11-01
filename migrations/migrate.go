package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"time"
)

// advisoryLockID serialises concurrent migrators. The value is arbitrary but
// stable across releases.
const advisoryLockID int64 = 0x70726F63757265 // "procure"

var fileNamePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.(up|down)\.sql$`)

// Migration is one reversible schema change.
type Migration struct {
	Version string
	Name    string
	Up      string
	Down    string
}

// Status describes whether a migration has been applied.
type Status struct {
	Version   string
	Name      string
	Applied   bool
	AppliedAt time.Time
}

// Up applies every pending migration in version order.
func Up(ctx context.Context, db *sql.DB) error {
	return withLock(ctx, db, func(conn *sql.Conn) error {
		if err := ensureTable(ctx, conn); err != nil {
			return err
		}

		applied, err := appliedVersions(ctx, conn)
		if err != nil {
			return err
		}

		list, err := load()
		if err != nil {
			return err
		}

		for _, m := range list {
			if _, ok := applied[m.Version]; ok {
				continue
			}
			if err := run(ctx, conn, m, true); err != nil {
				return fmt.Errorf("apply migration %s_%s: %w", m.Version, m.Name, err)
			}
		}
		return nil
	})
}

// Down rolls back the most recent applied migration. It returns false when
// there is nothing left to roll back.
func Down(ctx context.Context, db *sql.DB) (bool, error) {
	rolledBack := false

	err := withLock(ctx, db, func(conn *sql.Conn) error {
		if err := ensureTable(ctx, conn); err != nil {
			return err
		}

		applied, err := appliedVersions(ctx, conn)
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			return nil
		}

		list, err := load()
		if err != nil {
			return err
		}

		latest := ""
		for version := range applied {
			if version > latest {
				latest = version
			}
		}

		for i := len(list) - 1; i >= 0; i-- {
			m := list[i]
			if m.Version != latest {
				continue
			}
			if err := run(ctx, conn, m, false); err != nil {
				return fmt.Errorf("roll back migration %s_%s: %w", m.Version, m.Name, err)
			}
			rolledBack = true
			return nil
		}
		return fmt.Errorf("applied migration %q has no embedded up/down files", latest)
	})
	return rolledBack, err
}

// List reports the state of every embedded migration.
func List(ctx context.Context, db *sql.DB) ([]Status, error) {
	list, err := load()
	if err != nil {
		return nil, err
	}

	applied := map[string]time.Time{}
	err = withLock(ctx, db, func(conn *sql.Conn) error {
		if err := ensureTable(ctx, conn); err != nil {
			return err
		}

		rows, err := conn.QueryContext(ctx, `SELECT version, applied_at FROM schema_migrations`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var version string
			var appliedAt time.Time
			if err := rows.Scan(&version, &appliedAt); err != nil {
				return err
			}
			applied[version] = appliedAt
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	statuses := make([]Status, 0, len(list))
	for _, m := range list {
		appliedAt, ok := applied[m.Version]
		statuses = append(statuses, Status{
			Version:   m.Version,
			Name:      m.Name,
			Applied:   ok,
			AppliedAt: appliedAt,
		})
	}
	return statuses, nil
}

func load() ([]Migration, error) {
	return loadFS(Files)
}

func loadFS(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	byVersion := make(map[string]*Migration)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		match := fileNamePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".sql") {
				return nil, fmt.Errorf("migration file %q does not match NNNN_name.(up|down).sql", entry.Name())
			}
			continue
		}

		version, name, direction := match[1], match[2], match[3]
		content, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}

		m, ok := byVersion[version]
		if !ok {
			m = &Migration{Version: version, Name: name}
			byVersion[version] = m
		}
		if m.Name != name {
			return nil, fmt.Errorf("migration %s has conflicting names %q and %q", version, m.Name, name)
		}

		switch direction {
		case "up":
			m.Up = string(content)
		case "down":
			m.Down = string(content)
		}
	}

	list := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if strings.TrimSpace(m.Up) == "" {
			return nil, fmt.Errorf("migration %s_%s has no up file", m.Version, m.Name)
		}
		if strings.TrimSpace(m.Down) == "" {
			return nil, fmt.Errorf("migration %s_%s has no down file", m.Version, m.Name)
		}
		list = append(list, *m)
	}

	sort.Slice(list, func(i, j int) bool { return list[i].Version < list[j].Version })
	return list, nil
}

func ensureTable(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text        PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

func appliedVersions(ctx context.Context, conn *sql.Conn) (map[string]struct{}, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]struct{})
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}

func run(ctx context.Context, conn *sql.Conn, m Migration, up bool) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	statement := m.Up
	if !up {
		statement = m.Down
	}

	if _, err := tx.ExecContext(ctx, statement); err != nil {
		return err
	}

	if up {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, m.Version)
	} else {
		_, err = tx.ExecContext(ctx,
			`DELETE FROM schema_migrations WHERE version = $1`, m.Version)
	}
	if err != nil {
		return err
	}

	return tx.Commit()
}

func withLock(ctx context.Context, db *sql.DB, fn func(*sql.Conn) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, advisoryLockID)
	}()

	return fn(conn)
}
