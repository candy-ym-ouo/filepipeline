package migrate

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestApplyIsIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := New()
	if err := m.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("migration count=%d", count)
	}
	if _, err := db.Exec(`INSERT INTO tasks(id,filename,stored_name,size,sha256,mime,status,stage,next_run_at,created_at,updated_at) VALUES('t_1','a','f',1,'x','text/plain','PENDING','VALIDATE','n','n','n')`); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentApply(t *testing.T) {
	path := t.TempDir() + "/concurrent.db"
	open := func() *sql.DB {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	dbs := []*sql.DB{open(), open()}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, db := range dbs {
		go func(db *sql.DB) { <-start; errs <- New().Apply(context.Background(), db) }(db)
	}
	close(start)
	for range dbs {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}
