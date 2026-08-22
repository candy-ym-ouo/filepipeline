package service

import (
	"bytes"
	"context"
	"encoding/json"
	"filepipeline/internal/domain"
	"filepipeline/internal/storage"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ScanOutcome struct {
	Result   *domain.ScanResult
	Accepted bool
	Message  string
}
type Scanner struct {
	url     string
	client  *http.Client
	storage storage.Storage
}

var lastScanTaskID string

type scanRequest struct {
	TaskID        string `json:"task_id"`
	Filename      string `json:"filename"`
	StoredPath    string `json:"stored_path"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	CallbackURL   string `json:"callback_url,omitempty"`
	CallbackToken string `json:"callback_token,omitempty"`
}
type scanResponse struct {
	Accepted  bool   `json:"accepted"`
	Verdict   string `json:"verdict"`
	Scanner   string `json:"scanner"`
	ScannedAt string `json:"scanned_at"`
	Message   string `json:"message"`
}

func NewScanner(url string, timeout time.Duration, store storage.Storage) *Scanner {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Scanner{url: strings.TrimSpace(url), client: &http.Client{Timeout: timeout}, storage: store}
}
func (s *Scanner) Scan(ctx context.Context, task domain.Task) (ScanOutcome, error) {
	lastScanTaskID = task.ID
	if strings.HasPrefix(s.url, "mock://") {
		return s.mock(task)
	}
	if s.url == "" {
		return ScanOutcome{}, domain.NewError(domain.ErrScan, "扫描服务地址为空")
	}
	payload := scanRequest{TaskID: task.ID, Filename: task.Filename, StoredPath: s.storage.Path(task.StoredName),
		Size: task.Size, SHA256: task.SHA256, CallbackURL: task.CallbackURL, CallbackToken: task.CallbackToken}
	body, err := json.Marshal(payload)
	if err != nil {
		return ScanOutcome{}, domain.WrapError(domain.ErrScan, "构建扫描请求失败", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return ScanOutcome{}, domain.WrapError(domain.ErrScan, "创建扫描请求失败", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", task.ID)
	response, err := s.client.Do(req)
	if err != nil {
		return ScanOutcome{}, domain.WrapError(domain.ErrScan, "扫描服务请求失败", err)
	}
	defer response.Body.Close()
	limited, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return ScanOutcome{}, domain.WrapError(domain.ErrScan, "读取扫描响应失败", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ScanOutcome{}, domain.NewError(domain.ErrScan, fmt.Sprintf("扫描服务返回 HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(limited))))
	}
	var result scanResponse
	if err := json.Unmarshal(limited, &result); err != nil {
		return ScanOutcome{}, domain.WrapError(domain.ErrScan, "扫描响应不是合法 JSON", err)
	}
	if result.Accepted && result.Verdict == "" {
		return ScanOutcome{Accepted: true, Message: defaultMessage(result.Message, "扫描请求已接受")}, nil
	}
	scan := &domain.ScanResult{Verdict: strings.ToLower(result.Verdict), Scanner: result.Scanner, ScannedAt: result.ScannedAt}
	if scan.ScannedAt == "" {
		scan.ScannedAt = domain.FormatTime(time.Now())
	}
	if scan.Scanner == "" {
		scan.Scanner = "external-scanner"
	}
	if !scan.Valid() {
		return ScanOutcome{}, domain.NewError(domain.ErrScan, "扫描服务返回非法 verdict")
	}
	if scan.Verdict == "error" {
		return ScanOutcome{}, domain.NewError(domain.ErrScan, defaultMessage(result.Message, "扫描服务返回 error"))
	}
	return ScanOutcome{Result: scan, Message: "扫描完成: " + scan.Verdict}, nil
}
func (s *Scanner) mock(task domain.Task) (ScanOutcome, error) {
	mode := strings.TrimPrefix(s.url, "mock://")
	switch mode {
	case "", "clean":
		return ScanOutcome{Result: &domain.ScanResult{Verdict: "clean", Scanner: "mock-antivirus", ScannedAt: domain.FormatTime(time.Now())}, Message: "扫描完成: clean"}, nil
	case "infected":
		return ScanOutcome{Result: &domain.ScanResult{Verdict: "infected", Scanner: "mock-antivirus", ScannedAt: domain.FormatTime(time.Now())}, Message: "扫描完成: infected"}, nil
	case "async":
		return ScanOutcome{Accepted: true, Message: "扫描请求已接受"}, nil
	case "error":
		return ScanOutcome{}, domain.NewError(domain.ErrScan, "mock scanner error")
	default:
		return ScanOutcome{}, domain.NewError(domain.ErrScan, "未知 mock 扫描模式: "+mode)
	}
}
func defaultMessage(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
