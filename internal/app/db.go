package app

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

func OpenDatabase(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return db, nil
}

func migrate(db *sql.DB) error {
	statements := strings.Split(schema, "-- statement")
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range statements {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("database migration: %w", err)
		}
	}
	return tx.Commit()
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
-- statement
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
-- statement
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);
-- statement
CREATE TABLE IF NOT EXISTS lists (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT '#357266',
  position INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
-- statement
CREATE TABLE IF NOT EXISTS tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  color TEXT NOT NULL DEFAULT '#687078',
  created_at TEXT NOT NULL
);
-- statement
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  notes TEXT NOT NULL DEFAULT '',
  list_id TEXT REFERENCES lists(id) ON DELETE SET NULL,
  due_at TEXT,
  priority INTEGER NOT NULL DEFAULT 0 CHECK(priority BETWEEN 0 AND 3),
  done INTEGER NOT NULL DEFAULT 0,
  completed_at TEXT,
  archived INTEGER NOT NULL DEFAULT 0,
  recurrence_unit TEXT NOT NULL DEFAULT '' CHECK(recurrence_unit IN ('', 'day', 'week', 'month', 'year')),
  recurrence_interval INTEGER NOT NULL DEFAULT 1 CHECK(recurrence_interval BETWEEN 1 AND 365),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
-- statement
CREATE INDEX IF NOT EXISTS idx_tasks_status_due ON tasks(done, archived, due_at);
-- statement
CREATE INDEX IF NOT EXISTS idx_tasks_list ON tasks(list_id);
-- statement
CREATE TABLE IF NOT EXISTS task_tags (
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY(task_id, tag_id)
);
-- statement
CREATE TABLE IF NOT EXISTS reminders (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK(kind IN ('absolute', 'relative')),
  offset_minutes INTEGER,
  trigger_at TEXT NOT NULL,
  sent_at TEXT,
  created_at TEXT NOT NULL
);
-- statement
CREATE INDEX IF NOT EXISTS idx_reminders_due ON reminders(sent_at, trigger_at);
-- statement
CREATE TABLE IF NOT EXISTS notifications (
  id TEXT PRIMARY KEY,
  task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  reminder_id TEXT,
  title TEXT NOT NULL,
  read_at TEXT,
  created_at TEXT NOT NULL
);
-- statement
CREATE INDEX IF NOT EXISTS idx_notifications_created ON notifications(created_at DESC);
-- statement
CREATE TABLE IF NOT EXISTS api_tokens (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  scopes TEXT NOT NULL,
  expires_at TEXT,
  last_used_at TEXT,
  created_at TEXT NOT NULL
);
-- statement
CREATE TABLE IF NOT EXISTS webhook_config (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  enabled INTEGER NOT NULL DEFAULT 0,
  url TEXT NOT NULL DEFAULT '',
  secret TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
-- statement
CREATE TABLE IF NOT EXISTS outbox (
  id TEXT PRIMARY KEY,
  event_id TEXT NOT NULL UNIQUE,
  event_type TEXT NOT NULL,
  payload TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'delivered', 'dead')),
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  delivered_at TEXT
);
-- statement
CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox(status, next_attempt_at);
-- statement
CREATE TABLE IF NOT EXISTS login_attempts (
  key TEXT PRIMARY KEY,
  failures INTEGER NOT NULL DEFAULT 0,
  blocked_until TEXT,
  updated_at TEXT NOT NULL
);
`
