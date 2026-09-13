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
migration: ## Create a migration: make migration name=add_users
	@test -n "$(name)" || (echo "usage: make migration name=<snake_case_name>" && exit 1)
	@printf -- "-- +goose Up\n-- +goose StatementBegin\n\n-- +goose StatementEnd\n\n-- +goose Down\n-- +goose StatementBegin\n\n-- +goose StatementEnd\n" \
		> $(SERVER)/internal/db/migrations/$$(date -u +%Y%m%d%H%M%S)_$(name).sql
	@echo "created $(SERVER)/internal/db/migrations/*_$(name).sql"

## ---------------------------------------------------------- code  gen ------

.PHONY: generate
generate: generate-sql generate-api ## Regenerate everything

.PHONY: generate-sql
generate-sql: ## Regenerate sqlc code from internal/db/queries
	cd $(SERVER) && go tool sqlc generate

.PHONY: generate-api
generate-api: ## Regenerate the Go server + TypeScript client from api/openapi.yaml
	cd $(SERVER) && go tool oapi-codegen -config oapi-codegen.yaml ../../api/openapi.yaml
	pnpm --filter @openchat/api-client generate

## ----------------------------------------------------------- verify --------

.PHONY: check
check: lint test build ## Everything CI runs

.PHONY: lint
lint: ## Vet Go and lint the web app
	cd $(SERVER) && go vet ./...
	pnpm --filter @openchat/web lint

.PHONY: test
test: ## Run Go tests
	cd $(SERVER) && go test ./...

.PHONY: build
build: ## Build the server binary and the web bundle
	cd $(SERVER) && go build -o ../../bin/openchat ./cmd/openchat
	pnpm --filter @openchat/web build

.PHONY: typecheck
typecheck: ## Typecheck every TS package
	pnpm -r typecheck
