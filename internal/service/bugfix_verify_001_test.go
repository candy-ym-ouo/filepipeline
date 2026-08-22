package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"filepipeline/internal/domain"
	"filepipeline/internal/storage"
)

func TestBug001_ConcurrentScanKeepsTaskIdentity(t *testing.T) {
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := 0
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		seen++
		if seen == 2 {
			close(release)
		}
		mu.Unlock()
		select {
		case <-release:
		case <-time.After(time.Second):
			t.Error("scan requests did not overlap")
		}
		fmt.Fprint(w, `{"verdict":"clean","scanner":"test"}`)
	}))
	defer server.Close()

	pipeline := NewPipeline(NewValidator(store), NewExtractor(store), NewScanner(server.URL, time.Second, store))
	results := make(chan error, 2)
	for _, id := range []string{"t-one", "t-two"} {
		go func(id string) {
			_, err := pipeline.Execute(context.Background(), domain.Task{ID: id, Status: domain.StatusProcessing, Stage: domain.StageScan})
			results <- err
		}(id)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent scan failed: %v", err)
		}
	}
}
