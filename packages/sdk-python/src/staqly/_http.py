"""HTTP core shared by the sync and async clients: configuration, auth,
timeouts, retries with exponential backoff + jitter, idempotency keys and
error mapping. Only the I/O differs between :class:`HTTPClient` and
:class:`AsyncHTTPClient`."""

from __future__ import annotations

import asyncio
import contextlib
import json
import mimetypes
import os
import random
import time
import uuid
from collections.abc import AsyncIterator, Iterator, Mapping
from dataclasses import dataclass, field
from datetime import date, datetime
from pathlib import Path
from typing import IO, Any, TypedDict

import httpx

from ._version import __version__
from .errors import (
    APIConnectionError,
    APIError,
    TimeoutError,
    make_api_error,
    parse_retry_after,
)

DEFAULT_BASE_URL = "http://localhost:3333"
DEFAULT_TIMEOUT = 60.0
DEFAULT_MAX_RETRIES = 2
#: A ``Retry-After`` longer than this (seconds) is not waited for; the 429 is raised.
MAX_RETRY_AFTER = 60.0
USER_AGENT = f"staqly-sdk-python/{__version__}"
RETRYABLE_STATUS = frozenset({408, 429, 500, 502, 503, 504})

#: Anything ``items.upload`` / ``media.upload`` accept: a path, bytes, or a binary file object.
FileInput = str | os.PathLike[str] | bytes | bytearray | memoryview | IO[bytes]


class RequestOptions(TypedDict, total=False):
    """Per-request overrides accepted by every method as ``request_options=``."""

    timeout: float
    """Timeout for this request in seconds."""
    max_retries: int
    idempotency_key: str
    """Idempotency-Key for unsafe requests (generated automatically when retries are enabled)."""
    headers: Mapping[str, str]


@dataclass
class RequestSpec:
    method: str
    path: str
    params: Mapping[str, Any] | None = None
    json: Any = None
    files: dict[str, tuple[str, bytes, str]] | None = None
    data: dict[str, str] | None = None
    no_auth: bool = False
    options: RequestOptions = field(default_factory=lambda: RequestOptions())


def clean(d: Mapping[str, Any]) -> dict[str, Any]:
    """Drops ``None`` values (unset optional arguments)."""
    return {k: v for k, v in d.items() if v is not None}


def query_value(v: Any) -> str:
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, (datetime, date)):
        return v.isoformat()
    return str(v)


def read_file(file: FileInput, filename: str | None, content_type: str | None) -> tuple[str, bytes, str]:
    """Normalises an upload into ``(filename, bytes, content_type)``.

    Files are read into memory (the server limits uploads by plan, 25 MB on Free).
    """
    name = filename
    if isinstance(file, (str, os.PathLike)):
        path = Path(file)
        data = path.read_bytes()
        name = name or path.name
    elif isinstance(file, (bytes, bytearray, memoryview)):
        data = bytes(file)
    else:
        data = file.read()
        if not isinstance(data, bytes):
            raise TypeError("file objects must be opened in binary mode")
        raw_name = getattr(file, "name", None)
        if not name and isinstance(raw_name, str) and raw_name:
            name = os.path.basename(raw_name)
    name = name or "upload"
    ctype = content_type or mimetypes.guess_type(name)[0] or "application/octet-stream"
    return name, data, ctype


def open_for_write(path: Path) -> IO[bytes]:
    """Opens a download target (kept out of async code paths for linters; writes are small chunks)."""
    return path.open("wb")


def backoff(attempt: int) -> float:
    """0.5 s * 2^attempt capped at 8 s, with up to 25 % jitter."""
    base: float = min(0.5 * 2**attempt, 8.0)
    return base * (0.75 + random.random() * 0.25)


def _env(name: str) -> str | None:
    return os.environ.get(name) or None


class BaseHTTPClient:
    """Configuration and request building shared by both clients."""

    def __init__(
        self,
        *,
        base_url: str | None,
        api_key: str | None,
        timeout: float | httpx.Timeout | None,
        max_retries: int | None,
        default_headers: Mapping[str, str] | None,
        user_agent: str | None,
    ) -> None:
        self.base_url = (base_url or _env("STAQLY_API_URL") or DEFAULT_BASE_URL).rstrip("/")
        # api_key=None reads STAQLY_API_KEY; api_key="" means "no key" explicitly.
        self.api_key = (_env("STAQLY_API_KEY") if api_key is None else api_key) or None
        self.timeout = timeout if timeout is not None else DEFAULT_TIMEOUT
        self.max_retries = max(0, DEFAULT_MAX_RETRIES if max_retries is None else max_retries)
        self.default_headers = dict(default_headers or {})
        self.user_agent = f"{USER_AGENT} {user_agent}" if user_agent else USER_AGENT

    def url(self, path: str) -> str:
        return self.base_url + path

    def _prepare(self, spec: RequestSpec) -> tuple[dict[str, Any], bool, int]:
        opts = spec.options
        max_retries = max(0, opts.get("max_retries", self.max_retries))
        headers: dict[str, str] = {"Accept": "application/json", "User-Agent": self.user_agent}
        headers.update(self.default_headers)
        if self.api_key and not spec.no_auth:
            headers["Authorization"] = f"Bearer {self.api_key}"
        kwargs: dict[str, Any] = {}
        if spec.files is not None:
            kwargs["files"] = spec.files
            if spec.data:
                kwargs["data"] = spec.data
        elif spec.json is not None:
            headers["Content-Type"] = "application/json"
            kwargs["content"] = json.dumps(spec.json, default=_json_default).encode()
        # Unsafe JSON requests carry an Idempotency-Key so a retry never applies
        # twice (the server replays the first response). Multipart is not covered
        # by the server, so uploads are only retried on 429 (not processed).
        unsafe = spec.method != "GET"
        idempotent = not unsafe or (spec.files is None and ("idempotency_key" in opts or max_retries > 0))
        if unsafe and spec.files is None and idempotent:
            headers["Idempotency-Key"] = opts.get("idempotency_key") or f"sdk-{uuid.uuid4()}"
        headers.update(opts.get("headers", {}))
        params = {k: query_value(v) for k, v in (spec.params or {}).items() if v is not None and v != ""}
        kwargs.update(
            method=spec.method,
            url=self.url(spec.path),
            params=params,
            headers=headers,
            timeout=opts.get("timeout", self.timeout),
        )
        return kwargs, idempotent, max_retries

    @staticmethod
    def _retry_delay(error: APIError, idempotent: bool, attempt: int) -> float | None:
        """Seconds to wait before retrying ``error``, or ``None`` to raise it."""
        retryable = error.status in RETRYABLE_STATUS or (
            error.status == 409 and error.code == "idempotency_in_progress"
        )
        # 429 means the server did not process the request: always safe to repeat.
        if error.status != 429 and not (idempotent and retryable):
            return None
        retry_after = parse_retry_after(error.headers.get("retry-after"))
        if retry_after is None:
            return backoff(attempt)
        return retry_after if retry_after <= MAX_RETRY_AFTER else None

    @staticmethod
    def _connection_error(exc: httpx.HTTPError, spec: RequestSpec) -> APIConnectionError:
        if isinstance(exc, httpx.TimeoutException):
            return TimeoutError(f"Request timed out: {spec.method} {spec.path}")
        return APIConnectionError(f"Connection error: {spec.method} {spec.path}: {exc}")


def _json_default(o: Any) -> Any:
    if isinstance(o, (datetime, date)):
        return o.isoformat()
    if hasattr(o, "model_dump"):
        return o.model_dump(exclude_none=True, mode="json")
    raise TypeError(f"Object of type {type(o).__name__} is not JSON serializable")


class HTTPClient(BaseHTTPClient):
    """Blocking transport (``httpx.Client``)."""

    def __init__(self, *, http_client: httpx.Client | None = None, **kwargs: Any) -> None:
        super().__init__(**kwargs)
        self._owns_client = http_client is None
        self._client = http_client or httpx.Client()

    def _send(self, spec: RequestSpec, *, stream: bool) -> httpx.Response:
        kwargs, idempotent, max_retries = self._prepare(spec)
        timeout = kwargs.pop("timeout")
        attempt = 0
        while True:
            try:
                request = self._client.build_request(**kwargs, timeout=timeout)
                response = self._client.send(request, stream=stream)
            except httpx.TransportError as exc:
                if attempt < max_retries and idempotent:
                    self._sleep(backoff(attempt))
                    attempt += 1
                    continue
                raise self._connection_error(exc, spec) from exc
            if response.is_success:
                return response
            if stream:
                response.read()
                response.close()
            error = make_api_error(response)
            delay = self._retry_delay(error, idempotent, attempt) if attempt < max_retries else None
            if delay is None:
                raise error
            self._sleep(delay)
            attempt += 1

    def request(self, spec: RequestSpec) -> httpx.Response:
        return self._send(spec, stream=False)

    @contextlib.contextmanager
    def stream(self, spec: RequestSpec) -> Iterator[httpx.Response]:
        """Like :meth:`request` but leaves the body unread for iteration."""
        response = self._send(spec, stream=True)
        try:
            yield response
        except httpx.TransportError as exc:
            raise self._connection_error(exc, spec) from exc
        finally:
            response.close()

    @staticmethod
    def _sleep(seconds: float) -> None:
        time.sleep(seconds)

    def close(self) -> None:
        if self._owns_client:
            self._client.close()


class AsyncHTTPClient(BaseHTTPClient):
    """Asyncio transport (``httpx.AsyncClient``)."""

    def __init__(self, *, http_client: httpx.AsyncClient | None = None, **kwargs: Any) -> None:
        super().__init__(**kwargs)
        self._owns_client = http_client is None
        self._client = http_client or httpx.AsyncClient()

    async def _send(self, spec: RequestSpec, *, stream: bool) -> httpx.Response:
        kwargs, idempotent, max_retries = self._prepare(spec)
        timeout = kwargs.pop("timeout")
        attempt = 0
        while True:
            try:
                request = self._client.build_request(**kwargs, timeout=timeout)
                response = await self._client.send(request, stream=stream)
            except httpx.TransportError as exc:
                if attempt < max_retries and idempotent:
                    await self._sleep(backoff(attempt))
                    attempt += 1
                    continue
                raise self._connection_error(exc, spec) from exc
            if response.is_success:
                return response
            if stream:
                await response.aread()
                await response.aclose()
            error = make_api_error(response)
            delay = self._retry_delay(error, idempotent, attempt) if attempt < max_retries else None
            if delay is None:
                raise error
            await self._sleep(delay)
            attempt += 1

    async def request(self, spec: RequestSpec) -> httpx.Response:
        return await self._send(spec, stream=False)

    @contextlib.asynccontextmanager
    async def stream(self, spec: RequestSpec) -> AsyncIterator[httpx.Response]:
        """Like :meth:`request` but leaves the body unread for iteration."""
        response = await self._send(spec, stream=True)
        try:
            yield response
        except httpx.TransportError as exc:
            raise self._connection_error(exc, spec) from exc
        finally:
            await response.aclose()

    @staticmethod
    async def _sleep(seconds: float) -> None:
        await asyncio.sleep(seconds)

    async def aclose(self) -> None:
        if self._owns_client:
            await self._client.aclose()
