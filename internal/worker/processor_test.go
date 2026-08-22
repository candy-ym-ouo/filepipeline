package worker

import (
	"context"
	"io"
	"log"
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

func TestProcessorSuccessfulPipeline(t *testing.T) {
	db, err := repository.Open(filepath.Join(t.TempDir(), "worker.db"))
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
	stored, err := store.Save(context.Background(), "hello.txt", strings.NewReader("hello pipeline"), domain.MaxFileSize)
	if err != nil {
		t.Fatal(err)
	}
	task, err := domain.NewTask(domain.NewTaskInput{Filename: "hello.txt", StoredName: stored.Name, Size: stored.Size, SHA256: stored.SHA256, MIME: stored.MIME, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	pipeline := service.NewPipeline(service.NewValidator(store), service.NewExtractor(store), service.NewScanner("mock://clean", time.Second, store))
	processor := NewProcessor(repo, pipeline, service.NewRetryPolicy(), time.Second, log.New(io.Discard, "", 0))
	for range 4 {
		claimed, err := repo.ClaimPending(context.Background(), time.Now().Add(time.Second), time.Minute, 1)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claimed=%d err=%v", len(claimed), err)
		}
		processor.Process(context.Background(), claimed[0])
	}
	finished, err := repo.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != domain.StatusSucceeded || finished.ScanVerdict != "clean" {
		t.Fatalf("task=%+v", finished)
	}
	events, err := repo.Events(context.Background(), task.ID)
	if err != nil || len(events) != 4 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
}
