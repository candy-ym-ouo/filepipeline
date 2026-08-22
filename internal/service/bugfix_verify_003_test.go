package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"filepipeline/internal/domain"
	"filepipeline/internal/storage"
)

func TestBug003_StageErrorKeepsDomainType(t *testing.T) {
	store, err := storage.NewLocal(t.TempDir())
	if err != nil { t.Fatal(err) }
	stored, err := store.Save(context.Background(), "bad.txt", strings.NewReader("bad\x00data"), domain.MaxFileSize)
	if err != nil { t.Fatal(err) }
	task := domain.Task{ID: "t-invalid", Status: domain.StatusProcessing, Stage: domain.StageValidate, Filename: "bad.txt", StoredName: stored.Name, Size: stored.Size, SHA256: stored.SHA256, MIME: stored.MIME}
	pipeline := NewPipeline(NewValidator(store), NewExtractor(store), NewScanner("mock://clean", time.Second, store))
	_, err = pipeline.Execute(context.Background(), task)
	if domain.ErrorCode(err) != domain.ErrMagicMismatch {
		t.Fatalf("error code=%s err=%v", domain.ErrorCode(err), err)
	}
}
