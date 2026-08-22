ALTER TABLE tasks ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 3;
ALTER TABLE tasks ADD COLUMN next_run_at TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_tasks_status_next ON tasks(status, next_run_at);
