package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"filepipeline/internal/domain"
	"filepipeline/internal/migrate"
)

func testRepository(t *testing.T) *Repository {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrate.New().Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func createTask(t *testing.T, repo *Repository, now time.Time) domain.Task {
	t.Helper()
	task, err := domain.NewTask(domain.NewTaskInput{Filename: "a.txt", StoredName: "f_a.txt", Size: 5, SHA256: "x", MIME: "text/plain", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	return *task
}

func TestClaimCompleteAndEvents(t *testing.T) {
	repo := testRepository(t)
	now := time.Now()
	original := createTask(t, repo, now)
	claimed, err := repo.ClaimPending(context.Background(), now.Add(time.Second), time.Minute, 2)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%d err=%v", len(claimed), err)
	}
	if claimed[0].ID != original.ID || claimed[0].Status != domain.StatusProcessing {
		t.Fatalf("task=%+v", claimed[0])
	}
	if err := repo.CompleteStage(context.Background(), claimed[0], domain.StageExtract, "ok", "", nil, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.GetTask(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != domain.StatusPending || updated.Stage != domain.StageExtract {
		t.Fatalf("updated=%+v", updated)
	}
	events, err := repo.Events(context.Background(), original.ID)
	if err != nil || len(events) != 1 || events[0].Status != "SUCCEEDED" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestFailureManualRetryAndLeaseReclaim(t *testing.T) {
	repo := testRepository(t)
	now := time.Now()
	task := createTask(t, repo, now)
	claimed, _ := repo.ClaimPending(context.Background(), now.Add(time.Second), time.Second, 1)
	claimed[0].MaxAttempts = 1
	status, err := repo.FailStage(context.Background(), claimed[0], domain.NewError(domain.ErrExtract, "bad"), now, now)
	if err != nil || status != domain.StatusFailed {
		t.Fatalf("status=%s err=%v", status, err)
	}
	retried, err := repo.ManualRetry(context.Background(), task.ID, now.Add(time.Second))
	if err != nil || retried.Status != domain.StatusPending || retried.RetryCount != 1 {
		t.Fatalf("task=%+v err=%v", retried, err)
	}
	claimed, _ = repo.ClaimPending(context.Background(), now.Add(2*time.Second), time.Second, 1)
	count, err := repo.ReclaimExpired(context.Background(), now.Add(4*time.Second))
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestAsyncCallbackTimeoutHonorsMaxAttempts(t *testing.T) {
	repo := testRepository(t)
	now := time.Now()
	task := createTask(t, repo, now)
	if _, err := repo.DB().Exec(`UPDATE tasks SET stage=?,max_attempts=3 WHERE id=?`, domain.StageScan, task.ID); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		claimed, err := repo.ClaimPending(context.Background(), now.Add(time.Duration(attempt)*time.Second), time.Minute, 1)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("attempt=%d claimed=%d err=%v", attempt, len(claimed), err)
		}
		if err := repo.MarkWaitingCallback(context.Background(), claimed[0], now, now); err != nil {
			t.Fatal(err)
		}
		stored, err := repo.GetTask(context.Background(), task.ID)
		if err != nil || stored.Attempts != attempt {
			t.Fatalf("attempt counter=%d err=%v", stored.Attempts, err)
		}
		count, err := repo.ReclaimCallbackTimeouts(context.Background(), now.Add(time.Second))
		if err != nil || count != 1 {
			t.Fatalf("reclaimed=%d err=%v", count, err)
		}
		stored, err = repo.GetTask(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := domain.StatusPending
		if attempt == 3 {
			want = domain.StatusFailed
		}
		if stored.Status != want {
			t.Fatalf("attempt=%d status=%s want=%s", attempt, stored.Status, want)
		}
	}
}
