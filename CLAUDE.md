# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

`make help` lists everything. The ones that matter:

```bash
make setup          # install deps, create .env files
make up             # Postgres + MinIO via docker compose
make dev-server     # Go API on :8080, migrates on boot
make dev-web        # Vite on :5173, proxies /api to :8080
make check          # lint + test + build — what CI runs
make generate       # regenerate sqlc AND the API contract (both sides)
```

Single Go test:

```bash
cd apps/server && go test ./internal/auth/ -run TestHashAndVerify -v
```

There is no front-end test runner. Front-end verification is `pnpm --filter @openchat/web exec tsc --noEmit`, `eslint .`, and `vite build`.

Without Docker, point `DATABASE_URL` at any Postgres 18; only attachments need MinIO.

## The contract is the source of truth

`api/openapi.yaml` generates code for **both** sides:

- `apps/server/internal/api/openapi.gen.go` — Chi handlers and models (oapi-codegen)
- `packages/api-client/src/schema.d.ts` — TypeScript types (openapi-typescript)

Editing the contract without running `make generate-api` leaves the two out of sync. CI fails if generated code is not committed.

**Never hand-edit** anything marked `DO NOT EDIT`: `internal/db/sqlc/*`, `internal/api/openapi.gen.go`, `apps/web/src/routeTree.gen.ts`, `packages/api-client/src/schema.d.ts`. Change the source (`.sql` queries, `openapi.yaml`, files in `src/routes/`) and regenerate.

TypeScript is pinned to 5.9.3 repo-wide. `openapi-typescript` needs the JS compiler API, which TS 7 (the native port) does not provide.

## Two design systems, deliberately

This is the single biggest source of bugs in this codebase. The chat UI and the admin panel do **not** share a visual language:

| | chat / auth | admin panel |
| --- | --- | --- |
| tokens | `--n-*` on `:root` | shadcn's `--background`, `--border`, … |
| scope class | `.prism-next`, `.n-auth` | `.shadcn-scope` |
| styles | `next-theme.css` (compiled from prism's Less — keep verbatim so it can be re-synced), `auth.css`, `menu.css` | `shadcn-theme.css`, `shadcn-base.css` |

Three rules keep them apart:

1. **In Tailwind 4, `border` sets width only — the colour comes from elsewhere.** With no default, it falls back to `currentColor`, i.e. the text colour, and renders as a black outline. Both systems supply that default within their own scope.
2. **Radix portals render under `<body>`, outside every scope class.** shadcn components are reached there through `[data-slot]` (which they all carry) *and* `[data-slot] *` for plain elements nested inside them. Forgetting the descendant selector is what produced black frames inside dropdowns.
3. **`shadcn-base.css` must stay in `@layer base`.** Unlayered rules outrank every cascade layer, so an unlayered default border colour beats Tailwind's own `border-*` utilities — components that reserve space with `border-transparent` then draw it as a visible line.

Our `:focus-visible` treatment in `focus.css` is scoped to the prism surfaces on purpose; shadcn components ship their own focus rings and the two stack if it goes global.

`components/shadcn/**` is vendored from [satnaing/shadcn-admin](https://github.com/satnaing/shadcn-admin) (MIT). Keep deviations from upstream commented with the reason, so a future re-sync can tell intent from drift. Known deviations: theme provider proxies our store, greys are zero-chroma, bulk-actions bar is localised and no longer scales on hover.

## State: server vs client

- **TanStack Query** owns anything the server is authoritative for — conversations, messages, the session, admin data. Query keys live in `lib/query-keys.ts` and `features/admin/use-admin.ts`.
- **Zustand** owns what is not on the server: the in-flight streaming reply (`stores/chat-store.ts`, which changes many times a second and would thrash the query cache), plus theme and sidebar state (`stores/ui-store.ts`).

When a stream finishes, `use-send-message.ts` invalidates the message list so Query re-fetches the authoritative version. That handoff is the pattern to follow.

Any query whose key includes a filter (page, search, time window) needs `placeholderData: (previous) => previous`, or the UI empties out on every change.

## Auth

Sessions are **HttpOnly cookies** backed by a `sessions` table. The cookie carries 32 bytes of entropy; only its SHA-256 is stored.

Consequences that shape the code:

- No token is ever returned, stored, or read by the front end. "Am I signed in?" is answered by calling `/auth/session`, which is why the session is a Query and not Zustand state.
- The API client sets `credentials: 'include'` (`lib/api.ts`). Without it the browser silently drops the cookie.
- `features/auth/` holds the seam: an `AuthApi` interface selected on one line in `index.ts`.
- The first account registered becomes the admin, decided inside the INSERT so concurrent registrations cannot both claim it. Admins cannot remove their own access, and the last enabled admin cannot be demoted or disabled.

## Backend conventions

**Routes are mounted by hand** in `internal/api/router.go`, not via `HandlerFromMux`, so each endpoint's protection level is visible in one place. A new endpoint must be placed in a group deliberately — it cannot inherit "public" by being forgotten.

**Ownership is part of the query, not a check after it.** `GetConversation` takes a `user_id`; someone else's row reads as "not found". The admin API answers 404 rather than 403 for the same reason — a 403 confirms the panel exists.

**Migrations are embedded** (`go:embed`) and run on boot, so a deploy needs no separate step. Goose runs each in a transaction, so a failed migration rolls back cleanly. Create one with `make migration name=add_something`.

The chat provider is a two-method interface (`internal/chat`). `Echo` ships by default so the stack runs with no model key; swap it in `cmd/openchat/main.go`.

`POST /conversations/{id}/messages` responds with SSE. The assistant row is written **before** the first token so a reply survives a crash mid-stream. A cancelled context (Stop, closed tab) saves the partial reply as `complete`; only a provider error is `failed`.

## Front-end conventions

**Routing** is file-based under `src/routes/`. Pathless layouts carry the guards: `_authed.tsx` requires a session, `admin.tsx` additionally requires the admin role. Both run in `beforeLoad`, so a signed-out visitor never sees a flash of the shell. Guards use `context.queryClient.ensureQueryData`, so navigating between protected routes costs no extra request.

**Tables** run in manual mode (`manualPagination`, `manualFiltering`) — paging and search belong to the server. `getRowId` must be the account id so a selection survives a re-fetch.

**i18n**: every user-facing string goes through `t()`, including in vendored components — locales are `zh` and `en`. Keys are typed against the locale files (`types/i18next.d.ts`), so a renamed key breaks the build. Vendoring a new shadcn component means translating its strings before using it; upstream ships hardcoded English, and `sidebar-data.ts` had to become a `useSidebarData()` hook because `t` is only available inside a component.

**Motion**: `prefers-reduced-motion` is honoured globally in `globals.css`, but elements marked `data-motion="essential"` opt out and handle it themselves. Loading indicators use this — a frozen progress bar is indistinguishable from a hung page. They swap travel for an opacity pulse rather than stopping.

## Scoped out so far

No model provider catalogue (the ported UI expects one: per-provider keys, model lists, reasoning effort, web-search toggles). No attachments UI, tool calls, or reasoning blocks. No user-deletion endpoint — disabling is the reversible equivalent, and deleting a user would cascade away their conversations.
