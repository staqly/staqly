.PHONY: help dashboard release up dev dev-tunnel site dev-down stripe-secret pull open down logs ps build test check test-e2e lint-openapi gen backup restore seed mcp-stdio recovery-key token connect workspaces set-plan

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

# The dev stack on public https addresses through a named Cloudflare tunnel, for platforms that call back over
# https and download media themselves (Instagram). TUNNEL is a cloudflared config whose ingress serves
# localhost:8080 (the dashboard) and localhost:9000 (MaxIOFS) on two hostnames; media links are signed for the
# second. The dashboard is public meanwhile, so the dev sign-in (readable email codes) is off.
TUNNEL_FILE = $(shell echo $(TUNNEL))
TUNNEL_LOG := $(or $(TMPDIR),/tmp/)staqly-tunnel.log

dev-tunnel: ## Dev stack behind a named Cloudflare tunnel (Instagram tests): make dev-tunnel TUNNEL=~/.cloudflared/<name>.yml
	@test -n "$(TUNNEL)" || { echo "Usage: make dev-tunnel TUNNEL=~/.cloudflared/<name>.yml (hostnames for localhost:8080 and localhost:9000)"; exit 1; }
	@app=$$(awk '/hostname:/{h=$$NF} /service:.*:8080/{print h; exit}' $(TUNNEL_FILE)); \
	media=$$(awk '/hostname:/{h=$$NF} /service:.*:9000/{print h; exit}' $(TUNNEL_FILE)); \
	test -n "$$app" -a -n "$$media" || { echo "$(TUNNEL) needs one hostname for localhost:8080 and one for localhost:9000"; exit 1; }; \
	pkill -f "cloudflared tunnel --no-autoupdate --config $(TUNNEL_FILE)" || true; \
	nohup cloudflared tunnel --no-autoupdate --config $(TUNNEL_FILE) run > $(TUNNEL_LOG) 2>&1 & \
	for i in $$(seq 30); do [ "$$(curl -s -o /dev/null -w '%{http_code}' https://$$media)" != 530 ] && break; sleep 1; done; \
	PUBLIC_DASHBOARD_URL=https://$$app AUTH_DEV_SIGNIN=false S3_ENDPOINT=https://$$media $(DEV) up -d --build --wait && \
	echo "Dashboard: https://$$app  Media: https://$$media  Tunnel log: $(TUNNEL_LOG)  (make dev-down stops both)"

pull: ## Download the released image (STAQLY_VERSION=v0.2.0 for a specific release)
	docker compose pull staqly


site: ## Landing page (http://localhost:8070) and docs (http://localhost:8090) from this checkout, linked to each other and the dev dashboard; Ctrl+C stops them
	NEXT_PUBLIC_APP_URL=http://localhost:8080 NEXT_PUBLIC_DOCS_URL=http://localhost:8090 NEXT_PUBLIC_WEB_URL=http://localhost:8070 \
		pnpm turbo run dev --filter=@staqly/web --filter=@staqly/docs

dev-down: ## Stop the dev stack (data volume kept) and a dev-tunnel
	$(DEV) --profile stripe down
	@pkill -f "cloudflared tunnel --no-autoupdate --config" || true

stripe-secret: ## Print the webhook signing secret of the Stripe CLI (put it in .env as STRIPE_WEBHOOK_SECRET)
	@$(DEV) run --rm --no-deps stripe listen --print-secret

# Tags the pushed origin/main (never local work), only when its CI run went green; the tag triggers
# .github/workflows/release.yml (binaries + ghcr.io/staqly/staqly:VERSION and :latest).
release: ## Tag origin/main as VERSION (make release VERSION=v0.2.0); CI publishes the binaries and image
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || { echo "usage: make release VERSION=v<major>.<minor>.<patch>" >&2; exit 2; }
	@git fetch --quiet --tags origin main
	@! git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null || { echo "tag $(VERSION) already exists" >&2; exit 1; }
	@ci=$$(gh run list --workflow ci.yml --branch main --commit "$$(git rev-parse origin/main)" --json conclusion --jq '.[0].conclusion' 2>/dev/null); \
	  [ "$$ci" = success ] || { echo "CI for $$(git rev-parse --short origin/main) is '$${ci:-not found}', not 'success'; not releasing." >&2; exit 1; }
	git tag -a "$(VERSION)" origin/main -m "release $(VERSION)"
	git push --quiet origin "$(VERSION)"

open: ## Open the dashboard in this computer's browser, signed in with the access token (local mode)
	@port=$$(docker port staqly 8080/tcp 2>/dev/null | head -n1 | sed 's/.*://'); \
	  if tok=$$(docker exec staqly server token 2>/dev/null); then link="http://localhost:$${port:-8080}/#token=$$tok"; \
	  else link=$$(docker exec staqly server connect-link 2>/dev/null); fi; \
	  [ -n "$$link" ] && echo "Dashboard: $$link" && \
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

token: ## Print local mode's access token (agents, SDKs, curl): make token [ROTATE=1] (a new one; every browser is signed out)
	@docker exec staqly server token $(if $(ROTATE),--rotate)

connect: ## Print a one-time sign-in link for another browser (a phone): make connect [WS={WORKSPACE_ID}]
	@docker exec staqly server connect-link $(WS)

workspaces: ## List workspaces and their plans (cloud mode)
	docker exec staqly server workspaces

set-plan: ## Change a workspace's plan by hand (cloud mode): make set-plan WS=ws_... PLAN=pro
	docker exec staqly server set-plan $(WS) $(PLAN)

