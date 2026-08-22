package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

type Migrator struct {
	FS fs.FS
}

func New() Migrator { return Migrator{FS: migrationFiles} }
func (m Migrator) Apply(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}
	files, err := fs.Glob(m.FS, "sql/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)
	for _, name := range files {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		if applied[version] {
			continue
		}
		body, err := fs.ReadFile(m.FS, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := applyOne(ctx, db, version, filepath.Base(name), string(body)); err != nil {
			return err
		}
	}
	return nil
}
func appliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query migration versions: %w", err)
	}
	defer rows.Close()
	result := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		result[version] = true
	}
	return result, rows.Err()
}
func migrationVersion(name string) (int, error) {
	base := filepath.Base(name)
	part := strings.SplitN(base, "_", 2)[0]
	version, err := strconv.Atoi(part)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}
	return version, nil
}
func applyOne(ctx context.Context, db *sql.DB, version int, name, body string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open migration connection %s: %w", name, err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	var exists int
	err = conn.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version=?`, version).Scan(&exists)
	if err == nil {
		_, err = conn.ExecContext(ctx, `COMMIT`)
		return err
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("check migration %s: %w", name, err)
	}
	if _, err := conn.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("execute migration %s: %w", name, err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`,
		version, name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}
