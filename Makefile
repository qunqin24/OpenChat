SHELL := /bin/bash
SERVER := apps/server

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ---------------------------------------------------------------- setup ----

.PHONY: setup
setup: ## Install JS deps, Go modules, and create .env files
	pnpm install
	cd $(SERVER) && go mod download
	[ -f .env ] || cp .env.example .env
	[ -f apps/web/.env ] || cp apps/web/.env.example apps/web/.env

.PHONY: up
up: ## Start Postgres + MinIO
	docker compose up -d

.PHONY: down
down: ## Stop the local infrastructure
	docker compose down

.PHONY: reset
reset: ## Destroy local data and start fresh
	docker compose down -v
	docker compose up -d

## ------------------------------------------------------------------ dev ----

.PHONY: ser
ser: ## Run the Go API on :8080 (migrates on boot)
	cd $(SERVER) && go run ./cmd/openchat

.PHONY: web
web: ## Run the Vite dev server on :5173
	pnpm --filter @openchat/web dev

# The old names, kept working so existing notes and muscle memory do not break.
# Undocumented on purpose: `make help` should list one name per job, or the
# short names are no easier to find than the long ones were.
.PHONY: dev-server dev-web
dev-server: ser
dev-web: web

## ------------------------------------------------------------- database ----

.PHONY: migrate
migrate: ## Apply pending migrations
	cd $(SERVER) && go run ./cmd/openchat migrate

.PHONY: rollback
rollback: ## Revert the most recent migration
	cd $(SERVER) && go run ./cmd/openchat rollback

.PHONY: migration
# The version is the current UTC time, or one past the newest migration if that
# is later: goose refuses to start on a database that has run a higher version
# than a migration it has not, so a new one must always sort last.
migration: ## Create a migration: make migration name=add_users
	@test -n "$(name)" || (echo "usage: make migration name=<snake_case_name>" && exit 1)
	@dir=$(SERVER)/internal/db/migrations; \
		latest=$$(ls $$dir | sed -n 's/^\([0-9]\{14\}\)_.*\.sql$$/\1/p' | sort | tail -1); \
		now=$$(date -u +%Y%m%d%H%M%S); \
		version=$$(( now > $${latest:-0} ? now : $${latest:-0} + 1 )); \
		printf -- "-- +goose Up\n-- +goose StatementBegin\n\n-- +goose StatementEnd\n\n-- +goose Down\n-- +goose StatementBegin\n\n-- +goose StatementEnd\n" \
			> $$dir/$${version}_$(name).sql; \
		echo "created $$dir/$${version}_$(name).sql"

## ---------------------------------------------------------- code  gen ------

.PHONY: generate
generate: generate-sql generate-api generate-genui ## Regenerate everything

.PHONY: generate-sql
generate-sql: ## Regenerate sqlc code from internal/db/queries
	cd $(SERVER) && go tool sqlc generate

.PHONY: generate-api
generate-api: ## Regenerate the Go server + TypeScript client from api/openapi.yaml
	cd $(SERVER) && go tool oapi-codegen -config oapi-codegen.yaml ../../api/openapi.yaml
	pnpm --filter @openchat/api-client generate

.PHONY: generate-genui
generate-genui: ## Copy the generative UI catalogue (api/genui-catalog.json) to server and client
	node scripts/sync-genui-catalog.mjs

## ----------------------------------------------------------- verify --------

.PHONY: check
check: lint test build ## Everything CI runs

.PHONY: lint
lint: ## Vet Go and lint the web app, the mobile app and the shared chat logic
	cd $(SERVER) && go vet ./...
	pnpm --filter @openchat/web lint
	pnpm --filter @openchat/chat-core lint
	pnpm --filter @openchat/mobile lint

# The database-backed Go tests run on the Postgres server and account that
# DATABASE_URL in .env points at, under the name openchat_test (override by
# setting TEST_DATABASE_URL): every test package makes and migrates a database
# of its own named openchat_test_<package>, so packages can run in parallel and
# nothing is named like the dev database. Without a URL those tests skip.
TEST_DATABASE_URL ?= $(shell sed -n 's/^DATABASE_URL=//p' .env 2>/dev/null | sed -E 's,/[^/?]+(\?|$$),/openchat_test\1,')
export TEST_DATABASE_URL

.PHONY: test
test: test-server test-web ## Run the Go and web tests

.PHONY: test-server
test-server: ## Run Go tests (database-backed ones need TEST_DATABASE_URL, see above)
	@[ -n "$(TEST_DATABASE_URL)" ] || echo "note: no TEST_DATABASE_URL (or DATABASE_URL in .env): database-backed tests will be skipped"
	cd $(SERVER) && go test ./...

.PHONY: test-web
test-web: ## Run the web unit tests (vitest), and the shared packages'
	pnpm --filter @openchat/chat-core test
	pnpm --filter @openchat/api-client test
	pnpm --filter @openchat/web test

.PHONY: build
build: build-web ## Build the single binary: server with the web app embedded (bin/openchat)
	cd $(SERVER) && go build -o ../../bin/openchat ./cmd/openchat

.PHONY: build-web
build-web: ## Build the web bundle (precompressed) and copy it into the server package for embedding
	pnpm --filter @openchat/web build
	node scripts/embed-web.mjs

.PHONY: typecheck
typecheck: ## Typecheck every TS package
	pnpm -r typecheck
