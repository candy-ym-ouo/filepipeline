package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"filepipeline/internal/domain"
	"filepipeline/internal/migrate"
)

func TestBug007_CreateTaskDoesNotPanicOnFirstUse(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := migrate.New().Apply(context.Background(), db); err != nil { t.Fatal(err) }
	repo := New(db)
	task, err := domain.NewTask(domain.NewTaskInput{Filename: "a.txt", StoredName: "f_a.txt", Size: 1, SHA256: "x", MIME: "text/plain", Now: time.Now()})
	if err != nil { t.Fatal(err) }
	if err := repo.CreateTask(context.Background(), task); err != nil { t.Fatal(err) }
}
