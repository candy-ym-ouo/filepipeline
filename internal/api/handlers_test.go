package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"filepipeline/internal/domain"
	"filepipeline/internal/migrate"
	"filepipeline/internal/repository"
	"filepipeline/internal/service"
	"filepipeline/internal/storage"
)

func testServer(t *testing.T) (*httptest.Server, *repository.Repository) {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrate.New().Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandlers(repository.New(db), store, 3, service.NewRetryPolicy(), log.New(io.Discard, "", 0))
	return httptest.NewServer(NewRouter(h, filepath.Join("..", "..", "web"), log.New(io.Discard, "", 0))), repository.New(db)
}

func TestUploadListAndDetail(t *testing.T) {
	server, _ := testServer(t)
	defer server.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "hello.txt")
	_, _ = part.Write([]byte("hello world"))
	writer.Close()
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		data, _ := io.ReadAll(res.Body)
		t.Fatalf("status=%d body=%s", res.StatusCode, data)
	}
	var task struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&task); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/tasks", "/api/v1/tasks/" + task.ID, "/healthz"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d", path, response.StatusCode)
		}
	}
}

func TestSignedScanCallback(t *testing.T) {
	server, repo := testServer(t)
	defer server.Close()
	now := time.Now()
	task, err := domain.NewTask(domain.NewTaskInput{Filename: "a.txt", StoredName: "f_a.txt", Size: 1, SHA256: "x", MIME: "text/plain", CallbackToken: "secret", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB().Exec(`UPDATE tasks SET stage=? WHERE id=?`, domain.StageScan, task.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimPending(context.Background(), now.Add(time.Second), time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%d err=%v", len(claimed), err)
	}
	if err := repo.MarkWaitingCallback(context.Background(), claimed[0], now.Add(time.Minute), now); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(domain.ScanCallback{TaskID: task.ID, Verdict: "clean", Scanner: "test"})
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write(body)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/scan-callback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Callback-Signature", hex.EncodeToString(mac.Sum(nil)))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	updated, err := repo.GetTask(context.Background(), task.ID)
	if err != nil || updated.Stage != domain.StageDone || updated.WaitingCallback {
		t.Fatalf("task=%+v err=%v", updated, err)
	}
}
