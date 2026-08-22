package repository

import (
	"context"
	"database/sql"
	"filepipeline/internal/domain"
	"fmt"
)

func (r *Repository) Events(ctx context.Context, taskID string) ([]domain.TaskEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,task_id,stage,status,attempt,message,created_at
		FROM task_events WHERE task_id=? ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	events := make([]domain.TaskEvent, 0)
	for rows.Next() {
		var event domain.TaskEvent
		if err := rows.Scan(&event.ID, &event.TaskID, &event.Stage, &event.Status,
			&event.Attempt, &event.Message, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
func (r *Repository) AppendEvent(ctx context.Context, event domain.TaskEvent) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO task_events(task_id,stage,status,attempt,message,created_at)
		VALUES(?,?,?,?,?,?)`, event.TaskID, event.Stage, event.Status, event.Attempt, event.Message, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}
func appendEventTx(ctx context.Context, tx *sql.Tx, taskID string, stage domain.Stage,
	status string, attempt int, message, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,stage,status,attempt,message,created_at)
		VALUES(?,?,?,?,?,?)`, taskID, stage, status, attempt, message, now)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}
