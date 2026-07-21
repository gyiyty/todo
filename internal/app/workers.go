package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func (s *Server) StartWorkers(ctx context.Context) {
	go s.runTicker(ctx, 10*time.Second, s.processReminders)
	go s.runTicker(ctx, 10*time.Second, s.processOutbox)
	go s.runTicker(ctx, 6*time.Hour, s.cleanup)
	go s.runTicker(ctx, time.Hour, s.dailyBackup)
}

func (s *Server) dailyBackup(_ context.Context) {
	now := time.Now().In(s.cfg.Timezone)
	if now.Hour() < 3 || (now.Hour() == 3 && now.Minute() < 20) {
		return
	}
	destination := filepath.Join(s.cfg.BackupDir, "todo-"+now.Format("20060102")+".db")
	if _, err := os.Stat(destination); err == nil {
		return
	}
	if err := Backup(s.db, destination); err != nil {
		s.logger.Error("automatic backup", "error", err)
		return
	}
	if err := PruneBackups(s.cfg.BackupDir, 14*24*time.Hour); err != nil {
		s.logger.Warn("prune automatic backups", "error", err)
	}
	s.logger.Info("automatic backup complete", "destination", destination)
}

func (s *Server) runTicker(ctx context.Context, interval time.Duration, work func(context.Context)) {
	work(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			work(ctx)
		}
	}
}

func (s *Server) processReminders(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.task_id, r.trigger_at, t.title, t.due_at, t.priority, l.name
FROM reminders r JOIN tasks t ON t.id = r.task_id LEFT JOIN lists l ON l.id = t.list_id
WHERE r.sent_at IS NULL AND r.trigger_at <= ? AND t.done = 0 AND t.archived = 0 ORDER BY r.trigger_at LIMIT 50`, nowString())
	if err != nil {
		s.logger.Error("query reminders", "error", err)
		return
	}
	type dueReminder struct {
		ID, TaskID, TriggerAt, Title string
		DueAt, ListName              sql.NullString
		Priority                     int
	}
	items := []dueReminder{}
	for rows.Next() {
		var item dueReminder
		if rows.Scan(&item.ID, &item.TaskID, &item.TriggerAt, &item.Title, &item.DueAt, &item.Priority, &item.ListName) == nil {
			items = append(items, item)
		}
	}
	rows.Close()
	for _, item := range items {
		if err := s.fireReminder(ctx, item.ID, item.TaskID, item.TriggerAt, item.Title, item.DueAt, item.Priority, item.ListName); err != nil {
			s.logger.Error("fire reminder", "reminder_id", item.ID, "error", err)
		}
	}
}

func (s *Server) fireReminder(ctx context.Context, reminderID, taskID, triggerAt, title string, dueAt sql.NullString, priority int, listName sql.NullString) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE reminders SET sent_at = ? WHERE id = ? AND sent_at IS NULL", nowString(), reminderID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO notifications(id, task_id, reminder_id, title, created_at) VALUES(?, ?, ?, ?, ?)", newID("ntf"), taskID, reminderID, title, nowString())
	if err != nil {
		return err
	}
	var enabled int
	var webhookURL, secret string
	err = tx.QueryRowContext(ctx, "SELECT enabled, url, secret FROM webhook_config WHERE id=1").Scan(&enabled, &webhookURL, &secret)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if enabled == 1 && webhookURL != "" && secret != "" {
		eventID := newID("evt")
		payload := map[string]any{
			"event_id": eventID, "event_type": "reminder.due", "occurred_at": nowString(),
			"task":     map[string]any{"id": taskID, "title": title, "due_at": nullableString(dueAt), "priority": priority, "list": nullableString(listName)},
			"reminder": map[string]any{"id": reminderID, "trigger_at": triggerAt},
		}
		encoded, _ := json.Marshal(payload)
		_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id, event_id, event_type, payload, next_attempt_at, created_at) VALUES(?, ?, 'reminder.due', ?, ?, ?)`, newID("out"), eventID, string(encoded), nowString(), nowString())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func (s *Server) processOutbox(ctx context.Context) {
	var enabled int
	var webhookURL, secret string
	if err := s.db.QueryRowContext(ctx, "SELECT enabled, url, secret FROM webhook_config WHERE id=1").Scan(&enabled, &webhookURL, &secret); err != nil || enabled != 1 {
		return
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, event_id, payload, attempts, created_at FROM outbox WHERE status='pending' AND next_attempt_at <= ? ORDER BY next_attempt_at LIMIT 20", nowString())
	if err != nil {
		s.logger.Error("query outbox", "error", err)
		return
	}
	type item struct {
		id, eventID, payload, createdAt string
		attempts                        int
	}
	items := []item{}
	for rows.Next() {
		var value item
		if rows.Scan(&value.id, &value.eventID, &value.payload, &value.attempts, &value.createdAt) == nil {
			items = append(items, value)
		}
	}
	rows.Close()
	for _, value := range items {
		s.deliver(ctx, webhookURL, secret, value.id, value.eventID, value.payload, value.createdAt, value.attempts)
	}
}

func (s *Server) deliver(ctx context.Context, webhookURL, secret, id, eventID, payload, createdAt string, attempts int) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + payload))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.WebhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, webhookURL, bytes.NewBufferString(payload))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Todo-Event-ID", eventID)
		req.Header.Set("X-Todo-Timestamp", timestamp)
		req.Header.Set("X-Todo-Signature", signature)
		response, requestErr := http.DefaultClient.Do(req)
		if requestErr == nil {
			defer response.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				_, _ = s.db.ExecContext(ctx, "UPDATE outbox SET status='delivered', attempts=attempts+1, delivered_at=?, last_error='' WHERE id=?", nowString(), id)
				return
			}
			err = fmt.Errorf("HTTP %d", response.StatusCode)
		} else {
			err = requestErr
		}
	}
	created, _ := time.Parse(timeFormat, createdAt)
	status := "pending"
	if time.Since(created) > 7*24*time.Hour {
		status = "dead"
	}
	backoff := time.Duration(1<<min(attempts, 10)) * 30 * time.Second
	if backoff > 6*time.Hour {
		backoff = 6 * time.Hour
	}
	_, _ = s.db.ExecContext(ctx, "UPDATE outbox SET status=?, attempts=attempts+1, next_attempt_at=?, last_error=? WHERE id=?", status, time.Now().UTC().Add(backoff).Format(timeFormat), truncateError(err), id)
}

func truncateError(err error) string {
	if err == nil {
		return "unknown delivery error"
	}
	value := err.Error()
	if len(value) > 500 {
		return value[:500]
	}
	return value
}

func (s *Server) cleanup(ctx context.Context) {
	now := time.Now().UTC()
	_, _ = s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?", now.Format(timeFormat))
	_, _ = s.db.ExecContext(ctx, "DELETE FROM login_attempts WHERE updated_at < ?", now.Add(-24*time.Hour).Format(timeFormat))
	_, _ = s.db.ExecContext(ctx, "DELETE FROM notifications WHERE created_at < ?", now.AddDate(0, 0, -90).Format(timeFormat))
	_, _ = s.db.ExecContext(ctx, "DELETE FROM outbox WHERE status='delivered' AND delivered_at < ?", now.AddDate(0, 0, -30).Format(timeFormat))
}
