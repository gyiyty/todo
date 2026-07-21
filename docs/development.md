# Development Guide

## 1. System overview

The service is designed to fit beside AstrBot on a memory-constrained host. The production artifact is one Go binary containing the compiled Preact application.

```text
Browser / installed PWA
        |
        | HTTPS, session cookie + CSRF
        v
Go HTTP server (chi) ---------- Bearer token ----------> future AstrBot commands
        |
        +---- SQLite (WAL)
        |
        +---- reminder worker -> persistent outbox -> signed Webhook -> AstrBot -> QQ
```

SQLite, the backup directory, and logs are runtime state and must remain outside Git. No Node process runs in production.

## 2. Repository layout

| Path | Ownership |
| --- | --- |
| `cmd/todo/main.go` | CLI commands, process lifecycle, HTTP server startup |
| `internal/app` | Configuration, schema, authentication, task API, reminder workers, backup |
| `internal/webui` | Embedded production frontend filesystem |
| `frontend/src` | Preact application, API client, responsive layout and styles |
| `docs/astrbot-integration.md` | Stable API/Webhook contract for the future AstrBot plugin |
| `deploy` | Nginx and user-level systemd examples |
| `compose.yaml` | Restricted 128 MiB Docker deployment |

## 3. Runtime and configuration

The binary accepts no argument to serve HTTP and supports these operational commands:

```bash
todo admin create <username>          # password is read from stdin
todo admin reset-password <username>  # invalidates all sessions
todo backup [destination]
todo healthcheck
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `TODO_LISTEN_ADDR` | `127.0.0.1:8787` | HTTP listen address |
| `TODO_BASE_URL` | `http://127.0.0.1:8787` | Public origin; HTTPS enables Secure cookies and HSTS |
| `TODO_DATABASE_PATH` | `./data/todo.db` | SQLite file |
| `TODO_BACKUP_DIR` | sibling `backups` directory | Consistent SQLite backups |
| `TODO_TIMEZONE` | `Asia/Shanghai` | Calendar grouping and automatic backup timezone |

The server starts four lightweight workers: reminder scan every 10 seconds, outbox delivery every 10 seconds, cleanup every 6 hours, and an hourly check that creates the daily post-03:20 backup. Backups older than 14 days are removed.

## 4. Authentication and security

- Passwords use Argon2id. Public registration does not exist; the first and only user is created through the CLI.
- Browser login creates a hashed server-side session, an HttpOnly session cookie, and a readable CSRF cookie. Every non-read browser request must echo the CSRF value in `X-CSRF-Token`.
- Five failed logins block the source address for 15 minutes.
- Integration tokens are stored as SHA-256 hashes, shown only once, and limited to `tasks:read` and/or `tasks:write`.
- Admin endpoints for tokens and AstrBot settings reject integration-token principals even if they have task scopes.
- The webhook shared secret must contain at least 24 characters. It is stored in the permission-restricted SQLite database because it is needed for outbound signing.
- The application listens on loopback in host deployments. Nginx/BT Panel terminates TLS; do not expose the application port or SQLite file directly.

## 5. Data model

The authoritative DDL is in `internal/app/db.go`.

| Tables | Purpose |
| --- | --- |
| `users`, `sessions`, `login_attempts` | Single-user identity, sessions, login throttling |
| `lists`, `tasks`, `tags`, `task_tags` | Task organization and many-to-many tags |
| `reminders`, `notifications` | Absolute/relative trigger definitions and in-app history |
| `api_tokens` | Hashed AstrBot/API credentials and scopes |
| `webhook_config` | Singleton outbound AstrBot configuration |
| `outbox` | Durable webhook event, attempts, retry time and terminal status |

IDs use random prefixed strings such as `tsk_`, `rem_`, and `evt_`. Timestamps are UTC RFC3339Nano strings, which also preserve chronological ordering in SQLite. SQLite uses foreign keys, WAL, a five-second busy timeout, `synchronous=NORMAL`, and at most four open connections.

Recurrence is intentionally bounded to an interval plus `day`, `week`, `month`, or `year`. Completing a recurring task closes the current row and creates a new task anchored to the previous due date; tags and shifted reminders are copied. Do not silently change recurrence to completion-time anchoring.

## 6. HTTP API

All application APIs use JSON under `/api/v1`; errors have the shape `{"error":"message"}`.

| Area | Endpoints |
| --- | --- |
| Session | `POST/DELETE /session`, `GET /me` |
| Navigation | `GET /dashboard` |
| Lists | `GET/POST /lists`, `PATCH/DELETE /lists/{id}` |
| Tags | `GET/POST /tags`, `DELETE /tags/{id}` |
| Tasks | `GET/POST /tasks`, `GET/PATCH/DELETE /tasks/{id}` |
| State changes | `POST /tasks/{id}/complete`, `POST /tasks/{id}/reopen` |
| Notifications | `GET /notifications`, `POST /notifications/{id}/read` |
| Admin | `/tokens`, `/integrations/astrbot`, `/integrations/astrbot/deliveries` |
| Health | `GET /health/live`, `GET /health/ready` |

Task list filters include `view=today|upcoming|completed`, `list_id`, `q`, and `priority`; results are capped at 500. Relative reminders store an offset in minutes and are recalculated when a due date changes. Absolute reminders keep their explicit trigger time.

## 7. Reminder and AstrBot flow

When a reminder becomes due, one transaction conditionally marks it sent and creates an in-app notification. If AstrBot integration is enabled, the same transaction creates a stable outbox event. The outbox worker signs `timestamp + "." + raw_json_body` using HMAC-SHA256 and sends these headers:

```text
X-Todo-Event-ID
X-Todo-Timestamp
X-Todo-Signature: sha256=<hex>
```

Only a 2xx response marks delivery complete. Failures use exponential backoff from 30 seconds, capped at 6 hours, and become `dead` after 7 days. Admins can manually requeue dead deliveries. Delivery is at least once, so the AstrBot receiver must deduplicate by event ID. Keep payload and verification details synchronized with `docs/astrbot-integration.md`.

## 8. Frontend

Preact, TypeScript, Vite, and `lucide-preact` build a small responsive PWA. The API wrapper in `frontend/src/api.ts` owns credentials, JSON errors, and CSRF headers. `App.tsx` owns application state and the task/settings workflows; `styles.css` defines breakpoints:

- Desktop: navigation sidebar, task list, and task editor.
- Tablet: drawer navigation with list and editor panes.
- Mobile: one content column, bottom navigation, floating create control, full-screen editor.

The service worker caches only GET application assets; offline mutations and conflict resolution are deliberately out of scope. Generated `public/icon.svg`, Vite output, and TypeScript build info are ignored.

## 9. Build, test, and deployment

Local prerequisites are Go 1.24+, GCC/CGO, Node 20+ and npm. The container build uses Node 22. Node 18 currently builds with a dependency warning but is not the supported development baseline.

```bash
make build GO=go       # builds frontend, then an 8-10 MiB stripped binary
make test GO=go        # builds frontend and runs Go tests
go vet ./...
```

Backend tests in `internal/app/server_test.go` use temporary SQLite databases and real `httptest` servers. They cover login/CSRF, recurrence, token scope isolation, signed webhook delivery, backup, and password policy. UI changes should additionally be checked at approximately 1440x900 and 390x844 viewports.

For production, `compose.yaml` binds only `127.0.0.1:8787`, drops Linux capabilities, uses a read-only root filesystem, and limits the container to 128 MiB and half a CPU. Set `TODO_BASE_URL` to the final HTTPS origin before login. Host deployment can use the units in `deploy/systemd`; the timer is an extra backup trigger in addition to the built-in daily backup.

## 10. Current boundaries

- Single user only; no registration, sharing, teams, browser push, email, or native mobile application.
- PWA is online-first with cached application assets, not an offline synchronization engine.
- Webhook is the reminder transport; the AstrBot plugin itself is not part of this repository yet.
- Schema setup is idempotent DDL, not a general versioned migration system. Add migrations before any incompatible schema evolution.
- Runtime data from a local/manual test instance is not reproducible project seed data and must never be committed.
