.PHONY: help web release up dev pull deploy open down test-cli logs ps build test test-api test-sdk test-web test-e2e lint-openapi sqlc backup restore seed mcp-stdio recovery-key connect workspaces set-plan

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

up: ## Pull the released images, start staqly and open the dashboard signed in (http://localhost:8080)
	@docker compose pull --quiet api web || { echo "Released images not available, building locally"; docker compose build; }
	docker compose up -d --wait
	@$(MAKE) --no-print-directory open

dev: ## Build the images from this checkout, start staqly and open the dashboard
	docker compose up -d --build --wait
	@$(MAKE) --no-print-directory open

pull: ## Download the released images (STAQLY_VERSION=prod-<timestamp> for a specific deploy)
	docker compose pull api web

deploy: ## Tag origin/main as prod-<timestamp>; CI publishes the images to ghcr.io
	bin/deploy.sh

release: ## Tag origin/main as VERSION (make release VERSION=v0.2.0); CI publishes the staqly binaries
	bin/release.sh $(VERSION)

open: ## Open the dashboard in this computer's browser with a one-time sign-in link
	@link=$$(docker exec staqly-api server connect-link 2>/dev/null) && echo "Sign-in link (one-time, 5 min): $$link" && \
	  ( command -v open >/dev/null && open "$$link" || command -v xdg-open >/dev/null && xdg-open "$$link" || true )

down: ## Stop containers (data volume is kept)
	docker compose down

logs: ## Follow logs
	docker compose logs -f

ps: ## Show container status
	docker compose ps

build: ## Build the images from this checkout
	docker compose build

web: ## Build the dashboard (static export) into apps/api/web for the Go binaries
	cd apps/web && npm ci && npm run build
	find apps/api/web -mindepth 1 ! -name .gitkeep -delete
	cp -R apps/web/out/. apps/api/web/

test: test-api test-cli test-sdk test-web ## Run unit/integration tests

test-api: ## Go tests (with race detector)
	cd apps/api && go test -race ./...

test-cli: ## staqly CLI tests
	cd apps/cli && go test -race ./...

test-sdk: ## TypeScript + Python SDK tests
	cd packages/sdk-typescript && npm ci && npm test
	cd packages/sdk-python && uv run pytest -q

test-web: ## Dashboard tests
	cd apps/web && npm ci && npm test

test-e2e: ## Full Docker end-to-end test in an isolated compose project
	./scripts/e2e.sh

REDOCLY_VERSION ?= 2.63.0

lint-openapi: ## Validate the OpenAPI document
	npx -y @redocly/cli@$(REDOCLY_VERSION) lint apps/api/openapi.yaml --skip-rule operation-operationId --skip-rule operation-4xx-response --skip-rule no-server-example.com --skip-rule tag-description --skip-rule no-unused-components

sqlc: ## Regenerate typed queries (requires sqlc)
	cd apps/api && sqlc generate

backup: ## Consistent backup into ./backups/<timestamp>
	./scripts/backup.sh

restore: ## Restore: make restore BACKUP=backups/<timestamp>
	./scripts/restore.sh $(BACKUP)

seed: ## Load demo data into the running instance
	./scripts/seed.sh

mcp-stdio: ## Run the MCP STDIO server in the API container
	docker exec -i staqly-api mcp-stdio

recovery-key: ## Print a new recovery key: make recovery-key [WS=ws_...]
	docker exec staqly-api server recovery-key $(WS)

connect: ## Print a one-time sign-in link for another browser: make connect [WS={WORKSPACE_ID}]
	@docker exec staqly-api server connect-link $(WS)

workspaces: ## List workspaces and their plans (cloud operator)
	docker exec staqly-api server workspaces

set-plan: ## Change a plan (no payments yet): make set-plan WS=ws_... PLAN=indie
	docker exec staqly-api server set-plan $(WS) $(PLAN)

