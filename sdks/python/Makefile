.PHONY: install gen lint typecheck test test-live build check

install:            ## dev environment in .venv
	uv sync

gen:                ## regenerate the sync client from the async source
	uv run python scripts/gen_sync.py

lint:
	uv run ruff check . && uv run ruff format --check .

typecheck:
	uv run mypy --strict

test:
	uv run pytest

test-live:          ## needs a server: STAQLY_API_URL=... [STAQLY_API_KEY=...]
	STAQLY_LIVE=1 uv run pytest tests/test_live.py -v

build:
	uv run python -m build

check: gen lint typecheck test build
