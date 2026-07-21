package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	ListenAddr     string
	DatabasePath   string
	BackupDir      string
	BaseURL        string
	Timezone       *time.Location
	SessionTTL     time.Duration
	WebhookTimeout time.Duration
}

func LoadConfig() (Config, error) {
	tzName := env("TODO_TIMEZONE", "Asia/Shanghai")
	tz, err := time.LoadLocation(tzName)
	if err != nil {
		return Config{}, fmt.Errorf("load timezone %q: %w", tzName, err)
	}

	dbPath := env("TODO_DATABASE_PATH", "./data/todo.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return Config{}, fmt.Errorf("create data directory: %w", err)
	}

	return Config{
		ListenAddr:     env("TODO_LISTEN_ADDR", "127.0.0.1:8787"),
		DatabasePath:   dbPath,
		BackupDir:      env("TODO_BACKUP_DIR", filepath.Join(filepath.Dir(dbPath), "backups")),
		BaseURL:        strings.TrimRight(env("TODO_BASE_URL", "http://127.0.0.1:8787"), "/"),
		Timezone:       tz,
		SessionTTL:     30 * 24 * time.Hour,
		WebhookTimeout: 10 * time.Second,
	}, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
