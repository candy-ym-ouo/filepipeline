package service

import (
	"context"
	"testing"
	"time"

	"filepipeline/internal/domain"
	"filepipeline/internal/storage"
)

func TestBug004_AsyncScanDoesNotDerefNilResult(t *testing.T) {
	store, err := storage.NewLocal(t.TempDir())
	if err != nil { t.Fatal(err) }
	pipeline := NewPipeline(NewValidator(store), NewExtractor(store), NewScanner("mock://async", time.Second, store))
	defer func() {
		if recovered := recover(); recovered != nil { t.Fatalf("async scan panicked: %v", recovered) }
	}()
	out, err := pipeline.Execute(context.Background(), domain.Task{ID: "t-async", Status: domain.StatusProcessing, Stage: domain.StageScan})
	if err != nil { t.Fatal(err) }
	if !out.Waiting { t.Fatal("async scan was not marked waiting") }
}
