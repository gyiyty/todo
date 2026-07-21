package app

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := r.Context().Value(principalKey).(principal)
		if p.Token {
			writeError(w, http.StatusForbidden, "administrator session required")
			return
		}
		next(w, r)
	}
}

type notification struct {
	ID, Title, CreatedAt       string
	TaskID, ReminderID, ReadAt *string
}

func (n notification) MarshalJSON() ([]byte, error) {
	type output struct {
		ID         string  `json:"id"`
		TaskID     *string `json:"task_id,omitempty"`
		ReminderID *string `json:"reminder_id,omitempty"`
		Title      string  `json:"title"`
		ReadAt     *string `json:"read_at,omitempty"`
		CreatedAt  string  `json:"created_at"`
	}
	return jsonMarshal(output{n.ID, n.TaskID, n.ReminderID, n.Title, n.ReadAt, n.CreatedAt})
}

func (s *Server) listNotifications(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query("SELECT id, task_id, reminder_id, title, read_at, created_at FROM notifications ORDER BY created_at DESC LIMIT 100")
	if err != nil {
		writeError(w, 500, "could not load notifications")
		return
	}
	defer rows.Close()
	items := []notification{}
	for rows.Next() {
		var item notification
		var taskID, reminderID, readAt sql.NullString
		if rows.Scan(&item.ID, &taskID, &reminderID, &item.Title, &readAt, &item.CreatedAt) == nil {
			item.TaskID = nullStringPointer(taskID)
			item.ReminderID = nullStringPointer(reminderID)
			item.ReadAt = nullStringPointer(readAt)
			items = append(items, item)
		}
	}
	writeJSON(w, 200, items)
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec("UPDATE notifications SET read_at=COALESCE(read_at, ?) WHERE id=?", nowString(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, "could not update notification")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, 404, "notification not found")
		return
	}
	w.WriteHeader(204)
}

type tokenSummary struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  *string  `json:"expires_at,omitempty"`
	LastUsedAt *string  `json:"last_used_at,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

func (s *Server) listTokens(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query("SELECT id,name,scopes,expires_at,last_used_at,created_at FROM api_tokens ORDER BY created_at DESC")
	if err != nil {
		writeError(w, 500, "could not load tokens")
		return
	}
	defer rows.Close()
	items := []tokenSummary{}
	for rows.Next() {
		var item tokenSummary
		var scopes string
		var expires, used sql.NullString
		if rows.Scan(&item.ID, &item.Name, &scopes, &expires, &used, &item.CreatedAt) == nil {
			item.Scopes = strings.Split(scopes, ",")
			item.ExpiresAt = nullStringPointer(expires)
			item.LastUsedAt = nullStringPointer(used)
			items = append(items, item)
		}
	}
	writeJSON(w, 200, items)
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ExpiresAt *string  `json:"expires_at"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 80 {
		writeError(w, 400, "token name is required")
		return
	}
	allowed := map[string]bool{"tasks:read": true, "tasks:write": true}
	if len(input.Scopes) == 0 {
		writeError(w, 400, "at least one scope is required")
		return
	}
	for _, scope := range input.Scopes {
		if !allowed[scope] {
			writeError(w, 400, "invalid scope: "+scope)
			return
		}
	}
	var expires any
	if input.ExpiresAt != nil && *input.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, *input.ExpiresAt)
		if err != nil || parsed.Before(time.Now()) {
			writeError(w, 400, "expires_at must be a future RFC3339 time")
			return
		}
		expires = parsed.UTC().Format(timeFormat)
	}
	raw := "tdk_" + randomToken(32)
	item := tokenSummary{ID: newID("tok"), Name: input.Name, Scopes: input.Scopes, CreatedAt: nowString()}
	_, err := s.db.Exec("INSERT INTO api_tokens(id,name,token_hash,scopes,expires_at,created_at) VALUES(?,?,?,?,?,?)", item.ID, item.Name, tokenHash(raw), strings.Join(input.Scopes, ","), expires, item.CreatedAt)
	if err != nil {
		writeError(w, 500, "could not create token")
		return
	}
	writeJSON(w, 201, map[string]any{"token": raw, "details": item})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec("DELETE FROM api_tokens WHERE id=?", chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, "could not revoke token")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, 404, "token not found")
		return
	}
	w.WriteHeader(204)
}

type astrBotConfig struct {
	Enabled   bool   `json:"enabled"`
	URL       string `json:"url"`
	HasSecret bool   `json:"has_secret"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func (s *Server) getAstrBotConfig(w http.ResponseWriter, _ *http.Request) {
	var enabled int
	var item astrBotConfig
	var secret string
	err := s.db.QueryRow("SELECT enabled,url,secret,updated_at FROM webhook_config WHERE id=1").Scan(&enabled, &item.URL, &secret, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		writeJSON(w, 200, item)
		return
	}
	if err != nil {
		writeError(w, 500, "could not load integration")
		return
	}
	item.Enabled = enabled == 1
	item.HasSecret = secret != ""
	writeJSON(w, 200, item)
}

func (s *Server) updateAstrBotConfig(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled bool    `json:"enabled"`
		URL     string  `json:"url"`
		Secret  *string `json:"secret"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.URL = strings.TrimSpace(input.URL)
	if input.Enabled {
		parsed, err := url.Parse(input.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			writeError(w, 400, "a valid HTTP(S) webhook URL is required")
			return
		}
	}
	var secret string
	_ = s.db.QueryRow("SELECT secret FROM webhook_config WHERE id=1").Scan(&secret)
	if input.Secret != nil {
		secret = strings.TrimSpace(*input.Secret)
	}
	if input.Enabled && len(secret) < 24 {
		writeError(w, 400, "webhook secret must contain at least 24 characters")
		return
	}
	_, err := s.db.Exec(`INSERT INTO webhook_config(id,enabled,url,secret,updated_at) VALUES(1,?,?,?,?) ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled,url=excluded.url,secret=excluded.secret,updated_at=excluded.updated_at`, boolInt(input.Enabled), input.URL, secret, nowString())
	if err != nil {
		writeError(w, 500, "could not save integration")
		return
	}
	s.getAstrBotConfig(w, r)
}

type delivery struct {
	ID            string  `json:"id"`
	EventID       string  `json:"event_id"`
	EventType     string  `json:"event_type"`
	Status        string  `json:"status"`
	Attempts      int     `json:"attempts"`
	NextAttemptAt string  `json:"next_attempt_at"`
	LastError     string  `json:"last_error"`
	CreatedAt     string  `json:"created_at"`
	DeliveredAt   *string `json:"delivered_at,omitempty"`
}

func (s *Server) listDeliveries(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query("SELECT id,event_id,event_type,status,attempts,next_attempt_at,last_error,created_at,delivered_at FROM outbox ORDER BY created_at DESC LIMIT 100")
	if err != nil {
		writeError(w, 500, "could not load deliveries")
		return
	}
	defer rows.Close()
	items := []delivery{}
	for rows.Next() {
		var item delivery
		var delivered sql.NullString
		if rows.Scan(&item.ID, &item.EventID, &item.EventType, &item.Status, &item.Attempts, &item.NextAttemptAt, &item.LastError, &item.CreatedAt, &delivered) == nil {
			item.DeliveredAt = nullStringPointer(delivered)
			items = append(items, item)
		}
	}
	writeJSON(w, 200, items)
}

func (s *Server) retryDelivery(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec("UPDATE outbox SET status='pending',next_attempt_at=?,last_error='' WHERE id=? AND status!='delivered'", nowString(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, "could not retry delivery")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, 409, "delivery is complete or does not exist")
		return
	}
	w.WriteHeader(204)
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
