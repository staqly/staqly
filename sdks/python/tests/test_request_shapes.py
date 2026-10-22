"""Headers on every resource method, plus the request shapes and error paths that
test_resources.py / test_client.py only cover for one of the two clients."""

from __future__ import annotations

import time
from email.utils import formatdate
from pathlib import Path

import httpx
import pytest

from staqly import (
    APIError,
    AsyncStaqly,
    ExportFailedError,
    InternalServerError,
    NotFoundError,
    PermissionDeniedError,
    PlanLimitError,
    RateLimitError,
    Staqly,
    WaitTimeoutError,
)
from staqly.errors import parse_retry_after

from .conftest import BASE, Mock, api_error, body, jsonr, post, query
from .test_resources import CASES, COL, Case

# Endpoints sent without credentials (the token / recovery key is the credential).
PUBLIC = {
    "health",
    "live",
    "ready",
    "setup",
    "sessions.recover",
    "sessions.sign_in_with_api_key",
    "device_links.redeem",
}
# Unsafe requests that are never retried, so they carry no automatic Idempotency-Key.
NOT_RETRIED: set[str] = set()


def _reply(case: Case) -> httpx.Response:
    return httpx.Response(case.status) if case.reply is None else jsonr(case.reply, case.status)


def _verify_headers(case: Case, req: httpx.Request) -> None:
    if case.name in PUBLIC:
        assert "authorization" not in req.headers
    else:
        assert req.headers["authorization"] == "Bearer stk_test"
    assert req.headers["accept"] == "application/json"
    assert req.headers["user-agent"].startswith("staqly-sdk-python/")
    if case.method == "GET" or case.name in NOT_RETRIED:
        assert "idempotency-key" not in req.headers
    else:
        assert req.headers["idempotency-key"].startswith("sdk-")
    if case.body is not None:
        assert req.headers["content-type"] == "application/json"


@pytest.mark.parametrize("case", CASES, ids=[c.name for c in CASES])
def test_headers_sync(case: Case, client: Staqly, mock: Mock) -> None:
    mock.reply(_reply(case))
    case.call(client)
    _verify_headers(case, mock.last)


@pytest.mark.parametrize("case", CASES, ids=[c.name for c in CASES])
async def test_headers_async(case: Case, aclient: AsyncStaqly, mock: Mock) -> None:
    mock.reply(_reply(case))
    await case.call(aclient)
    _verify_headers(case, mock.last)


class TestResolveId:
    def test_id_needs_no_request(self, client: Staqly, mock: Mock) -> None:
        assert client.collections.resolve_id("col_9") == "col_9"
        assert mock.requests == []

    def test_name_is_looked_up(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(COL | {"id": "col_7"}))
        assert client.collections.resolve_id("My posts") == "col_7"
        assert mock.last.method == "GET"
        assert mock.last.url.raw_path == b"/v1/collections/My%20posts"

    async def test_async(self, aclient: AsyncStaqly, mock: Mock) -> None:
        assert await aclient.collections.resolve_id("col_9") == "col_9"
        assert mock.requests == []
        mock.reply(jsonr(COL | {"id": "col_7"}))
        assert await aclient.collections.resolve_id("Posts") == "col_7"
        assert mock.last.url.path == "/v1/collections/Posts"

    def test_unknown_name_raises(self, client: Staqly, mock: Mock) -> None:
        mock.reply(api_error(404, "not_found"))
        with pytest.raises(NotFoundError):
            client.collections.resolve_id("nope")


TS = "2026-01-01T00:00:00Z"


def job(status: str) -> httpx.Response:
    return jsonr({"id": "exp_1", "status": status, "size_bytes": 3, "created_at": TS, "expires_at": TS})


class TestExports:
    def test_save_workspace_export(self, client: Staqly, mock: Mock, tmp_path: Path) -> None:
        mock.reply(httpx.Response(200, content=b"PK\x03\x04", headers={"Content-Type": "application/zip"}))
        out = client.exports.save_workspace_export("exp/1", tmp_path / "ws.zip")
        assert out == tmp_path / "ws.zip"
        assert out.read_bytes() == b"PK\x03\x04"
        assert mock.last.method == "GET"
        assert mock.last.url.raw_path == b"/v1/workspace/exports/exp%2F1/download"
        assert mock.last.headers["authorization"] == "Bearer stk_test"

    async def test_save_workspace_export_async(
        self, aclient: AsyncStaqly, mock: Mock, tmp_path: Path
    ) -> None:
        mock.reply(httpx.Response(200, content=b"PK\x03\x04"))
        out = await aclient.exports.save_workspace_export("exp_1", str(tmp_path / "ws.zip"))
        assert out.read_bytes() == b"PK\x03\x04"
        assert mock.last.url.path == "/v1/workspace/exports/exp_1/download"

    def test_save_events_query(self, client: Staqly, mock: Mock, tmp_path: Path) -> None:
        mock.reply(httpx.Response(200, text="id\n"))
        client.exports.save_events(
            tmp_path / "e.ndjson",
            from_="2026-01-01",
            to="2026-02-01",
            type="post.published",
            actor_type="api_key",
            actor_id="key_1",
            collection_id="col_1",
        )
        assert mock.last.url.path == "/v1/events/export"
        assert query(mock.last) == {
            "format": "ndjson",
            "from": "2026-01-01",
            "to": "2026-02-01",
            "type": "post.published",
            "actor_type": "api_key",
            "actor_id": "key_1",
            "collection_id": "col_1",
        }

    async def test_save_events_async(self, aclient: AsyncStaqly, mock: Mock, tmp_path: Path) -> None:
        mock.reply(httpx.Response(200, text="a,b\n"))
        out = await aclient.exports.save_events(tmp_path / "e.csv", format="csv")
        assert out.read_text() == "a,b\n"
        assert query(mock.last) == {"format": "csv"}

    async def test_wait_async_failed(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(job("failed"))
        with pytest.raises(ExportFailedError):
            await aclient.exports.wait_for_workspace_export("exp_1")

    async def test_wait_async_timeout(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(job("running"))
        with pytest.raises(WaitTimeoutError):
            await aclient.exports.wait_for_workspace_export("exp_1", timeout=0.5, poll_interval=0.01)
        assert len(mock.requests) >= 2

    def test_export_workspace_without_path(self, client: Staqly, mock: Mock) -> None:
        mock.reply(job("pending"), job("ready"))
        assert client.exports.export_workspace(poll_interval=0.01).status == "ready"
        assert [f"{r.method} {r.url.path}" for r in mock.requests] == [
            "POST /v1/workspace/export",
            "GET /v1/workspace/exports/exp_1",
        ]

    async def test_stream_error_async(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(api_error(403, "forbidden"))
        with pytest.raises(PermissionDeniedError):
            async for _ in aclient.exports.events():
                pass


class TestAsyncOnlyGaps:
    async def test_media_upload(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(
            jsonr(
                {
                    "id": "med_1",
                    "mime_type": "application/pdf",
                    "size_bytes": 4,
                    "original_name": "a.pdf",
                    "url": "u",
                },
                201,
            )
        )
        out = await aclient.media.upload(b"%PDF", filename="a.pdf")
        assert out.mime_type == "application/pdf"
        req = mock.last
        assert req.method == "POST"
        assert req.url.path == "/v1/media"
        assert req.headers["content-type"].startswith("multipart/form-data; boundary=")
        assert "idempotency-key" not in req.headers
        assert b'filename="a.pdf"' in req.content
        assert b"Content-Type: application/pdf" in req.content

    async def test_media(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(httpx.Response(200, content=b"\x01\x02"))
        assert await aclient.media.download("med_1") == b"\x01\x02"
        assert mock.last.url.path == "/v1/media/med_1"
        assert aclient.media.url("med 1") == f"{BASE}/v1/media/med%201"

    async def test_text_endpoints(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(httpx.Response(200, text="# HELP\n"))
        assert await aclient.metrics(token="mtok") == "# HELP\n"
        assert mock.last.headers["authorization"] == "Bearer mtok"
        assert mock.last.headers["accept"] == "text/plain"
        assert await aclient.metrics() == "# HELP\n"
        assert "authorization" not in mock.last.headers
        mock.reply(httpx.Response(200, text="openapi: 3.2.0\n"))
        assert await aclient.openapi() == "openapi: 3.2.0\n"
        assert mock.last.url.path == "/openapi.yaml"
        assert mock.last.headers["accept"] == "application/yaml"

    def test_metrics_without_token_sends_no_credentials(self, client: Staqly, mock: Mock) -> None:
        mock.reply(httpx.Response(200, text="x"))
        client.metrics()
        assert "authorization" not in mock.last.headers

    async def test_low_level_request(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(jsonr({"ok": True}))
        assert await aclient.request("post", "/v1/anything", params={"a": 1}, json={"b": 2}) == {"ok": True}
        assert mock.last.method == "POST"
        assert query(mock.last) == {"a": "1"}
        assert body(mock.last) == {"b": 2}
        assert mock.last.headers["idempotency-key"].startswith("sdk-")
        mock.reply(httpx.Response(204))
        assert await aclient.request("DELETE", "/v1/x") is None

    def test_low_level_request_204(self, client: Staqly, mock: Mock) -> None:
        mock.reply(httpx.Response(204))
        assert client.request("DELETE", "/v1/x") is None

    async def test_workspaces_create(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(
            jsonr(
                {
                    "workspace": {"id": "ws_1", "name": "x", "plan": "free", "created_at": TS},
                    "api_key": {
                        "id": "key_1",
                        "name": "Admin key",
                        "prefix": "stk_abc",
                        "scopes": ["admin"],
                        "created_at": TS,
                        "key": "stk_secret",
                    },
                    "recovery_key": "rk",
                    "csrf_token": "c",
                },
                201,
            )
        )
        out = await aclient.workspaces.create(name="x", setup_token="st")
        assert out.recovery_key == "rk"
        assert mock.last.url.path == "/v1/workspaces"
        assert body(mock.last) == {"name": "x", "setup_token": "st"}
        assert "authorization" not in mock.last.headers

    async def test_custom_headers_are_merged(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(httpx.Response(200, text="openapi: 3.2.0\n"))
        await aclient.openapi(request_options={"headers": {"X-T": "1"}})
        assert mock.last.headers["accept"] == "application/yaml"
        assert mock.last.headers["x-t"] == "1"


class TestPagination:
    def test_iterate_starts_at_cursor(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr({"data": [post(id="z")]}))
        assert [i.id for i in client.posts.iterate(cursor="start", tag="a")] == ["z"]
        assert query(mock.last) == {"cursor": "start", "tag": "a"}

    async def test_async_iterate_multi_page(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(jsonr({"data": [post()], "next_cursor": "n"}), jsonr({"data": []}))
        assert len([i async for i in aclient.posts.iterate(collection_id="News")]) == 1
        assert [r.url.path for r in mock.requests] == ["/v1/posts", "/v1/posts"]
        assert mock.requests[1].url.params["cursor"] == "n"
        assert mock.requests[1].url.params["collection_id"] == "News"

    async def test_async_events_multi_page(self, aclient: AsyncStaqly, mock: Mock) -> None:
        ev = {
            "seq": 1,
            "type": "post.created",
            "actor": {"type": "local", "id": "l", "name": "L"},
            "created_at": TS,
        }
        mock.reply(
            jsonr({"data": [ev | {"id": "e1"}], "next_cursor": "x"}), jsonr({"data": [ev | {"id": "e2"}]})
        )
        assert [e.id async for e in aclient.events.iterate(limit=1)] == ["e1", "e2"]
        assert [r.url.params.get("cursor") for r in mock.requests] == [None, "x"]

    def test_iterate_stops_on_error(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr({"data": [post(id="a")], "next_cursor": "c"}), api_error(404, "not_found"))
        it = client.posts.iterate()
        assert next(it).id == "a"
        with pytest.raises(NotFoundError):
            next(it)


class TestErrorMapping:
    @pytest.mark.parametrize(
        ("status", "code", "cls"),
        [
            (402, "payment_required", PlanLimitError),
            (402, "plan_limit_exceeded", PlanLimitError),
            (403, "plan_limit_exceeded", PlanLimitError),
            (429, "rate_limited", RateLimitError),
            (500, "internal", InternalServerError),
            (502, "bad_gateway", InternalServerError),
            (503, "unavailable", InternalServerError),
            (504, "timeout", InternalServerError),
        ],
    )
    async def test_status_async(
        self, aclient: AsyncStaqly, mock: Mock, status: int, code: str, cls: type[APIError]
    ) -> None:
        mock.reply(api_error(status, code))
        with pytest.raises(cls) as exc:
            await aclient.posts.create("Posts", content="x", request_options={"max_retries": 0})
        assert exc.value.status_code == status
        assert exc.value.code == code

    def test_plan_limit_without_details(self, client: Staqly, mock: Mock) -> None:
        mock.reply(api_error(402, "payment_required", "pay"))
        with pytest.raises(PlanLimitError) as exc:
            client.posts.list()
        assert exc.value.details == {}
        assert exc.value.limit is None

    @pytest.mark.parametrize(
        "raw",
        [b"", b"[1,2]", b'{"error": "flat"}', b'{"error": {"code": 7}}'],
        ids=["empty", "list", "flat", "bad-code"],
    )
    def test_unstructured_bodies(self, client: Staqly, mock: Mock, raw: bytes) -> None:
        mock.reply(httpx.Response(404, content=raw))
        with pytest.raises(NotFoundError) as exc:
            client.posts.get("pst_1")
        assert exc.value.code == "http_error"
        assert exc.value.message == "HTTP 404"

    def test_500_text_body_is_truncated(self, client: Staqly, mock: Mock) -> None:
        mock.reply(httpx.Response(500, text="x" * 900))
        with pytest.raises(InternalServerError) as exc:
            client.posts.list(request_options={"max_retries": 0})
        assert exc.value.message == "x" * 500
        assert exc.value.body == "x" * 900

    def test_fields_and_details_must_be_objects(self, client: Staqly, mock: Mock) -> None:
        mock.reply(api_error(422, "validation_failed", fields=["x"], details="y"))
        with pytest.raises(APIError) as exc:
            client.posts.list()
        assert exc.value.fields == {}
        assert exc.value.details == {}


class TestRetryAfter:
    def test_seconds(self) -> None:
        assert parse_retry_after("2.5") == 2.5
        assert parse_retry_after("-3") == 0.0
        assert parse_retry_after(None) is None
        assert parse_retry_after("") is None

    def test_http_date(self) -> None:
        future = formatdate(timeval=time.time() + 60, usegmt=True)
        out = parse_retry_after(future)
        assert out is not None
        assert 50 < out <= 61
        assert parse_retry_after("Thu, 01 Jan 1970 00:00:00 GMT") == 0.0

    def test_garbage_is_ignored(self) -> None:
        assert parse_retry_after("soon") is None

    def test_malformed_header_still_raises_rate_limit(
        self, client: Staqly, mock: Mock, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        monkeypatch.setattr("staqly._http.backoff", lambda attempt: 0.0)
        mock.reply(httpx.Response(429, json={}, headers={"Retry-After": "soon"}))
        with pytest.raises(RateLimitError) as exc:
            client.posts.list()
        assert exc.value.retry_after is None
        assert len(mock.requests) == 3  # falls back to exponential backoff

    async def test_malformed_header_async(
        self, aclient: AsyncStaqly, mock: Mock, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        monkeypatch.setattr("staqly._http.backoff", lambda attempt: 0.0)
        mock.reply(httpx.Response(429, json={}, headers={"Retry-After": "soon"}), jsonr({"data": []}))
        await aclient.posts.list()
        assert len(mock.requests) == 2
