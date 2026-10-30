from __future__ import annotations

import re
from pathlib import Path

import httpx
import pytest

import staqly
from staqly import (
    APIConnectionError,
    APIError,
    AsyncStaqly,
    AuthenticationError,
    BadRequestError,
    ConflictError,
    GoneError,
    InternalServerError,
    NotFoundError,
    PayloadTooLargeError,
    PermissionDeniedError,
    PlanLimitError,
    RateLimitError,
    Staqly,
    StaqlyError,
    UnsupportedMediaTypeError,
    ValidationError,
)

from .conftest import BASE, Mock, api_error, body, jsonr, post


class TestConfiguration:
    def test_defaults_to_local_without_credentials(self, mock: Mock, monkeypatch: pytest.MonkeyPatch) -> None:
        monkeypatch.delenv("STAQLY_API_URL", raising=False)
        monkeypatch.delenv("STAQLY_API_KEY", raising=False)
        mock.reply(jsonr({"data": []}))
        with Staqly() as c:
            assert c.base_url == "http://localhost:3333"
            c.collections.list()
        req = mock.last
        assert str(req.url) == "http://localhost:3333/v1/collections"
        assert "authorization" not in req.headers
        assert req.headers["user-agent"] == f"staqly-sdk-python/{staqly.__version__}"

    def test_reads_environment(self, mock: Mock, monkeypatch: pytest.MonkeyPatch) -> None:
        monkeypatch.setenv("STAQLY_API_URL", "https://staqly.example.com/")
        monkeypatch.setenv("STAQLY_API_KEY", "stk_env")
        mock.reply(jsonr({"data": []}))
        with Staqly() as c:
            c.plans.list()
        assert str(mock.last.url) == "https://staqly.example.com/v1/plans"
        assert mock.last.headers["authorization"] == "Bearer stk_env"

    def test_empty_api_key_ignores_environment(self, mock: Mock, monkeypatch: pytest.MonkeyPatch) -> None:
        monkeypatch.setenv("STAQLY_API_KEY", "stk_env")
        mock.reply(jsonr({"data": []}))
        with Staqly(base_url=BASE, api_key="") as c:
            assert c.api_key is None
            c.plans.list()
        assert "authorization" not in mock.last.headers

    def test_custom_headers_and_user_agent(self, mock: Mock) -> None:
        mock.reply(jsonr({"id": "ws_1", "name": "W", "plan": "free", "created_at": "2026-01-01T00:00:00Z"}))
        with Staqly(base_url=BASE, default_headers={"X-Trace": "1"}, user_agent="bot/2") as c:
            c.workspace.get(request_options={"headers": {"X-Req": "2"}})
        assert mock.last.headers["x-trace"] == "1"
        assert mock.last.headers["x-req"] == "2"
        assert mock.last.headers["user-agent"] == f"staqly-sdk-python/{staqly.__version__} bot/2"

    def test_api_key_is_private_and_redacted(self, mock: Mock) -> None:
        secret = "stk_super_secret_value"
        with Staqly(base_url=BASE, api_key=secret) as c:
            assert c.api_key == secret
            for obj in (c, c._http):
                assert secret not in repr(obj)
                assert secret not in str(obj)
            assert "api_key='***'" in repr(c)
            assert "api_key" not in vars(c._http)
        with Staqly(base_url=BASE, api_key="") as c:
            assert "api_key=None" in repr(c)

    async def test_async_api_key_is_redacted(self) -> None:
        secret = "stk_super_secret_value"
        async with AsyncStaqly(base_url=BASE, api_key=secret) as c:
            assert c.api_key == secret
            for obj in (c, c._http):
                assert secret not in repr(obj)
            assert "api_key" not in vars(c._http)

    def test_version_matches_pyproject(self) -> None:
        text = (Path(__file__).parent.parent / "pyproject.toml").read_text()
        match = re.search(r'^version = "([^"]+)"', text, re.M)
        assert match is not None
        assert staqly.__version__ == match.group(1)

    def test_public_endpoints_send_no_credentials(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr({"status": "ok"}))
        client.health()
        assert "authorization" not in mock.last.headers
        mock.reply(
            jsonr(
                {
                    "workspace": {
                        "id": "ws_1",
                        "name": "x",
                        "plan": "free",
                        "created_at": "2026-01-01T00:00:00Z",
                    },
                    "api_key": {
                        "id": "key_1",
                        "name": "Admin key",
                        "prefix": "stk_abc",
                        "scopes": ["admin"],
                        "created_at": "2026-01-01T00:00:00Z",
                        "key": "stk_secret",
                    },
                    "recovery_key": "rk",
                    "csrf_token": "c",
                },
                201,
            )
        )
        created = client.workspaces.create(name="x")
        assert created.api_key.key == "stk_secret"
        assert "authorization" not in mock.last.headers
        for call in (
            lambda: client.setup(),
            lambda: client.live(),
            lambda: client.sessions.recover("rk"),
            lambda: client.sessions.sign_in_with_api_key("stk_admin"),
            lambda: client.device_links.redeem("tok"),
        ):
            mock.reply(
                jsonr(
                    {
                        "initialized": True,
                        "mode": "local",
                        "signup_open": False,
                        "setup_token_required": False,
                        "status": "ok",
                        "csrf_token": "c",
                        "api_key": {
                            "id": "key_2",
                            "name": "Admin key",
                            "prefix": "stk_new",
                            "scopes": ["admin"],
                            "created_at": "2026-01-01T00:00:00Z",
                            "key": "stk_new_secret",
                        },
                        "recovery_key": "rk_next",
                    }
                )
            )
            call()
            assert "authorization" not in mock.last.headers

    def test_text_endpoints(self, client: Staqly, mock: Mock) -> None:
        mock.reply(httpx.Response(200, text="# HELP staqly_up 1\n"))
        assert client.metrics(token="mtok").startswith("# HELP")
        assert mock.last.url.path == "/metrics"
        assert mock.last.headers["authorization"] == "Bearer mtok"
        assert mock.last.headers["accept"] == "text/plain"
        mock.reply(httpx.Response(200, text="openapi: 3.2.0\n"))
        assert client.openapi() == "openapi: 3.2.0\n"
        assert "authorization" not in mock.last.headers

    def test_own_http_client_is_not_closed(self, mock: Mock) -> None:
        mock.reply(jsonr({"data": []}))
        hc = httpx.Client()
        with Staqly(base_url=BASE, http_client=hc) as c:
            c.plans.list()
        assert not hc.is_closed
        hc.close()

    def test_low_level_request(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr({"ok": True}))
        assert client.request("get", "/v1/anything", params={"a": 1}) == {"ok": True}
        assert mock.last.url.path == "/v1/anything"


ERROR_CASES: list[tuple[int, str, type[APIError]]] = [
    (400, "bad_request", BadRequestError),
    (401, "unauthorized", AuthenticationError),
    (403, "forbidden", PermissionDeniedError),
    (404, "not_found", NotFoundError),
    (409, "conflict", ConflictError),
    (410, "gone", GoneError),
    (413, "too_large", PayloadTooLargeError),
    (415, "unsupported_media_type", UnsupportedMediaTypeError),
    (422, "validation_failed", ValidationError),
    (418, "teapot", APIError),
]


class TestErrors:
    @pytest.mark.parametrize(("status", "code", "cls"), ERROR_CASES)
    def test_status_mapping(self, client: Staqly, mock: Mock, status: int, code: str, cls: type) -> None:
        mock.reply(api_error(status, code, "boom"))
        with pytest.raises(cls) as exc:
            client.posts.get("pst_1")
        err = exc.value
        assert isinstance(err, APIError)
        assert isinstance(err, StaqlyError)
        assert (err.status, err.code, err.message) == (status, code, "boom")
        assert str(err) == f"{status} {code}: boom"

    @pytest.mark.parametrize(("status", "code", "cls"), ERROR_CASES)
    async def test_status_mapping_async(
        self, aclient: AsyncStaqly, mock: Mock, status: int, code: str, cls: type
    ) -> None:
        mock.reply(api_error(status, code))
        with pytest.raises(cls):
            await aclient.posts.get("pst_1")

    def test_validation_fields(self, client: Staqly, mock: Mock) -> None:
        mock.reply(api_error(422, "validation_failed", "name: is required", fields={"name": "is required"}))
        with pytest.raises(ValidationError) as exc:
            client.collections.create("")
        assert exc.value.fields == {"name": "is required"}

    def test_plan_limit(self, client: Staqly, mock: Mock) -> None:
        details = {"limit": "webhooks", "max": 3, "used": 3, "plan": "free"}
        mock.reply(api_error(402, "plan_limit_exceeded", "upgrade", details=details))
        with pytest.raises(PlanLimitError) as exc:
            client.webhooks.create("https://x")
        e = exc.value
        assert (e.limit, e.max, e.used, e.plan) == ("webhooks", 3, 3, "free")
        assert e.details == details

    def test_not_local(self, client: Staqly, mock: Mock) -> None:
        mock.reply(api_error(403, "not_local", "local mode only accepts connections from this machine"))
        with pytest.raises(PermissionDeniedError) as exc:
            client.posts.list()
        assert exc.value.is_not_local

    def test_rate_limit_and_non_json(self, client: Staqly, mock: Mock) -> None:
        mock.reply(
            httpx.Response(429, text="slow down", headers={"Retry-After": "120", "X-Request-Id": "r1"})
        )
        with pytest.raises(RateLimitError) as exc:
            client.posts.list()
        assert exc.value.retry_after == 120
        assert exc.value.request_id == "r1"
        assert exc.value.code == "http_error"
        assert exc.value.body == "slow down"

    def test_connection_error(self, mock: Mock) -> None:
        mock.reply(httpx.ConnectError("refused"))
        with Staqly(base_url=BASE, max_retries=0) as c, pytest.raises(APIConnectionError):
            c.posts.list()

    def test_timeout(self, mock: Mock) -> None:
        mock.reply(httpx.ReadTimeout("slow"))
        with Staqly(base_url=BASE, max_retries=0) as c, pytest.raises(staqly.TimeoutError) as exc:
            c.posts.list()
        assert isinstance(exc.value, APIConnectionError)
        assert isinstance(exc.value, TimeoutError)  # builtins.TimeoutError

    async def test_timeout_async(self, mock: Mock) -> None:
        mock.reply(httpx.ConnectTimeout("slow"))
        async with AsyncStaqly(base_url=BASE, max_retries=0) as c:
            with pytest.raises(staqly.TimeoutError):
                await c.posts.list()


def _retry_after_0(status: int, code: str = "x") -> httpx.Response:
    return httpx.Response(
        status, json={"error": {"code": code, "message": "m"}}, headers={"Retry-After": "0"}
    )


class TestRetries:
    def test_retries_5xx_with_same_idempotency_key(self, client: Staqly, mock: Mock) -> None:
        col = {
            "id": "col_1",
            "name": "a",
            "created_at": "2026-01-01T00:00:00Z",
            "updated_at": "2026-01-01T00:00:00Z",
        }
        mock.reply(_retry_after_0(503), _retry_after_0(500), jsonr(col, 201))
        assert client.collections.create("a").id == "col_1"
        keys = {r.headers["idempotency-key"] for r in mock.requests}
        assert len(mock.requests) == 3
        assert len(keys) == 1
        assert next(iter(keys)).startswith("sdk-")
        assert body(mock.last) == {"name": "a"}

    async def test_retries_async(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(_retry_after_0(502), jsonr({"data": []}))
        await aclient.plans.list()
        assert len(mock.requests) == 2

    def test_caller_idempotency_key(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(post(), 201))
        client.posts.create("Posts", title="Post", content="x", request_options={"idempotency_key": "job-42"})
        assert mock.last.headers["idempotency-key"] == "job-42"

    def test_get_has_no_idempotency_key(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr({"data": []}))
        client.posts.list()
        assert "idempotency-key" not in mock.last.headers

    def test_gives_up_after_max_retries(self, client: Staqly, mock: Mock) -> None:
        mock.reply(_retry_after_0(502))
        with pytest.raises(InternalServerError):
            client.posts.list()
        assert len(mock.requests) == 3

    def test_per_request_max_retries(self, client: Staqly, mock: Mock) -> None:
        mock.reply(_retry_after_0(502))
        with pytest.raises(InternalServerError):
            client.posts.list(request_options={"max_retries": 0})
        assert len(mock.requests) == 1

    def test_no_retry_on_4xx(self, client: Staqly, mock: Mock) -> None:
        mock.reply(api_error(409, "conflict"))
        with pytest.raises(ConflictError):
            client.posts.publish("pst_1")
        assert len(mock.requests) == 1

    def test_retries_idempotency_in_progress(self, client: Staqly, mock: Mock) -> None:
        mock.reply(_retry_after_0(409, "idempotency_in_progress"), jsonr(post()))
        client.posts.publish("pst_1")
        assert len(mock.requests) == 2

    def test_429_retry_after(self, client: Staqly, mock: Mock) -> None:
        mock.reply(_retry_after_0(429), jsonr({"data": []}))
        client.posts.list()
        assert len(mock.requests) == 2

    def test_long_retry_after_is_not_waited(self, client: Staqly, mock: Mock) -> None:
        mock.reply(httpx.Response(429, json={}, headers={"Retry-After": "3600"}))
        with pytest.raises(RateLimitError):
            client.posts.list()
        assert len(mock.requests) == 1

    def test_network_errors_are_retried(self, mock: Mock) -> None:
        mock.reply(httpx.ConnectError("refused"), jsonr({"data": []}))
        with Staqly(base_url=BASE, max_retries=1) as c:
            c.plans.list()
        assert len(mock.requests) == 2

    def test_multipart_not_retried_on_5xx(self, client: Staqly, mock: Mock) -> None:
        mock.reply(_retry_after_0(503))
        with pytest.raises(InternalServerError):
            client.media.upload(b"\x01\x02")
        assert len(mock.requests) == 1
        assert "idempotency-key" not in mock.last.headers

    def test_multipart_retried_on_429(self, client: Staqly, mock: Mock) -> None:
        media = {"id": "med_1", "mime_type": "image/png", "size_bytes": 2, "original_name": "a", "url": "u"}
        mock.reply(_retry_after_0(429), jsonr(media, 201))
        client.media.upload(b"\x01\x02")
        assert len(mock.requests) == 2

    def test_no_automatic_key_without_retries(self, mock: Mock) -> None:
        mock.reply(jsonr(post(), 201))
        with Staqly(base_url=BASE, max_retries=0) as c:
            c.posts.create("x", title="Post", content="t")
        assert "idempotency-key" not in mock.last.headers
