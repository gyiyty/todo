# Agent Guide

This repository is a single-user, low-memory task manager intended for a 1.6 GiB cloud server. Read `docs/development.md` before changing behavior and `docs/astrbot-integration.md` before changing integration contracts.

## Non-negotiable constraints

- Keep the production topology to one Go process plus SQLite. Do not introduce a required database server, Redis, or a Node runtime.
- Keep the JSON API under `/api/v1`. Existing fields and webhook signatures are compatibility surfaces for the future AstrBot plugin.
- Store timestamps as UTC RFC3339 values. Convert them only at UI boundaries; the configured display timezone defaults to `Asia/Shanghai`.
- Preserve at-least-once webhook delivery. Every reminder event needs a stable `event_id`, a persisted outbox row, HMAC-SHA256 signing, retry, and receiver-side deduplication.
- Browser mutations require the session cookie and CSRF header. API tokens must remain scoped and must never access admin settings.
- Keep desktop, tablet, and mobile layouts usable. Mobile uses a single-column task flow; desktop uses list and detail panes.
- Never commit `.env`, `data/`, SQLite files, logs, screenshots, credentials, API tokens, generated frontend output, `node_modules`, or binaries.

## Required checks

Run these before handing off a change:

```bash
cd frontend && npm ci --registry=https://registry.npmmirror.com && npm run build
cd ..
go test ./...
go vet ./...
```

On this server, Go is installed at `/home/yunyyyy/.cache/go-toolchain/bin/go`; `make test GO=/home/yunyyyy/.cache/go-toolchain/bin/go` is the equivalent full check. Add or extend tests in `internal/app/server_test.go` for authentication, persistence, recurrence, reminders, tokens, or webhook behavior.

## Editing notes

- Backend routes are registered in `internal/app/server.go`; keep HTTP handlers thin and put shared persistence behavior near the owning module.
- The schema currently lives in `internal/app/db.go`. New additive objects may use idempotent DDL; destructive or data-transforming changes require a versioned migration mechanism first.
- Frontend source lives in `frontend/src`. `npm run build` writes ignored assets to `internal/webui/dist`, which are embedded by `internal/webui/webui.go`.
- Use Lucide icons for controls. Keep the quiet operational visual style and avoid card nesting or desktop-only interactions.
- Update `docs/development.md` whenever architecture, configuration, schema ownership, commands, or public interfaces change.
