package worker

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filepipeline/internal/domain"
	"filepipeline/internal/migrate"
	"filepipeline/internal/repository"
	"filepipeline/internal/service"
	"filepipeline/internal/storage"
)

func TestBug002_ProcessorPropagatesCancellation(t *testing.T) {
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate.New().Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Save(context.Background(), "a.txt", strings.NewReader("hello"), domain.MaxFileSize)
	if err != nil {
		t.Fatal(err)
	}
	task, err := domain.NewTask(domain.NewTaskInput{Filename: "a.txt", StoredName: stored.Name, Size: stored.Size, SHA256: stored.SHA256, MIME: stored.MIME})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB().Exec(`UPDATE tasks SET stage=? WHERE id=?`, domain.StageScan, task.ID); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		fmt.Fprint(w, `{"verdict":"clean","scanner":"test"}`)
	}))
	defer server.Close()
	pipeline := service.NewPipeline(service.NewValidator(store), service.NewExtractor(store), service.NewScanner(server.URL, time.Second, store))
	processor := NewProcessor(repo, pipeline, service.NewRetryPolicy(), time.Second, log.New(io.Discard, "", 0))
	claimed, err := repo.ClaimPending(context.Background(), time.Now().Add(time.Second), time.Second, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%d err=%v", len(claimed), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	processor.Process(ctx, claimed[0])
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("cancelled processor took %s", elapsed)
	}
}
