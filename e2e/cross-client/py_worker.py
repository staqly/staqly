"""JSON-lines bridge that lets the cross-client suite drive the Python SDK.

Run by e2e/cross-client/lib.ts as ``uv run --project sdks/python python py_worker.py``.
Every stdin line is one call::

    {"id": 1, "mode": "sync" | "async", "client": {"base_url": ..., "api_key": ...},
     "call": "posts.create", "args": [...], "kwargs": {...}}

and every stdout line one answer: ``{"id": 1, "ok": true, "result": ...}`` or
``{"id": 1, "ok": false, "error": {"type": "ConflictError", "status": 409, ...}}``.
Calls run concurrently (sync ones in threads), so the suite can race the
Python SDK against other clients. ``{"$bytes": "<base64>"}`` in arguments
becomes ``bytes``; ``bytes`` results come back the same way. Generators and
async generators are collected into lists.

Client options beyond the constructor's:

* ``drop_first_response``: the first unsafe response is thrown away and a
  network error raised instead (the server did the work, the client never
  heard), so the SDK's retry must be replayed by the server's idempotency.
* ``record``: remember the headers of every request (``_requests`` call).
"""

from __future__ import annotations

import asyncio
import base64
import inspect
import json
import sys
import threading
import time
import traceback
from concurrent.futures import ThreadPoolExecutor
from typing import Any

import httpx
import staqly
from staqly import APIError, AsyncStaqly, Staqly, StaqlyError

_write_lock = threading.Lock()
_pool = ThreadPoolExecutor(max_workers=32)
_records: dict[str, list[dict[str, Any]]] = {}


def _decode(v: Any) -> Any:
    if isinstance(v, dict):
        if set(v) == {"$bytes"}:
            return base64.b64decode(v["$bytes"])
        return {k: _decode(x) for k, x in v.items()}
    if isinstance(v, list):
        return [_decode(x) for x in v]
    return v


def _encode(v: Any) -> Any:
    if hasattr(v, "model_dump"):
        return v.model_dump(mode="json")
    if isinstance(v, (bytes, bytearray, memoryview)):
        return {"$bytes": base64.b64encode(bytes(v)).decode()}
    if isinstance(v, dict):
        return {k: _encode(x) for k, x in v.items()}
    if isinstance(v, (list, tuple)):
        return [_encode(x) for x in v]
    if hasattr(v, "__fspath__"):
        return str(v)
    return v


def _error(exc: BaseException) -> dict[str, Any]:
    out: dict[str, Any] = {"type": type(exc).__name__, "message": str(exc)}
    out["is_staqly_error"] = isinstance(exc, StaqlyError)
    out["mro"] = [c.__name__ for c in type(exc).__mro__]
    if isinstance(exc, APIError):
        out.update(status=exc.status, code=exc.code)
        for attr in ("retry_after", "limit", "max", "plan", "required_scope"):
            if hasattr(exc, attr):
                out[attr] = getattr(exc, attr)
    if not isinstance(exc, StaqlyError):
        out["traceback"] = traceback.format_exc()
    return out


class _DropFirst:
    """Shared by the sync and async transports below."""

    def __init__(self) -> None:
        self.dropped = False

    def should_drop(self, request: httpx.Request) -> bool:
        if self.dropped or request.method == "GET":
            return False
        self.dropped = True
        return True


class _SyncTransport(httpx.HTTPTransport):
    def __init__(self, key: str, drop: _DropFirst | None) -> None:
        super().__init__()
        self.key, self.drop = key, drop

    def handle_request(self, request: httpx.Request) -> httpx.Response:
        _records.setdefault(self.key, []).append(
            {"method": request.method, "headers": dict(request.headers)}
        )
        response = super().handle_request(request)
        if self.drop is not None and self.drop.should_drop(request):
            response.read()
            response.close()
            raise httpx.ReadError(
                "connection reset after the server answered", request=request
            )
        return response


class _AsyncTransport(httpx.AsyncHTTPTransport):
    def __init__(self, key: str, drop: _DropFirst | None) -> None:
        super().__init__()
        self.key, self.drop = key, drop

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        _records.setdefault(self.key, []).append(
            {"method": request.method, "headers": dict(request.headers)}
        )
        response = await super().handle_async_request(request)
        if self.drop is not None and self.drop.should_drop(request):
            await response.aread()
            await response.aclose()
            raise httpx.ReadError(
                "connection reset after the server answered", request=request
            )
        return response


def _make_client(mode: str, opts: dict[str, Any]) -> Any:
    opts = dict(opts)
    key = opts.pop("record_key", None) or "default"
    drop = _DropFirst() if opts.pop("drop_first_response", False) else None
    record = opts.pop("record", False)
    if mode == "async":
        if record or drop:
            opts["http_client"] = httpx.AsyncClient(
                transport=_AsyncTransport(key, drop)
            )
        return AsyncStaqly(**opts)
    if record or drop:
        opts["http_client"] = httpx.Client(transport=_SyncTransport(key, drop))
    return Staqly(**opts)


def _resolve(client: Any, path: str) -> Any:
    target: Any = client
    for part in path.split("."):
        target = getattr(target, part)
    return target


def _special_sync(
    call: str, client: Any, args: list[Any], kwargs: dict[str, Any]
) -> Any:
    if call == "_requests":
        return _records.get(kwargs.get("key", "default"), [])
    if call == "_verify_webhook":
        return staqly.verify_webhook(
            kwargs["secret"],
            kwargs["body"].encode(),
            kwargs["headers"],
            tolerance=kwargs.get("tolerance", 300),
        )
    if call == "_wait_timed":
        started = time.monotonic()
        try:
            return {
                "ok": True,
                "result": _encode(client.posts.wait_for_published(*args, **kwargs)),
            }
        except BaseException as exc:  # noqa: BLE001 - reported to the suite
            return {
                "ok": False,
                "error": _error(exc),
                "elapsed": time.monotonic() - started,
            }
    raise KeyError(call)


async def _special_async(
    call: str, client: Any, args: list[Any], kwargs: dict[str, Any]
) -> Any:
    if call == "_requests":
        return _records.get(kwargs.get("key", "default"), [])
    if call == "_cancel_wait":
        # Cancelling the task that polls must stop it promptly.
        task = asyncio.ensure_future(
            client.posts.wait_for_published(*args, timeout=60, poll_interval=0.2)
        )
        await asyncio.sleep(kwargs.get("after", 0.5))
        started = time.monotonic()
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            return {"cancelled": True, "elapsed": time.monotonic() - started}
        return {"cancelled": False}
    if call == "_wait_timed":
        started = time.monotonic()
        try:
            return {
                "ok": True,
                "result": _encode(
                    await client.posts.wait_for_published(*args, **kwargs)
                ),
            }
        except BaseException as exc:  # noqa: BLE001
            return {
                "ok": False,
                "error": _error(exc),
                "elapsed": time.monotonic() - started,
            }
    raise KeyError(call)


def _run_sync(msg: dict[str, Any]) -> Any:
    client = _make_client("sync", msg.get("client", {}))
    try:
        args, kwargs = _decode(msg.get("args", [])), _decode(msg.get("kwargs", {}))
        call = msg["call"]
        if call.startswith("_"):
            return _special_sync(call, client, args, kwargs)
        result = _resolve(client, call)(*args, **kwargs)
        if inspect.isgenerator(result):
            result = list(result)
        return result
    finally:
        client.close()


async def _run_async(msg: dict[str, Any]) -> Any:
    client = _make_client("async", msg.get("client", {}))
    try:
        args, kwargs = _decode(msg.get("args", [])), _decode(msg.get("kwargs", {}))
        call = msg["call"]
        if call.startswith("_"):
            return await _special_async(call, client, args, kwargs)
        result = _resolve(client, call)(*args, **kwargs)
        if inspect.isasyncgen(result):
            return [x async for x in result]
        if inspect.isawaitable(result):
            return await result
        return result
    finally:
        await client.aclose()


def _send(obj: dict[str, Any]) -> None:
    line = json.dumps(obj, default=str)
    with _write_lock:
        sys.stdout.write(line + "\n")
        sys.stdout.flush()


async def _handle(msg: dict[str, Any]) -> None:
    started = time.monotonic()
    try:
        if msg.get("mode") == "async":
            result = await _run_async(msg)
        else:
            result = await asyncio.get_running_loop().run_in_executor(
                _pool, _run_sync, msg
            )
        _send(
            {
                "id": msg["id"],
                "ok": True,
                "result": _encode(result),
                "elapsed": time.monotonic() - started,
            }
        )
    except BaseException as exc:  # noqa: BLE001 - every failure goes back to the suite
        _send(
            {
                "id": msg["id"],
                "ok": False,
                "error": _error(exc),
                "elapsed": time.monotonic() - started,
            }
        )


async def main() -> None:
    loop = asyncio.get_running_loop()
    reader = asyncio.StreamReader(limit=1 << 30)  # lines carry base64 uploads
    await loop.connect_read_pipe(
        lambda: asyncio.StreamReaderProtocol(reader), sys.stdin
    )
    tasks: set[asyncio.Task[None]] = set()
    _send({"ready": True, "version": staqly.__version__})
    while line := await reader.readline():
        if not line.strip():
            continue
        task = asyncio.create_task(_handle(json.loads(line)))
        tasks.add(task)
        task.add_done_callback(tasks.discard)
    if tasks:
        await asyncio.gather(*tasks)


if __name__ == "__main__":
    asyncio.run(main())
