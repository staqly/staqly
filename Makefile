.PHONY: help dashboard release up dev site cloud dev-down stripe-secret pull open down logs ps build test check test-e2e lint-openapi gen backup restore seed mcp-stdio recovery-key connect workspaces set-plan

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

up: ## compose.yml alone with the released image (contributors; users run the image with docker run, see the docs), opens the dashboard signed in
	@docker compose pull --quiet staqly || { echo "Released image not available, building locally"; docker compose build; }
	docker compose up -d --wait
	@$(MAKE) --no-print-directory open

DEV := docker compose -f compose.yml -f compose.dev.yml

dev: ## Build from this checkout with the dev stack (debug logs, mailpit :8025, Stripe CLI with COMPOSE_PROFILES=stripe)
	$(DEV) up -d --build --wait
	@$(MAKE) --no-print-directory open

pull: ## Download the released image (STAQLY_VERSION=v0.2.0 for a specific release)
	docker compose pull staqly


site: ## Landing page (http://localhost:8070) and docs (http://localhost:8090) from this checkout, linked to each other and the dev dashboard; Ctrl+C stops them
	NEXT_PUBLIC_APP_URL=http://localhost:8080 NEXT_PUBLIC_DOCS_URL=http://localhost:8090 NEXT_PUBLIC_WEB_URL=http://localhost:8070 \
		pnpm turbo run dev --filter=@staqly/web --filter=@staqly/docs

cloud: ## Production cloud mode: the pre-built image behind Caddy with automatic HTTPS (STAQLY_DOMAIN in .env)
	docker compose -f compose.yml -f compose.cloud.yml pull --quiet
	docker compose -f compose.yml -f compose.cloud.yml up -d --wait

dev-down: ## Stop the dev stack (data volume kept)
	$(DEV) --profile stripe down

stripe-secret: ## Print the webhook signing secret of the Stripe CLI (put it in .env as STRIPE_WEBHOOK_SECRET)
	@$(DEV) run --rm --no-deps stripe listen --print-secret

release: ## Tag origin/main as VERSION (make release VERSION=v0.2.0); CI publishes the binaries and images
	bin/release.sh $(VERSION)

open: ## Open the dashboard in this computer's browser with a one-time sign-in link
	@link=$$(docker exec staqly server connect-link 2>/dev/null) && echo "Sign-in link (one-time, 5 min): $$link" && \
	  if command -v open >/dev/null; then open "$$link"; elif command -v xdg-open >/dev/null; then xdg-open "$$link"; fi; true

down: ## Stop containers (data volume is kept)
	docker compose down

logs: ## Follow logs
	docker compose logs -f

ps: ## Show container status
	docker compose ps

build: ## Build the image from this checkout
	docker compose build

dashboard: ## Build the dashboard (static export) into apps/api/web for the Go binaries
	pnpm install --frozen-lockfile
	pnpm turbo run build --filter=@staqly/platform
	find apps/api/web -mindepth 1 ! -name .gitkeep -delete
	cp -R apps/platform/out/. apps/api/web/

check: ## Formatting, lint, types and unit tests of every package (turbo)
	pnpm turbo run format:check lint typecheck test

test: ## Unit and integration tests of every package (turbo)
	pnpm turbo run test

test-%: ## Tests of one package: test-api, test-cli, test-dashboard, test-sdk, test-sdk-python
	pnpm turbo run test --filter=@staqly/$*

test-e2e: ## Full Docker end-to-end test (compose.e2e.yml + e2e/)
	pnpm turbo run test:e2e

lint-openapi: ## Validate the OpenAPI document
	pnpm --filter @staqly/api run lint:openapi

gen: ## Regenerate sqlc queries, dashboard API types and the TypeScript SDK schema
	pnpm turbo run gen

backup: ## Back up the Compose install (container staqly, volume staqly_staqly-data) into ./backups/<timestamp>
	./scripts/backup.sh

restore: ## Restore the Compose install: make restore BACKUP=backups/<timestamp>
	./scripts/restore.sh $(BACKUP)

seed: ## Load demo data into the running instance
	./scripts/seed.sh

mcp-stdio: ## Run the MCP STDIO server in the staqly container
	docker exec -i staqly mcp-stdio

recovery-key: ## Print a new recovery key: make recovery-key [WS=ws_...]
	docker exec staqly server recovery-key $(WS)

connect: ## Print a one-time sign-in link for another browser: make connect [WS={WORKSPACE_ID}]
	@docker exec staqly server connect-link $(WS)

workspaces: ## List workspaces and their plans (cloud operator)
	docker exec staqly server workspaces

set-plan: ## Change a workspace's plan by hand (cloud operator): make set-plan WS=ws_... PLAN=pro
	docker exec staqly server set-plan $(WS) $(PLAN)

