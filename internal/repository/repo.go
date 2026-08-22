package repository

import (
	"context"
	"database/sql"
	"filepipeline/internal/domain"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

type Repository struct {
	db *sql.DB
}
type ListOptions struct {
	Status   domain.Status
	Page     int
	PageSize int
}
type ListResult struct {
	Items    []domain.Task `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

func Open(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}
	if path != ":memory:" && path != "file::memory:?cache=shared" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	for _, pragma := range []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA busy_timeout=5000`, `PRAGMA foreign_keys=ON`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite pragma: %w", err)
		}
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}
func New(db *sql.DB) *Repository  { return &Repository{db: db} }
func (r *Repository) DB() *sql.DB { return r.db }
func isNotFound(err error) error {
	if err == sql.ErrNoRows {
		return domain.NewError(domain.ErrTaskNotFound, "任务不存在")
	}
	return err
}
func affectedOne(result sql.Result, operation string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%s: state conflict", operation)
	}
	return nil
}
func rollback(tx *sql.Tx) { _ = tx.Rollback() }
func begin(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	return tx, nil
}
