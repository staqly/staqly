from __future__ import annotations

import json
from collections.abc import Iterator
from typing import Any

import httpx
import pytest
import respx

from staqly import AsyncStaqly, Staqly

BASE = "http://api.test"


def item(**overrides: Any) -> dict[str, Any]:
    data: dict[str, Any] = {
        "id": "itm_1",
        "collection_id": "col_1",
        "title": "t",
        "type": "text",
        "content": "",
        "url": "",
        "media": None,
        "metadata": {},
        "status": "pending",
        "source": "",
        "external_ref": "",
        "feedback": "",
        "version": 1,
        "decided_at": None,
        "expires_at": None,
        "editable": [],
        "created_at": "2026-01-01T00:00:00.000000Z",
        "updated_at": "2026-01-01T00:00:00.000000Z",
    }
    data.update(overrides)
    return data


def jsonr(body: Any, status: int = 200, headers: dict[str, str] | None = None) -> httpx.Response:
    return httpx.Response(status, json=body, headers=headers)


def api_error(status: int, code: str, message: str = "boom", **extra: Any) -> httpx.Response:
    return jsonr({"error": {"code": code, "message": message, **extra}}, status)


class Mock:
    """Catch-all respx route answering from a queue (the last reply repeats)."""

    def __init__(self, router: respx.MockRouter) -> None:
        self._queue: list[httpx.Response | Exception] = [jsonr({})]
        self.route = router.route()
        self.route.side_effect = self._next

    def reply(self, *responses: httpx.Response | Exception) -> Mock:
        self._queue = list(responses)
        return self

    def _next(self, request: httpx.Request) -> httpx.Response:
        nxt = self._queue.pop(0) if len(self._queue) > 1 else self._queue[0]
        if isinstance(nxt, Exception):
            raise nxt
        return httpx.Response(nxt.status_code, headers=nxt.headers, content=nxt.content)

    @property
    def requests(self) -> list[httpx.Request]:
        return [c.request for c in self.route.calls]

    @property
    def last(self) -> httpx.Request:
        return self.requests[-1]


def body(request: httpx.Request) -> Any:
    return json.loads(request.content) if request.content else None


def query(request: httpx.Request) -> dict[str, str]:
    return dict(request.url.params)


@pytest.fixture
def mock() -> Iterator[Mock]:
    with respx.mock(assert_all_mocked=True, assert_all_called=False) as router:
        yield Mock(router)


@pytest.fixture
def client(mock: Mock) -> Iterator[Staqly]:
    with Staqly(base_url=BASE, api_key="stk_test", max_retries=2) as c:
        yield c


@pytest.fixture
async def aclient(mock: Mock) -> Any:
    async with AsyncStaqly(base_url=BASE, api_key="stk_test", max_retries=2) as c:
        yield c
