CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  filename TEXT NOT NULL,
  stored_name TEXT NOT NULL,
  size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  mime TEXT NOT NULL,
  status TEXT NOT NULL,
  stage TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  callback_url TEXT NOT NULL DEFAULT '',
  callback_token TEXT NOT NULL DEFAULT '',
  extracted_summary TEXT NOT NULL DEFAULT '',
  leased_until TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  stage TEXT NOT NULL,
  status TEXT NOT NULL,
  attempt INTEGER NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_stage ON tasks(stage);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_lease ON tasks(status, leased_until);
CREATE INDEX IF NOT EXISTS idx_events_task ON task_events(task_id, created_at);
