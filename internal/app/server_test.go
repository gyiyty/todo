package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testEnvironment struct {
	server *Server
	http   *httptest.Server
	client *http.Client
	csrf   string
}

func newTestEnvironment(t *testing.T) *testEnvironment {
	t.Helper()
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := CreateAdmin(db, "tester", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadConfig()
	cfg.DatabasePath = filepath.Join(t.TempDir(), "unused.db")
	cfg.BaseURL = "http://example.test"
	server := NewServer(cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	env := &testEnvironment{server: server, http: httpServer, client: client}
	response := env.request(t, http.MethodPost, "/api/v1/session", map[string]any{"username": "tester", "password": "correct horse battery staple"}, false, "")
	var payload struct {
		CSRF string `json:"csrf_token"`
	}
	decodeResponse(t, response, &payload)
	env.csrf = payload.CSRF
	return env
}

func (e *testEnvironment) request(t *testing.T, method, path string, body any, withCSRF bool, bearer string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, _ := http.NewRequest(method, e.http.URL+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if withCSRF {
		req.Header.Set("X-CSRF-Token", e.csrf)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeResponse(t *testing.T, response *http.Response, value any) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected status %d: %s", response.StatusCode, data)
	}
	if value != nil && response.StatusCode != 204 {
		if err := json.NewDecoder(response.Body).Decode(value); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTaskLifecycleRecurrenceAndTokenScopes(t *testing.T) {
	env := newTestEnvironment(t)
	listResponse := env.request(t, http.MethodPost, "/api/v1/lists", map[string]any{"name": "工作"}, true, "")
	var list List
	decodeResponse(t, listResponse, &list)
	due := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	taskResponse := env.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{
		"title": "提交报告", "list_id": list.ID, "due_at": due, "priority": 3,
		"recurrence_unit": "day", "recurrence_interval": 1, "tag_ids": []string{},
		"reminders": []map[string]any{{"kind": "relative", "offset_minutes": -10}},
	}, true, "")
	var task Task
	decodeResponse(t, taskResponse, &task)
	if len(task.Reminders) != 1 || task.List == nil {
		t.Fatalf("task relations missing: %#v", task)
	}
	complete := env.request(t, http.MethodPost, "/api/v1/tasks/"+task.ID+"/complete", nil, true, "")
	var completed struct {
		Task Task  `json:"task"`
		Next *Task `json:"next"`
	}
	decodeResponse(t, complete, &completed)
	if !completed.Task.Done || completed.Next == nil || completed.Next.DueAt == nil {
		t.Fatal("recurring task did not create next occurrence")
	}

	tokenResponse := env.request(t, http.MethodPost, "/api/v1/tokens", map[string]any{"name": "AstrBot", "scopes": []string{"tasks:read"}}, true, "")
	var tokenPayload struct {
		Token string `json:"token"`
	}
	decodeResponse(t, tokenResponse, &tokenPayload)
	readResponse := env.request(t, http.MethodGet, "/api/v1/tasks", nil, false, tokenPayload.Token)
	decodeResponse(t, readResponse, &[]Task{})
	writeResponse := env.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "blocked"}, false, tokenPayload.Token)
	defer writeResponse.Body.Close()
	if writeResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", writeResponse.StatusCode)
	}
	adminResponse := env.request(t, http.MethodGet, "/api/v1/tokens", nil, false, tokenPayload.Token)
	defer adminResponse.Body.Close()
	if adminResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("token accessed admin API: %d", adminResponse.StatusCode)
	}
}

func TestCSRFMandatory(t *testing.T) {
	env := newTestEnvironment(t)
	response := env.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "should fail"}, false, "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected CSRF rejection, got %d", response.StatusCode)
	}
}

func TestTaskFiltersByPriorityAndTags(t *testing.T) {
	env := newTestEnvironment(t)
	createTag := func(name string) Tag {
		response := env.request(t, http.MethodPost, "/api/v1/tags", map[string]any{"name": name}, true, "")
		var tag Tag
		decodeResponse(t, response, &tag)
		return tag
	}
	work, focus := createTag("工作"), createTag("专注")
	tasks := []map[string]any{
		{"title": "匹配全部条件", "priority": 3, "tag_ids": []string{work.ID, focus.ID}},
		{"title": "缺少标签", "priority": 3, "tag_ids": []string{work.ID}},
		{"title": "优先级不同", "priority": 2, "tag_ids": []string{work.ID, focus.ID}},
	}
	for _, input := range tasks {
		decodeResponse(t, env.request(t, http.MethodPost, "/api/v1/tasks", input, true, ""), &Task{})
	}

	response := env.request(t, http.MethodGet, "/api/v1/tasks?priority=3&tag_id="+work.ID+"&tag_id="+focus.ID, nil, false, "")
	var filtered []Task
	decodeResponse(t, response, &filtered)
	if len(filtered) != 1 || filtered[0].Title != "匹配全部条件" {
		t.Fatalf("unexpected filtered tasks: %#v", filtered)
	}
}

func TestReminderWebhookSignature(t *testing.T) {
	env := newTestEnvironment(t)
	secret := "a webhook secret longer than twenty four chars"
	received := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		bodies <- data
		received <- r
		w.WriteHeader(204)
	}))
	defer target.Close()
	configResponse := env.request(t, http.MethodPut, "/api/v1/integrations/astrbot", map[string]any{"enabled": true, "url": target.URL, "secret": secret}, true, "")
	decodeResponse(t, configResponse, &map[string]any{})
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	taskResponse := env.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "到期任务", "notes": "携带雨伞", "due_at": past, "tag_ids": []string{}, "reminders": []map[string]any{{"kind": "absolute", "trigger_at": past}}}, true, "")
	decodeResponse(t, taskResponse, &Task{})
	env.server.processReminders(context.Background())
	env.server.processOutbox(context.Background())
	select {
	case request := <-received:
		body := <-bodies
		timestamp := request.Header.Get("X-Todo-Timestamp")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(timestamp + "." + string(body)))
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(request.Header.Get("X-Todo-Signature"))) {
			t.Fatal("invalid webhook signature")
		}
		if request.Header.Get("X-Todo-Event-ID") == "" {
			t.Fatal("missing event id")
		}
		var payload struct {
			Task struct {
				Title string `json:"title"`
				Notes string `json:"notes"`
			} `json:"task"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Task.Title != "到期任务" || payload.Task.Notes != "携带雨伞" {
			t.Fatalf("unexpected webhook task: %#v", payload.Task)
		}
	case <-time.After(time.Second):
		t.Fatal("webhook not delivered")
	}
}

func TestReminderWebhookIncludesEmptyNotes(t *testing.T) {
	env := newTestEnvironment(t)
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	response := env.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{
		"title": "无备注任务", "due_at": past, "tag_ids": []string{},
		"reminders": []map[string]any{{"kind": "absolute", "trigger_at": past}},
	}, true, "")
	decodeResponse(t, response, &Task{})
	_, err := env.server.db.Exec(`INSERT INTO webhook_config(id,enabled,url,secret,updated_at)
		VALUES(1,1,'http://example.invalid','a webhook secret longer than twenty four chars',?)`, nowString())
	if err != nil {
		t.Fatal(err)
	}
	env.server.processReminders(context.Background())
	var encoded string
	if err := env.server.db.QueryRow("SELECT payload FROM outbox").Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		t.Fatal(err)
	}
	task := payload["task"].(map[string]any)
	if notes, exists := task["notes"]; !exists || notes != "" {
		t.Fatalf("expected empty notes field, got %#v", task)
	}
}

func TestBackup(t *testing.T) {
	env := newTestEnvironment(t)
	destination := filepath.Join(t.TempDir(), "backups", "todo.db")
	if err := Backup(env.server.db, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Size() == 0 {
		t.Fatal("backup missing")
	}
}

func TestPasswordValidation(t *testing.T) {
	if _, err := hashPassword("short"); err == nil || !strings.Contains(err.Error(), "10") {
		t.Fatal("short password accepted")
	}
}
