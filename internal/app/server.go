package app

import (
	"database/sql"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"todo/internal/webui"
)

type Server struct {
	cfg    Config
	db     *sql.DB
	logger *slog.Logger
}

func NewServer(cfg Config, db *sql.DB, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, db: db, logger: logger}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(s.securityHeaders)

	r.Get("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/health/ready", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.db.Ping(); err != nil {
			writeError(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Post("/session", s.login)
		api.Group(func(private chi.Router) {
			private.Use(s.authenticate)
			private.Delete("/session", s.logout)
			private.Get("/me", s.me)
			private.Get("/dashboard", requireScope("tasks:read", s.dashboard))
			private.Get("/lists", requireScope("tasks:read", s.listLists))
			private.Post("/lists", requireScope("tasks:write", s.createList))
			private.Patch("/lists/{id}", requireScope("tasks:write", s.updateList))
			private.Delete("/lists/{id}", requireScope("tasks:write", s.deleteList))
			private.Get("/tags", requireScope("tasks:read", s.listTags))
			private.Post("/tags", requireScope("tasks:write", s.createTag))
			private.Delete("/tags/{id}", requireScope("tasks:write", s.deleteTag))
			private.Get("/tasks", requireScope("tasks:read", s.listTasks))
			private.Post("/tasks", requireScope("tasks:write", s.createTask))
			private.Get("/tasks/{id}", requireScope("tasks:read", s.getTask))
			private.Patch("/tasks/{id}", requireScope("tasks:write", s.updateTask))
			private.Delete("/tasks/{id}", requireScope("tasks:write", s.deleteTask))
			private.Post("/tasks/{id}/complete", requireScope("tasks:write", s.completeTask))
			private.Post("/tasks/{id}/reopen", requireScope("tasks:write", s.reopenTask))
			private.Get("/notifications", requireScope("tasks:read", s.listNotifications))
			private.Post("/notifications/{id}/read", requireScope("tasks:write", s.readNotification))
			private.Get("/tokens", requireAdmin(s.listTokens))
			private.Post("/tokens", requireAdmin(s.createToken))
			private.Delete("/tokens/{id}", requireAdmin(s.deleteToken))
			private.Get("/integrations/astrbot", requireAdmin(s.getAstrBotConfig))
			private.Put("/integrations/astrbot", requireAdmin(s.updateAstrBotConfig))
			private.Get("/integrations/astrbot/deliveries", requireAdmin(s.listDeliveries))
			private.Post("/integrations/astrbot/deliveries/{id}/retry", requireAdmin(s.retryDelivery))
		})
	})
	r.Handle("/*", s.frontendHandler())
	r.Handle("/", s.frontendHandler())
	return r
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		if strings.HasPrefix(s.cfg.BaseURL, "https://") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) frontendHandler() http.Handler {
	dist, err := fs.Sub(webui.Files, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "." || name == "" {
			name = "index.html"
		}
		data, readErr := fs.ReadFile(dist, name)
		if readErr != nil {
			data, readErr = fs.ReadFile(dist, "index.html")
			name = "index.html"
		}
		if readErr != nil {
			http.NotFound(w, r)
			return
		}
		if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if name == "index.html" || name == "sw.js" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		_, _ = w.Write(data)
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p, _ := r.Context().Value(principalKey).(principal)
	if p.Token {
		writeJSON(w, http.StatusOK, map[string]any{"integration": true, "scopes": p.Scopes})
		return
	}
	var username string
	if err := s.db.QueryRow("SELECT username FROM users WHERE id = ?", p.UserID).Scan(&username); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": p.UserID, "username": username, "timezone": s.cfg.Timezone.String()})
}
