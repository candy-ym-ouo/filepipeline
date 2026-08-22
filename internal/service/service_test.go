package service

import (
	"context"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"filepipeline/internal/domain"
	"filepipeline/internal/storage"
)

func saveTask(t *testing.T, name, content string) (*storage.Local, domain.Task) {
	t.Helper()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Save(context.Background(), name, strings.NewReader(content), domain.MaxFileSize)
	if err != nil {
		t.Fatal(err)
	}
	task, err := domain.NewTask(domain.NewTaskInput{Filename: name, StoredName: stored.Name, Size: stored.Size, SHA256: stored.SHA256, MIME: stored.MIME})
	if err != nil {
		t.Fatal(err)
	}
	return store, *task
}

func TestValidatorAndExtractor(t *testing.T) {
	store, task := saveTask(t, "a.json", `{"name":"demo","items":[1,2]}`)
	if _, err := NewValidator(store).Validate(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	summary, _, err := NewExtractor(store).Extract(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "items") || !strings.Contains(summary, "name") {
		t.Fatalf("summary=%s", summary)
	}
	badStore, bad := saveTask(t, "bad.txt", "abc\x00def")
	if _, err := NewValidator(badStore).Validate(context.Background(), bad); domain.ErrorCode(err) != domain.ErrMagicMismatch {
		t.Fatalf("err=%v", err)
	}
}

func TestExtractorFormatsAndTruncation(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"a.csv", "name,age\na,1\nb,2\n", "columns: name, age"},
		{"a.yaml", "name: demo\nitems:\n  - one\n", "name"},
		{"a.xml", "<root><one/><two/></root>", "root: root"},
	}
	for _, tc := range cases {
		store, task := saveTask(t, tc.name, tc.body)
		summary, _, err := NewExtractor(store).Extract(context.Background(), task)
		if err != nil || !strings.Contains(summary, tc.want) {
			t.Fatalf("%s summary=%q err=%v", tc.name, summary, err)
		}
	}
	if len(truncateSummary(strings.Repeat("界", 3000))) > domain.MaxSummarySize {
		t.Fatal("summary was not truncated")
	}
}

func TestRetryAndMockScanner(t *testing.T) {
	policy := NewRetryPolicy()
	policy.SetSource(rand.NewSource(1))
	for attempt, expected := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		delay := policy.Delay(attempt + 1)
		if delay < time.Duration(float64(expected)*.8) || delay > time.Duration(float64(expected)*1.2) {
			t.Fatalf("delay=%s", delay)
		}
	}
	store, task := saveTask(t, "a.txt", "hello")
	out, err := NewScanner("mock://infected", time.Second, store).Scan(context.Background(), task)
	if err != nil || out.Result.Verdict != "infected" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if _, err := os.Stat(store.Path(task.StoredName)); err != nil {
		t.Fatal(err)
	}
}
