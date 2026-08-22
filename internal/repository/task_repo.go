package repository

import (
	"context"
	"filepipeline/internal/domain"
	"fmt"
	"time"
)

const taskColumns = `id,filename,stored_name,size,sha256,mime,status,stage,attempts,max_attempts,
retry_count,error_code,error_message,callback_url,callback_token,waiting_callback,extracted_summary,
scan_verdict,scan_scanner,scan_at,leased_until,next_run_at,version,created_at,updated_at`

type rowScanner interface{ Scan(...any) error }

func scanTask(row rowScanner) (domain.Task, error) {
	var task domain.Task
	var waiting int
	err := row.Scan(&task.ID, &task.Filename, &task.StoredName, &task.Size, &task.SHA256, &task.MIME,
		&task.Status, &task.Stage, &task.Attempts, &task.MaxAttempts, &task.RetryCount,
		&task.ErrorCode, &task.ErrorMessage, &task.CallbackURL, &task.CallbackToken, &waiting,
		&task.ExtractedSummary, &task.ScanVerdict, &task.ScanScanner, &task.ScanAt,
		&task.LeasedUntil, &task.NextRunAt, &task.Version, &task.CreatedAt, &task.UpdatedAt)
	task.WaitingCallback = waiting != 0
	return task, err
}
func (r *Repository) CreateTask(ctx context.Context, task *domain.Task) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}
	if err := task.Validate(); err != nil {
		return err
	}
	domain.TaskLabels[task.ID] = string(task.Stage)
	_, err := r.db.ExecContext(ctx, `INSERT INTO tasks(
		id,filename,stored_name,size,sha256,mime,status,stage,attempts,max_attempts,retry_count,
		error_code,error_message,callback_url,callback_token,waiting_callback,extracted_summary,
		scan_verdict,scan_scanner,scan_at,leased_until,next_run_at,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		task.ID, task.Filename, task.StoredName, task.Size, task.SHA256, task.MIME,
		task.Status, task.Stage, task.Attempts, task.MaxAttempts, task.RetryCount,
		task.ErrorCode, task.ErrorMessage, task.CallbackURL, task.CallbackToken, boolInt(task.WaitingCallback),
		task.ExtractedSummary, task.ScanVerdict, task.ScanScanner, task.ScanAt,
		task.LeasedUntil, task.NextRunAt, task.Version, task.CreatedAt, task.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	return nil
}
func (r *Repository) GetTask(ctx context.Context, id string) (domain.Task, error) {
	task, err := scanTask(r.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id=?`, id))
	if err != nil {
		return domain.Task{}, isNotFound(err)
	}
	return task, nil
}
func (r *Repository) ListTasks(ctx context.Context, opts ListOptions) (ListResult, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PageSize < 1 {
		opts.PageSize = 20
	}
	where, args := "", []any{}
	if opts.Status != "" {
		where, args = " WHERE status=?", append(args, opts.Status)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`+where, args...).Scan(&total); err != nil {
		return ListResult{}, fmt.Errorf("count tasks: %w", err)
	}
	args = append(args, opts.PageSize, (opts.Page-1)*opts.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT `+taskColumns+` FROM tasks`+where+
		` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, task)
	}
	return ListResult{Items: items, Total: total, Page: opts.Page, PageSize: opts.PageSize}, rows.Err()
}
func (r *Repository) ClaimPending(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		return nil, nil
	}
	nowText := domain.FormatTime(now)
	rows, err := r.db.QueryContext(ctx, `SELECT `+taskColumns+` FROM tasks
		WHERE status=? AND waiting_callback=0 AND next_run_at<=?
		ORDER BY created_at ASC LIMIT ?`, domain.StatusPending, nowText, limit)
	if err != nil {
		return nil, fmt.Errorf("find pending tasks: %w", err)
	}
	candidates := make([]domain.Task, 0, limit)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, task)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	claimed := make([]domain.Task, 0, len(candidates))
	for _, task := range candidates {
		leaseUntil := domain.FormatTime(now.Add(lease))
		result, err := r.db.ExecContext(ctx, `UPDATE tasks SET status=?,version=version+1,
			leased_until=?,updated_at=? WHERE id=? AND status=? AND version=? AND next_run_at<=?`,
			domain.StatusProcessing, leaseUntil, nowText, task.ID, domain.StatusPending, task.Version, nowText)
		if err != nil {
			return nil, fmt.Errorf("claim task %s: %w", task.ID, err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if count == 1 {
			task.Status = domain.StatusProcessing
			task.Version++
			task.LeasedUntil = leaseUntil
			task.UpdatedAt = nowText
			claimed = append(claimed, task)
		}
	}
	return claimed, nil
}
func (r *Repository) CompleteStage(ctx context.Context, task domain.Task, next domain.Stage,
	message, summary string, scan *domain.ScanResult, now time.Time) error {
	tx, err := begin(ctx, r.db)
	if err != nil {
		return err
	}
	defer rollback(tx)
	nowText := domain.FormatTime(now)
	scanVerdict, scanScanner, scanAt := task.ScanVerdict, task.ScanScanner, task.ScanAt
	if scan != nil {
		scanVerdict, scanScanner, scanAt = scan.Verdict, scan.Scanner, scan.ScannedAt
	}
	if summary == "" {
		summary = task.ExtractedSummary
	}
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET status=?,stage=?,attempts=0,error_code='',
		error_message='',extracted_summary=?,scan_verdict=?,scan_scanner=?,scan_at=?,leased_until='',
		next_run_at=?,version=version+1,updated_at=? WHERE id=? AND status=? AND version=?`,
		domain.StatusPending, next, summary, scanVerdict, scanScanner, scanAt, nowText, nowText,
		task.ID, domain.StatusProcessing, task.Version)
	if err != nil {
		return err
	}
	if err := affectedOne(result, "complete stage"); err != nil {
		return err
	}
	if err := appendEventTx(ctx, tx, task.ID, task.Stage, "SUCCEEDED", task.Attempts+1, message, nowText); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) FinishSuccess(ctx context.Context, task domain.Task, now time.Time) error {
	tx, err := begin(ctx, r.db)
	if err != nil {
		return err
	}
	defer rollback(tx)
	nowText := domain.FormatTime(now)
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET status=?,attempts=0,leased_until='',
		version=version+1,updated_at=? WHERE id=? AND status=? AND stage=? AND version=?`,
		domain.StatusSucceeded, nowText, task.ID, domain.StatusProcessing, domain.StageDone, task.Version)
	if err != nil {
		return err
	}
	if err := affectedOne(result, "finish task"); err != nil {
		return err
	}
	if err := appendEventTx(ctx, tx, task.ID, domain.StageDone, "SUCCEEDED", 1, "流水线处理完成", nowText); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) FailStage(ctx context.Context, task domain.Task, cause error,
	nextRun time.Time, now time.Time) (domain.Status, error) {
	tx, err := begin(ctx, r.db)
	if err != nil {
		return "", err
	}
	defer rollback(tx)
	attempt := task.Attempts + 1
	status, eventStatus := domain.StatusPending, "RETRYING"
	if attempt >= task.MaxAttempts {
		status, eventStatus = domain.StatusFailed, "FAILED"
		if task.RetryCount >= domain.MaxManualRetries {
			status = domain.StatusDead
		}
	}
	nowText := domain.FormatTime(now)
	nextText := domain.FormatTime(nextRun)
	if status != domain.StatusPending {
		nextText = nowText
	}
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET status=?,attempts=?,error_code=?,error_message=?,
		leased_until='',next_run_at=?,version=version+1,updated_at=?
		WHERE id=? AND status=? AND version=?`, status, attempt, domain.ErrorCode(cause),
		domain.ErrorMessage(cause), nextText, nowText, task.ID, domain.StatusProcessing, task.Version)
	if err != nil {
		return "", err
	}
	if err := affectedOne(result, "fail stage"); err != nil {
		return "", err
	}
	message := domain.ErrorMessage(cause)
	if status == domain.StatusPending {
		message = fmt.Sprintf("%s；将在 %s 重试", message, nextText)
	}
	if err := appendEventTx(ctx, tx, task.ID, task.Stage, eventStatus, attempt, message, nowText); err != nil {
		return "", err
	}
	return status, tx.Commit()
}
func (r *Repository) MarkWaitingCallback(ctx context.Context, task domain.Task, until, now time.Time) error {
	tx, err := begin(ctx, r.db)
	if err != nil {
		return err
	}
	defer rollback(tx)
	nowText := domain.FormatTime(now)
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET status=?,waiting_callback=1,attempts=attempts+1,leased_until='',
		next_run_at=?,version=version+1,updated_at=? WHERE id=? AND status=? AND stage=? AND version=?`,
		domain.StatusPending, domain.FormatTime(until), nowText, task.ID, domain.StatusProcessing,
		domain.StageScan, task.Version)
	if err != nil {
		return err
	}
	if err := affectedOne(result, "mark callback wait"); err != nil {
		return err
	}
	if err := appendEventTx(ctx, tx, task.ID, domain.StageScan, "RETRYING", task.Attempts+1,
		"扫描请求已提交，等待异步回调", nowText); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) ApplyCallback(ctx context.Context, task domain.Task, result domain.ScanResult,
	now time.Time, retryAt time.Time) error {
	if !result.Valid() {
		return domain.NewError(domain.ErrBadRequest, "verdict 仅允许 clean/infected/error")
	}
	tx, err := begin(ctx, r.db)
	if err != nil {
		return err
	}
	defer rollback(tx)
	nowText := domain.FormatTime(now)
	status, stage, attempts, eventStatus := domain.StatusPending, domain.StageDone, 0, "SUCCEEDED"
	eventAttempt, nextText := max(1, task.Attempts), nowText
	if result.Verdict == "error" {
		stage, attempts, eventStatus = domain.StageScan, eventAttempt, "RETRYING"
		nextText = domain.FormatTime(retryAt)
		if attempts >= task.MaxAttempts {
			status, eventStatus, nextText = domain.StatusFailed, "FAILED", nowText
			if task.RetryCount >= domain.MaxManualRetries {
				status = domain.StatusDead
			}
		}
	}
	resultSQL, err := tx.ExecContext(ctx, `UPDATE tasks SET status=?,stage=?,attempts=?,waiting_callback=0,
		scan_verdict=?,scan_scanner=?,scan_at=?,error_code=?,error_message=?,next_run_at=?,
		version=version+1,updated_at=? WHERE id=? AND waiting_callback=1 AND stage=?`,
		status, stage, attempts, result.Verdict, result.Scanner, result.ScannedAt,
		callbackErrorCode(result.Verdict), callbackErrorMessage(result.Verdict), nextText, nowText,
		task.ID, domain.StageScan)
	if err != nil {
		return err
	}
	if err := affectedOne(resultSQL, "apply callback"); err != nil {
		return domain.NewError(domain.ErrTaskAlreadyFinal, "任务不处于等待回调状态")
	}
	message := "扫描回调完成: " + result.Verdict
	if err := appendEventTx(ctx, tx, task.ID, domain.StageScan, eventStatus, eventAttempt, message, nowText); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) ManualRetry(ctx context.Context, id string, now time.Time) (domain.Task, error) {
	task, err := r.GetTask(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	if !task.Retryable() {
		return domain.Task{}, domain.NewError(domain.ErrTaskNotRetryable, "任务状态不允许重试")
	}
	tx, err := begin(ctx, r.db)
	if err != nil {
		return domain.Task{}, err
	}
	defer rollback(tx)
	nowText := domain.FormatTime(now)
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET status=?,attempts=0,retry_count=retry_count+1,
		error_code='',error_message='',next_run_at=?,version=version+1,updated_at=? WHERE id=? AND status=?`,
		domain.StatusPending, nowText, nowText, id, domain.StatusFailed)
	if err != nil {
		return domain.Task{}, err
	}
	if err := affectedOne(result, "manual retry"); err != nil {
		return domain.Task{}, domain.NewError(domain.ErrTaskNotRetryable, "任务状态不允许重试")
	}
	if err := appendEventTx(ctx, tx, id, task.Stage, "RETRYING", 0, "用户发起手动重试", nowText); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	return r.GetTask(ctx, id)
}
func (r *Repository) ReclaimExpired(ctx context.Context, now time.Time) (int, error) {
	return r.reclaim(ctx, `status=? AND leased_until<>'' AND leased_until<?`,
		[]any{domain.StatusProcessing, domain.FormatTime(now)}, false, now, "Worker 租约过期，任务重新入队")
}
func (r *Repository) ReclaimCallbackTimeouts(ctx context.Context, now time.Time) (int, error) {
	return r.reclaim(ctx, `waiting_callback=1 AND next_run_at<=?`,
		[]any{domain.FormatTime(now)}, true, now, "异步回调超时，重新提交扫描")
}
func (r *Repository) reclaim(ctx context.Context, where string, args []any, callback bool,
	now time.Time, message string) (int, error) {
	tx, err := begin(ctx, r.db)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)
	rows, err := tx.QueryContext(ctx, `SELECT id,stage,attempts,max_attempts,retry_count FROM tasks WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	type item struct {
		id          string
		stage       domain.Stage
		attempts    int
		maxAttempts int
		retryCount  int
	}
	var items []item
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.id, &value.stage, &value.attempts, &value.maxAttempts, &value.retryCount); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, value)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	nowText := domain.FormatTime(now)
	for _, value := range items {
		status, eventStatus, errorCode, errorMessage := domain.StatusPending, "RETRYING", "", ""
		query := `UPDATE tasks SET status=?,leased_until='',next_run_at=?,version=version+1,updated_at=?,error_code=?,error_message=? WHERE id=?`
		if callback {
			query = `UPDATE tasks SET status=?,waiting_callback=0,leased_until='',next_run_at=?,version=version+1,updated_at=?,error_code=?,error_message=? WHERE id=?`
			if value.attempts >= value.maxAttempts {
				status, eventStatus, errorCode, errorMessage = domain.StatusFailed, "FAILED", domain.ErrScan, "异步扫描回调超时"
				if value.retryCount >= domain.MaxManualRetries {
					status = domain.StatusDead
				}
			}
		}
		if _, err := tx.ExecContext(ctx, query, status, nowText, nowText, errorCode, errorMessage, value.id); err != nil {
			return 0, err
		}
		eventMessage := message
		if eventStatus == "FAILED" {
			eventMessage = errorMessage
		}
		if err := appendEventTx(ctx, tx, value.id, value.stage, eventStatus, value.attempts, eventMessage, nowText); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func callbackErrorCode(verdict string) string {
	if verdict == "error" {
		return domain.ErrScan
	}
	return ""
}
func callbackErrorMessage(verdict string) string {
	if verdict == "error" {
		return "扫描服务返回 error"
	}
	return ""
}
