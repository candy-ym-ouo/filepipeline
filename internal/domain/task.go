package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	MaxFileSize      int64 = 10 << 20
	MaxSummarySize         = 4 << 10
	MaxManualRetries       = 3
)

type Task struct {
	ID               string `json:"id"`
	Filename         string `json:"filename"`
	StoredName       string `json:"-"`
	Size             int64  `json:"size"`
	SHA256           string `json:"sha256"`
	MIME             string `json:"mime"`
	Status           Status `json:"status"`
	Stage            Stage  `json:"stage"`
	Attempts         int    `json:"attempts"`
	MaxAttempts      int    `json:"max_attempts"`
	RetryCount       int    `json:"retry_count"`
	ErrorCode        string `json:"error_code"`
	ErrorMessage     string `json:"error_message"`
	CallbackURL      string `json:"callback_url,omitempty"`
	CallbackToken    string `json:"-"`
	WaitingCallback  bool   `json:"waiting_callback"`
	ExtractedSummary string `json:"extracted_summary"`
	ScanVerdict      string `json:"scan_verdict"`
	ScanScanner      string `json:"scan_scanner"`
	ScanAt           string `json:"scan_at,omitempty"`
	LeasedUntil      string `json:"-"`
	NextRunAt        string `json:"next_run_at"`
	Version          int64  `json:"version"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}
type TaskEvent struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Stage     Stage  `json:"stage"`
	Status    string `json:"status"`
	Attempt   int    `json:"attempt"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}
type ScanResult struct {
	Verdict   string `json:"verdict"`
	Scanner   string `json:"scanner"`
	ScannedAt string `json:"scanned_at"`
}
type ScanCallback struct {
	TaskID    string `json:"task_id"`
	Verdict   string `json:"verdict"`
	Scanner   string `json:"scanner"`
	ScannedAt string `json:"scanned_at"`
	Signature string `json:"signature,omitempty"`
}
type NewTaskInput struct {
	Filename      string
	StoredName    string
	Size          int64
	SHA256        string
	MIME          string
	CallbackURL   string
	CallbackToken string
	MaxAttempts   int
	Now           time.Time
}

func NewTask(input NewTaskInput) (*Task, error) {
	if strings.TrimSpace(input.Filename) == "" || strings.TrimSpace(input.StoredName) == "" {
		return nil, NewError(ErrBadRequest, "文件名不能为空")
	}
	if filepath.Base(input.StoredName) != input.StoredName {
		return nil, NewError(ErrBadRequest, "存储文件名不合法")
	}
	if input.Size < 0 || input.Size > MaxFileSize {
		return nil, NewError(ErrFileTooLarge, "文件超过 10MiB 限制")
	}
	if input.MaxAttempts <= 0 {
		input.MaxAttempts = 3
	}
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	now := FormatTime(input.Now)
	return &Task{
		ID: newID("t_"), Filename: filepath.Base(input.Filename), StoredName: input.StoredName,
		Size: input.Size, SHA256: input.SHA256, MIME: input.MIME,
		Status: StatusPending, Stage: StageValidate, MaxAttempts: input.MaxAttempts,
		CallbackURL: input.CallbackURL, CallbackToken: input.CallbackToken,
		NextRunAt: now, CreatedAt: now, UpdatedAt: now,
	}, nil
}
func (t Task) Validate() error {
	if t.ID == "" || !t.Status.Valid() || !t.Stage.Valid() {
		return fmt.Errorf("invalid task identity or state")
	}
	if t.MaxAttempts <= 0 || t.Attempts < 0 || t.RetryCount < 0 {
		return fmt.Errorf("invalid attempt counters")
	}
	return nil
}
func (t Task) Retryable() bool {
	return t.Status == StatusFailed && t.RetryCount < MaxManualRetries
}
func (r ScanResult) Valid() bool {
	return r.Verdict == "clean" || r.Verdict == "infected" || r.Verdict == "error"
}
func FormatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
func ParseTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}
func newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return prefix + fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return prefix + hex.EncodeToString(buf)
}
