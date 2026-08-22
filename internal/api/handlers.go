package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"filepipeline/internal/domain"
	"filepipeline/internal/repository"
	"filepipeline/internal/service"
	"filepipeline/internal/storage"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Handlers struct {
	repo        *repository.Repository
	storage     storage.Storage
	maxAttempts int
	retry       *service.RetryPolicy
	logger      *log.Logger
	now         func() time.Time
}

func NewHandlers(repo *repository.Repository, store storage.Storage, maxAttempts int,
	retry *service.RetryPolicy, logger *log.Logger) *Handlers {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if retry == nil {
		retry = service.NewRetryPolicy()
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Handlers{repo: repo, storage: store, maxAttempts: maxAttempts,
		retry: retry, logger: logger, now: time.Now}
}
func (h *Handlers) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "service": "file-pipeline", "time": domain.FormatTime(h.now()),
	})
}
func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		writeError(w, http.StatusUnsupportedMediaType, domain.ErrUnsupportedMedia, "上传必须使用 multipart/form-data")
		return
	}
	if r.ContentLength > domain.MaxFileSize+1<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, domain.ErrFileTooLarge, "文件超过 10MiB 限制")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, domain.MaxFileSize+1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, domain.ErrFileTooLarge, "文件超过 10MiB 限制")
		} else {
			writeError(w, http.StatusBadRequest, domain.ErrBadRequest, "无法解析上传请求")
		}
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrBadRequest, "缺少 file 字段")
		return
	}
	defer file.Close()
	stored, err := h.storage.Save(r.Context(), header.Filename, file, domain.MaxFileSize)
	if err != nil {
		var appErr *domain.Error
		if errors.As(err, &appErr) && appErr.Code == domain.ErrFileTooLarge {
			writeError(w, http.StatusRequestEntityTooLarge, appErr.Code, appErr.Message)
		} else {
			h.logger.Printf("[api] save_upload_error=%v", err)
			writeError(w, http.StatusInternalServerError, domain.ErrInternal, "保存文件失败")
		}
		return
	}
	task, err := domain.NewTask(domain.NewTaskInput{
		Filename: header.Filename, StoredName: stored.Name, Size: stored.Size,
		SHA256: stored.SHA256, MIME: stored.MIME, CallbackURL: r.FormValue("callback_url"),
		CallbackToken: r.FormValue("callback_token"), MaxAttempts: h.maxAttempts, Now: h.now(),
	})
	if err != nil {
		_ = h.storage.Remove(context.Background(), stored.Name)
		writeDomainError(w, err)
		return
	}
	if err := h.repo.CreateTask(r.Context(), task); err != nil {
		_ = h.storage.Remove(context.Background(), stored.Name)
		h.logger.Printf("[api] create_task_error=%v", err)
		writeError(w, http.StatusInternalServerError, domain.ErrInternal, "创建任务失败")
		return
	}
	writeJSON(w, http.StatusCreated, task)
}
func (h *Handlers) GetTask(w http.ResponseWriter, r *http.Request) {
	task, err := h.repo.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	events, err := h.repo.Events(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, domain.ErrInternal, "查询任务事件失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task, "events": events})
}
func (h *Handlers) ListTasks(w http.ResponseWriter, r *http.Request) {
	page, err := parseInteger(r.URL.Query().Get("page"), 1)
	if err != nil || page < 1 {
		writeError(w, http.StatusBadRequest, domain.ErrBadRequest, "page 必须是大于等于 1 的整数")
		return
	}
	pageSize, err := parseInteger(r.URL.Query().Get("page_size"), 20)
	if err != nil || pageSize < 1 || pageSize > 100 {
		writeError(w, http.StatusBadRequest, domain.ErrBadRequest, "page_size 必须在 1 到 100 之间")
		return
	}
	var status domain.Status
	if value := strings.ToUpper(r.URL.Query().Get("status")); value != "" {
		status, err = domain.ParseStatus(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, domain.ErrBadRequest, "status 参数不合法")
			return
		}
	}
	result, err := h.repo.ListTasks(r.Context(), repository.ListOptions{Status: status, Page: page, PageSize: pageSize})
	if err != nil {
		h.logger.Printf("[api] list_tasks_error=%v", err)
		writeError(w, http.StatusInternalServerError, domain.ErrInternal, "查询任务列表失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *Handlers) RetryTask(w http.ResponseWriter, r *http.Request) {
	task, err := h.repo.ManualRetry(r.Context(), r.PathValue("id"), h.now())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": task.ID, "status": task.Status, "retry_count": task.RetryCount})
}
func (h *Handlers) ScanCallback(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrBadRequest, "读取回调请求失败")
		return
	}
	var payload domain.ScanCallback
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrBadRequest, "回调 JSON 不合法")
		return
	}
	payload.Verdict = strings.ToLower(strings.TrimSpace(payload.Verdict))
	result := domain.ScanResult{Verdict: payload.Verdict, Scanner: payload.Scanner, ScannedAt: payload.ScannedAt}
	if !result.Valid() {
		writeError(w, http.StatusBadRequest, domain.ErrBadRequest, "verdict 仅允许 clean/infected/error")
		return
	}
	if payload.TaskID == "" {
		writeError(w, http.StatusBadRequest, domain.ErrBadRequest, "task_id 不能为空")
		return
	}
	task, err := h.repo.GetTask(r.Context(), payload.TaskID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if !task.WaitingCallback {
		writeError(w, http.StatusConflict, domain.ErrTaskAlreadyFinal, "任务不处于等待回调状态")
		return
	}
	signature := strings.TrimSpace(r.Header.Get("X-Callback-Signature"))
	if !validSignature(body, task.CallbackToken, signature) {
		writeError(w, http.StatusBadRequest, domain.ErrCallbackSignature, "回调签名校验失败")
		return
	}
	if result.Scanner == "" {
		result.Scanner = "external-scanner"
	}
	if result.ScannedAt == "" {
		result.ScannedAt = domain.FormatTime(h.now())
	}
	retryAt := h.now().Add(h.retry.Delay(task.Attempts + 1))
	if err := h.repo.ApplyCallback(r.Context(), task, result, h.now(), retryAt); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "task_id": task.ID})
}
func parseInteger(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}
func validSignature(body []byte, token, signature string) bool {
	if token == "" || signature == "" {
		return false
	}
	decoded, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write(body)
	return hmac.Equal(decoded, mac.Sum(nil))
}
func writeDomainError(w http.ResponseWriter, err error) {
	if err.Error() == domain.ErrTaskNotFound {
		writeError(w, http.StatusNotFound, domain.ErrTaskNotFound, "任务不存在")
		return
	}
	writeError(w, http.StatusInternalServerError, domain.ErrInternal, "服务内部错误")
}
