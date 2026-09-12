# OpenChat

A streaming chat application: React 19 SPA on a Go API, with the HTTP contract
generated from a single OpenAPI document.

## Stack

| Layer | Choice |
| --- | --- |
| Frontend | React 19 + TypeScript + Vite |
| Routing | TanStack Router (file-based) |
| Server state | TanStack Query |
| Local/stream state | Zustand |
| UI primitives | Radix UI |
| Styling | Tailwind CSS 4 + CSS variables |
| Animation | Motion |
| Icons | Lucide |
| Markdown | Streamdown |
| i18n | i18next (en, zh) |
| Backend | Go + net/http + Chi |
| Database | PostgreSQL 18 |
| Data access | pgx + sqlc |
| Migrations | **Goose** (embedded in the binary) |
| API contract | OpenAPI 3.1 → Go server + TypeScript client |
| Files | S3-compatible blob storage (MinIO locally) |
| Redis | not used |
| Desktop | Tauri 2 (later; the web build is already a static SPA) |

## Quick start

```bash
make setup     # install deps, create .env files
make up        # Postgres + MinIO via docker compose
make dev-server  # terminal 1 — API on :8080, migrates on boot
make dev-web     # terminal 2 — Vite on :5173
```

`http://localhost:5173` proxies `/api` to the Go server, so the browser stays
same-origin and SSE works without CORS preflights.

Without Docker, point `DATABASE_URL` at any Postgres 18 instance; attachments
are the only feature that needs MinIO.

## Layout

```
api/openapi.yaml          the contract — edit this first
apps/server/              Go API
  cmd/openchat/           entrypoint (serve | migrate | rollback)
  internal/api/           generated ServerInterface + handlers
  internal/chat/          model-provider seam (echo stub ships by default)
  internal/db/            pgx pool, goose migrations, sqlc queries
  internal/httpx/         JSON/SSE/error/logging helpers
  internal/storage/       presigned S3 uploads
apps/web/                 React SPA
  src/routes/             file-based routes → routeTree.gen.ts
  src/hooks/              TanStack Query hooks
  src/stores/             Zustand (UI prefs, live stream)
  src/components/         ui/ primitives, layout/, chat/
packages/api-client/      typed client generated from the contract
```

## Working on it

### Changing the API

`api/openapi.yaml` is the source of truth. After editing it:

```bash
make generate-api
```

That regenerates `apps/server/internal/api/openapi.gen.go` (Chi handlers and
models) and `packages/api-client/src/schema.d.ts` (TypeScript types), so a
contract change breaks the compile on both sides rather than at runtime.

### Changing the database

```bash
make migration name=add_something   # new goose file
# edit apps/server/internal/db/migrations/*_add_something.sql
make migrate                        # apply
make generate-sql                   # regenerate sqlc after query/schema edits
```

Migrations are embedded with `go:embed` and run automatically on server boot,
so a deploy never needs a separate migration step.

### Streaming

`POST /conversations/{id}/messages` responds with `text/event-stream`; each
frame is a JSON `StreamEvent` (`start` → `delta`… → `done` | `error`).

- The server writes the assistant row **before** the first token, so a reply
  survives a crash mid-stream.
- The client keeps live tokens in Zustand and the persisted history in TanStack
  Query — server state and ephemeral state never share a cache.
- Pressing Stop aborts the fetch; the partial reply is saved as `complete`.
  Only a genuine provider error is saved as `failed`.

### Wiring a real model

`internal/chat.Provider` is a two-method interface. Implement it, then swap the
`chat.Echo{}` line in `cmd/openchat/main.go`. Nothing in the HTTP layer changes.

## Commands

`make help` lists everything. The common ones:

| Command | What it does |
| --- | --- |
| `make check` | lint + test + build (what CI runs) |
| `make generate` | regenerate sqlc and the API contract |
| `make reset` | wipe local Postgres/MinIO volumes and restart |
