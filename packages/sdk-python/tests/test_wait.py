from __future__ import annotations

from typing import Any

import pytest

from staqly import AsyncStaqly, ConflictError, RejectedError, Staqly, WaitTimeoutError

from .conftest import Mock, body, item, jsonr


def waited(**kw: Any) -> Any:
    return jsonr({"decided": kw.pop("decided", True), "item": item(**kw)})


class TestWaitForDecision:
    def test_long_polls_until_decided(self, client: Staqly, mock: Mock) -> None:
        mock.reply(waited(decided=False), waited(status="approved"))
        seen: list[str] = []
        out = client.items.wait_for_decision("itm_1", timeout=120, on_poll=lambda i: seen.append(i.status))
        assert out.status == "approved"
        assert seen == ["pending", "approved"]
        assert [str(r.url).removeprefix("http://api.test") for r in mock.requests] == [
            "/v1/items/itm_1/wait?timeout=60",
            "/v1/items/itm_1/wait?timeout=60",
        ]

    def test_asks_only_for_remaining_time(self, client: Staqly, mock: Mock) -> None:
        mock.reply(waited(status="rejected"))
        client.items.wait_for_decision("itm_1", timeout=4.5)
        assert mock.last.url.params["timeout"] == "5"

    def test_http_timeout_outlasts_the_long_poll(self, client: Staqly, mock: Mock) -> None:
        mock.reply(waited(status="approved"))
        client.items.wait_for_decision("itm_1", timeout=60)
        assert mock.last.extensions["timeout"]["read"] == 75.0

    def test_expired_counts_as_decided(self, client: Staqly, mock: Mock) -> None:
        mock.reply(waited(status="expired"))
        assert client.items.wait_for_decision("itm_1").status == "expired"

    def test_timeout(self, client: Staqly, mock: Mock) -> None:
        mock.reply(waited(decided=False))
        with pytest.raises(WaitTimeoutError) as exc:
            client.items.wait_for_decision("itm_1", timeout=0.01)
        assert exc.value.item_id == "itm_1"
        assert exc.value.item is not None and exc.value.item.status == "pending"
        assert isinstance(exc.value, TimeoutError)

    async def test_async(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(waited(decided=False), waited(status="approved"))
        assert (await aclient.items.wait_for_decision("itm_1")).status == "approved"
        mock.reply(waited(decided=False))
        with pytest.raises(WaitTimeoutError):
            await aclient.items.wait_for_decision("itm_1", timeout=0.01)


class TestRequestApproval:
    def test_creates_and_waits(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(item(), 201), waited(status="rejected", feedback="no"))
        d = client.request_approval("Outbox", title="Send?", content="hi", expires_in=3600)
        assert (d.approved, d.expired, d.status, d.feedback) == (False, False, "rejected", "no")
        assert body(mock.requests[0]) == {"title": "Send?", "content": "hi", "expires_in": 3600}

    def test_expired(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(item(), 201), waited(status="expired"))
        d = client.request_approval("Outbox", title="Send?", expires_in="1h")
        assert d.expired and not d.approved

    def test_already_decided_by_rule(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(item(status="approved"), 201))
        assert client.request_approval("Outbox", title="x").approved
        assert len(mock.requests) == 1

    async def test_async(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(jsonr(item(), 201), waited(status="approved"))
        assert (await aclient.request_approval("Outbox", title="x")).approved


class TestRequiresApproval:
    def test_runs_after_approval_and_reports(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(item(), 201), waited(status="approved"), jsonr(item(status="completed")))

        @client.requires_approval(
            "Instagram",
            title=lambda caption, n: f"Publish {caption}?",
            metadata=lambda caption, n: {"n": n},
            editable=["content"],
        )
        def publish(caption: str, n: int) -> dict[str, str]:
            return {"url": f"https://x/{caption}/{n}"}

        assert publish("hello", 2) == {"url": "https://x/hello/2"}
        assert publish.__name__ == "publish"
        assert body(mock.requests[0]) == {
            "title": "Publish hello?",
            "content": {"args": ["hello", 2], "kwargs": {}},
            "metadata": {"n": 2},
            "editable": ["content"],
        }
        assert mock.requests[2].url.path == "/v1/items/itm_1/result"
        assert body(mock.requests[2]) == {"status": "completed", "result": {"url": "https://x/hello/2"}}

    def test_rejection_raises_without_running(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(item(), 201), waited(status="rejected", feedback="nope"))
        ran = []

        @client.requires_approval("X", title="t")
        def act() -> None:
            ran.append(1)

        with pytest.raises(RejectedError) as exc:
            act()
        assert exc.value.feedback == "nope"
        assert exc.value.status == "rejected"
        assert ran == []

    def test_failure_is_reported_and_reraised(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(item(), 201), waited(status="approved"), jsonr(item(status="failed")))

        @client.requires_approval("X", title="t", content="static")
        def act() -> None:
            raise RuntimeError("API down")

        with pytest.raises(RuntimeError, match="API down"):
            act()
        assert body(mock.requests[0])["content"] == "static"
        assert body(mock.requests[2]) == {"status": "failed", "error": "API down"}

    def test_report_can_be_disabled(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(item(), 201), waited(status="approved"))

        @client.requires_approval("X", title="t", report=False)
        def act() -> int:
            return 1

        assert act() == 1
        assert len(mock.requests) == 2

    def test_reporting_conflicts_surface(self, client: Staqly, mock: Mock) -> None:
        mock.reply(
            jsonr(item(), 201),
            waited(status="approved"),
            jsonr({"error": {"code": "conflict", "message": "x"}}, 409),
        )

        @client.requires_approval("X", title="t")
        def act() -> int:
            return 1

        with pytest.raises(ConflictError):
            act()

    async def test_async_decorator(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(jsonr(item(), 201), waited(status="approved"), jsonr(item(status="completed")))

        @aclient.requires_approval("X", title=lambda to: f"Email {to}?")
        async def send(to: str) -> str:
            return f"sent to {to}"

        assert await send("bob") == "sent to bob"
        assert body(mock.requests[0])["title"] == "Email bob?"
