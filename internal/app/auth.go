package app

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	sessionCookie = "todo_session"
	csrfCookie    = "todo_csrf"
)

type principal struct {
	UserID string
	Scopes map[string]bool
	Token  bool
}

type contextKey string

const principalKey contextKey = "principal"

func hashPassword(password string) (string, error) {
	if len(password) < 10 {
		return "", errors.New("password must contain at least 10 characters")
	}
	salt := make([]byte, 16)
	copy(salt, []byte(randomToken(16)))
	hash := argon2.IDKey([]byte(password), salt, 2, 32*1024, 2, 32)
	return fmt.Sprintf("argon2id$v=19$m=32768,t=2,p=2$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[3])
	expected, err2 := base64.RawStdEncoding.DecodeString(parts[4])
	if err1 != nil || err2 != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 2, 32*1024, 2, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func CreateAdmin(db *sql.DB, username, password string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 40 {
		return errors.New("username must contain 3 to 40 characters")
	}
	encoded, err := hashPassword(password)
	if err != nil {
		return err
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("an administrator already exists")
	}
	_, err = db.Exec("INSERT INTO users(id, username, password_hash, created_at) VALUES(?, ?, ?, ?)", newID("usr"), username, encoded, nowString())
	return err
}

func ResetPassword(db *sql.DB, username, password string) error {
	encoded, err := hashPassword(password)
	if err != nil {
		return err
	}
	result, err := db.Exec("UPDATE users SET password_hash = ? WHERE username = ?", encoded, strings.TrimSpace(username))
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.New("user not found")
	}
	_, _ = db.Exec("DELETE FROM sessions")
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	key, _, _ := net.SplitHostPort(r.RemoteAddr)
	if key == "" {
		key = r.RemoteAddr
	}
	var blocked sql.NullString
	_ = s.db.QueryRow("SELECT blocked_until FROM login_attempts WHERE key = ?", key).Scan(&blocked)
	if blocked.Valid {
		until, _ := time.Parse(timeFormat, blocked.String)
		if time.Now().Before(until) {
			writeError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
			return
		}
	}

	var userID, username, encoded string
	err := s.db.QueryRow("SELECT id, username, password_hash FROM users WHERE username = ?", strings.TrimSpace(input.Username)).Scan(&userID, &username, &encoded)
	if err != nil || !verifyPassword(encoded, input.Password) {
		s.recordLoginFailure(key)
		time.Sleep(250 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	sessionToken, csrf := randomToken(32), randomToken(24)
	expires := time.Now().UTC().Add(s.cfg.SessionTTL)
	_, err = s.db.Exec("INSERT INTO sessions(token_hash, user_id, csrf_token, expires_at, created_at) VALUES(?, ?, ?, ?, ?)",
		tokenHash(sessionToken), userID, csrf, expires.Format(timeFormat), nowString())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	_, _ = s.db.Exec("DELETE FROM login_attempts WHERE key = ?", key)
	secure := strings.HasPrefix(s.cfg.BaseURL, "https://")
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: sessionToken, Path: "/", Expires: expires, MaxAge: int(s.cfg.SessionTTL.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: csrf, Path: "/", Expires: expires, MaxAge: int(s.cfg.SessionTTL.Seconds()), Secure: secure, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"id": userID, "username": username}, "csrf_token": csrf})
}

func (s *Server) recordLoginFailure(key string) {
	now := time.Now().UTC()
	_, _ = s.db.Exec(`INSERT INTO login_attempts(key, failures, updated_at) VALUES(?, 1, ?)
ON CONFLICT(key) DO UPDATE SET failures = CASE WHEN blocked_until < ? THEN 1 ELSE failures + 1 END, updated_at = ?`, key, now.Format(timeFormat), now.Format(timeFormat), now.Format(timeFormat))
	var failures int
	_ = s.db.QueryRow("SELECT failures FROM login_attempts WHERE key = ?", key).Scan(&failures)
	if failures >= 5 {
		_, _ = s.db.Exec("UPDATE login_attempts SET blocked_until = ? WHERE key = ?", now.Add(15*time.Minute).Format(timeFormat), key)
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_, _ = s.db.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true})
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p principal
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			var scopes string
			err := s.db.QueryRow(`SELECT scopes FROM api_tokens WHERE token_hash = ? AND (expires_at IS NULL OR expires_at > ?)`, tokenHash(strings.TrimPrefix(auth, "Bearer ")), nowString()).Scan(&scopes)
			if err == nil {
				p = principal{Token: true, Scopes: parseScopes(scopes)}
				_, _ = s.db.Exec("UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ?", nowString(), tokenHash(strings.TrimPrefix(auth, "Bearer ")))
			}
		} else if cookie, err := r.Cookie(sessionCookie); err == nil {
			var csrf, expiry string
			err = s.db.QueryRow("SELECT user_id, csrf_token, expires_at FROM sessions WHERE token_hash = ?", tokenHash(cookie.Value)).Scan(&p.UserID, &csrf, &expiry)
			if err == nil && expiry > nowString() {
				p.Scopes = map[string]bool{"*": true}
				if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
					csrfCookieValue, cookieErr := r.Cookie(csrfCookie)
					if cookieErr != nil || csrfCookieValue.Value != csrf || r.Header.Get("X-CSRF-Token") != csrf {
						writeError(w, http.StatusForbidden, "invalid CSRF token")
						return
					}
				}
			}
		}
		if len(p.Scopes) == 0 {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}

func parseScopes(value string) map[string]bool {
	result := map[string]bool{}
	for _, scope := range strings.Split(value, ",") {
		result[strings.TrimSpace(scope)] = true
	}
	return result
}

func requireScope(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := r.Context().Value(principalKey).(principal)
		if !p.Scopes["*"] && !p.Scopes[scope] {
			writeError(w, http.StatusForbidden, "missing scope: "+scope)
			return
		}
		next(w, r)
	}
}
