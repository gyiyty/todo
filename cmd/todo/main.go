package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"todo/internal/app"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := app.LoadConfig()
	if err != nil {
		fatal(logger, err)
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 3 * time.Second}
		address := cfg.ListenAddr
		if strings.HasPrefix(address, "0.0.0.0:") {
			address = "127.0.0.1:" + strings.TrimPrefix(address, "0.0.0.0:")
		}
		response, requestErr := client.Get("http://" + address + "/health/ready")
		if requestErr != nil || response.StatusCode != http.StatusOK {
			fatal(logger, fmt.Errorf("service is not ready"))
		}
		response.Body.Close()
		return
	}
	db, err := app.OpenDatabase(cfg.DatabasePath)
	if err != nil {
		fatal(logger, err)
	}
	defer db.Close()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "admin":
			handleAdmin(db, logger)
		case "backup":
			destination := filepath.Join(cfg.BackupDir, "todo-"+time.Now().Format("20060102-150405")+".db")
			if len(os.Args) > 2 {
				destination = os.Args[2]
			}
			if err := app.Backup(db, destination); err != nil {
				fatal(logger, err)
			}
			if err := app.PruneBackups(cfg.BackupDir, 14*24*time.Hour); err != nil {
				logger.Warn("could not prune old backups", "error", err)
			}
			fmt.Println(destination)
		default:
			fatal(logger, fmt.Errorf("unknown command %q", os.Args[1]))
		}
		return
	}

	server := app.NewServer(cfg, db, logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server.StartWorkers(ctx)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		logger.Info("todo service listening", "address", cfg.ListenAddr, "base_url", cfg.BaseURL)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatal(logger, err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func handleAdmin(db *sql.DB, logger *slog.Logger) {
	if len(os.Args) < 4 || (os.Args[2] != "create" && os.Args[2] != "reset-password") {
		fatal(logger, fmt.Errorf("usage: todo admin <create|reset-password> <username>, with password on stdin"))
	}
	password := readPassword()
	var err error
	if os.Args[2] == "create" {
		err = app.CreateAdmin(db, os.Args[3], password)
	} else {
		err = app.ResetPassword(db, os.Args[3], password)
	}
	if err != nil {
		fatal(logger, err)
	}
	fmt.Println("administrator updated")
}

func readPassword() string {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func fatal(logger *slog.Logger, err error) {
	logger.Error("fatal", "error", err)
	os.Exit(1)
}
