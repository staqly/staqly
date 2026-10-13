# packages/sdk-python — `staqly` (PyPI)

Python SDK over the REST API: sync `Staqly` + async `AsyncStaqly` on httpx,
pydantic v2 models. Python ≥3.10. Tooling via uv (`.venv` in this directory).

| Path | Role |
|---|---|
| `src/staqly/_client_async.py` | **Source of truth** for all resources + `AsyncStaqly` |
| `src/staqly/_client_sync.py` | **Generated** by `scripts/gen_sync.py` (`make gen`). Never edit by hand |
| `src/staqly/_http.py` | Transport: config/env, auth, retries/backoff, Idempotency-Key, uploads |
| `src/staqly/errors.py` | Error hierarchy + status → class mapping |
| `src/staqly/models.py` | Hand-written pydantic models (`extra="allow"`), mirrored from OpenAPI |
| `src/staqly/webhooks.py` | `verify_webhook`, `compute_signature` |
| `tests/` | respx-mocked unit tests; `test_live.py` runs only with `STAQLY_LIVE=1` |

```bash
uv sync
make check          # gen + ruff + mypy --strict + pytest + python -m build
make test           # unit tests
STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3333 uv run pytest tests/test_live.py   # local server
STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3334 uv run pytest tests/test_live.py   # cloud server (creates a workspace)
```

Rules:

* Edit `_client_async.py`, then `make gen`; `tests/test_codegen.py` fails on a
  stale sync file. Generation strips async/await, drops the `Async` name prefix
  and removes lines tagged `# async-only`.
* API change → update `models.py`; `tests/test_models_contract.py` compares
  every model with `apps/api/openapi.yaml` and fails on drift.
* Keep the surface aligned with `packages/sdk-typescript` (same namespaces and
  method names, snake_case here). New endpoint → method + a `Case` row in
  `tests/test_resources.py` (runs sync and async) + README.
* Runtime deps stay `httpx` + `pydantic` only. Bump `pyproject.toml` and
  `src/staqly/_version.py` together (a test checks it); add a CHANGELOG entry.
