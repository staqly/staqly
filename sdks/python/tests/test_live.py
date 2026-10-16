"""Live integration tests against a real server. Skipped unless STAQLY_LIVE=1.

    STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3333 pytest tests/test_live.py   # local mode (keyless)
    STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3334 pytest tests/test_live.py   # cloud: new workspace
    STAQLY_LIVE=1 STAQLY_API_URL=https://... STAQLY_API_KEY=stk_... pytest tests/test_live.py  # your key

Webhook signatures are verified end to end when the server may deliver to
loopback (WEBHOOK_ALLOW_LOOPBACK=true); otherwise only the delivery record is.
"""

from __future__ import annotations

import asyncio
import base64
import json
import os
import threading
import time
import uuid
import zipfile
from collections.abc import Iterator
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from typing import Any

import httpx
import pytest

from staqly import (
    AsyncStaqly,
    AuthenticationError,
    ConflictError,
    NotFoundError,
    PermissionDeniedError,
    PlanLimitError,
    Staqly,
    ValidationError,
    verify_webhook,
)

pytestmark = [
    pytest.mark.live,
    pytest.mark.skipif(os.environ.get("STAQLY_LIVE") != "1", reason="set STAQLY_LIVE=1 to run live tests"),
]

PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
)
SCOPES = [
    "collections:read",
    "collections:write",
    "items:read",
    "items:write",
    "items:review",
    "actions:execute",
    "webhooks:manage",
    "watchers:manage",
    "channels:manage",
]


class Env:
    mode: str
    api_key: str | None
    admin: Staqly | None
    name: str
    col_id: str


@pytest.fixture(scope="module")
def env() -> Iterator[Env]:
    e = Env()
    e.admin = None
    e.api_key = os.environ.get("STAQLY_API_KEY")
    with Staqly(api_key="") as probe:
        e.mode = probe.request("GET", "/v1/setup")["mode"]
        if e.mode == "cloud" and not e.api_key:
            # Signup returns a default key (not every scope) plus a browser session;
            # use the session to mint a key with the scopes these tests need.
            r = httpx.post(probe.base_url + "/v1/workspaces", json={"name": f"sdk-py-{uuid.uuid4().hex[:6]}"})
            assert r.status_code == 201, r.text
            created = r.json()
            assert created["api_key"]["key"].startswith("stk_")
            cookie = "; ".join(f"{k}={v}" for k, v in r.cookies.items())
            e.admin = Staqly(
                api_key="", default_headers={"Cookie": cookie, "X-CSRF-Token": created["csrf_token"]}
            )
            e.api_key = e.admin.api_keys.create("sdk live", scopes=SCOPES).key
    e.name = f"SDK-PY {uuid.uuid4().hex[:8]}"
    with Staqly(api_key=e.api_key or "") as c:
        e.col_id = c.collections.create(e.name, description="SDK live test").id
        yield e
        c.collections.delete(e.col_id, confirm=e.name)
    if e.admin is not None:
        e.admin.close()


@pytest.fixture
def c(env: Env) -> Iterator[Staqly]:
    with Staqly(api_key=env.api_key or "") as client:
        yield client


@pytest.fixture
def receiver() -> Iterator[tuple[str, list[tuple[dict[str, str], bytes]]]]:
    received: list[tuple[dict[str, str], bytes]] = []

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:
            data = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            received.append((dict(self.headers.items()), data))
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")

        def log_message(self, *args: Any) -> None:
            pass

    server = HTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    yield f"http://127.0.0.1:{server.server_address[1]}/hook", received
    server.shutdown()


def test_health_whoami_usage(c: Staqly, env: Env) -> None:
    assert c.health().status == "ok"
    assert c.whoami().mode == env.mode
    assert c.workspace.get().id.startswith("ws_")
    usage = c.workspace.usage()
    assert usage.plan.id == ("unlocked" if env.mode == "local" else "free")
    assert "free" in [p.id for p in c.plans.list()]


def test_collections(c: Staqly, env: Env) -> None:
    assert c.collections.get(env.name).id == env.col_id
    assert c.collections.update(env.col_id, color="#00ff00").color == "#00ff00"
    assert env.col_id in [x.id for x in c.collections.list()]
    with pytest.raises(NotFoundError):
        c.collections.get("no such collection 123")


def test_instagram_avatar_flow(c: Staqly, env: Env) -> None:
    """Upload an image → wait while a reviewer approves elsewhere → report the publication."""
    item = c.items.upload(
        env.name,
        PNG,
        filename="avatar.png",
        title="New avatar",
        content="Caption: hello",
        metadata={"account": "@acme"},
        editable=["content", "metadata.caption"],
        expires_in="24h",
    )
    assert (item.type, item.status) == ("image", "pending")
    assert item.media is not None and item.media.mime_type == "image/png"
    assert item.expires_at is not None
    assert c.media.download(item.media.id) == PNG

    def reviewer() -> None:
        time.sleep(1.5)
        with Staqly(api_key=env.api_key or "") as other:
            other.items.approve(item.id, feedback="love it", edits={"content": "Caption: hello!"})

    t = threading.Thread(target=reviewer)
    t.start()
    started = time.monotonic()
    decided = c.items.wait_for_decision(item.id, timeout=30)
    t.join()
    assert decided.status == "approved"
    assert decided.content == "Caption: hello!"
    assert decided.feedback == "love it"
    assert time.monotonic() - started < 15

    done = c.items.report_result(
        item.id, status="completed", result={"post_url": "https://instagram.com/p/1"}
    )
    assert done.status == "completed"
    assert done.metadata["result"] == {"post_url": "https://instagram.com/p/1"}

    approved = next(e for e in c.items.history(item.id).data if e.type == "item.approved")
    assert "content" in approved.data["diff"]
    with pytest.raises(ConflictError):
        c.items.report_result(item.id, status="completed")


def test_unarchive(c: Staqly, env: Env) -> None:
    it = c.items.create(env.col_id, title="to archive", content="x")
    c.items.approve(it.id)
    c.items.update(it.id, status="archived")
    back = c.items.unarchive(it.id)
    assert back.status == "approved" and back.archived_at is None
    with pytest.raises(ConflictError):
        c.items.unarchive(it.id)


def test_publication_via_update(c: Staqly, env: Env) -> None:
    it = c.items.create(env.col_id, title="post 2", content="x")
    c.items.approve(it.id)
    done = c.items.update(it.id, status="completed", metadata_patch={"post_url": "https://x/2"})
    assert done.status == "completed"
    assert done.metadata["post_url"] == "https://x/2"


def test_editable_and_conflicts(c: Staqly, env: Env) -> None:
    it = c.items.create(env.name, title="locked", content="body", editable=["title"])
    with pytest.raises(ValidationError) as exc:
        c.items.approve(it.id, edits={"content": "changed"})
    assert exc.value.code == "not_editable"
    assert c.items.reject(it.id, feedback="too short").status == "rejected"
    with pytest.raises(ConflictError):
        c.items.approve(it.id)
    assert c.items.wait_for_decision(it.id, timeout=5).status == "rejected"


def test_list_search_iterate_bulk(c: Staqly, env: Env) -> None:
    made = [
        c.items.create(env.name, title=f"zebra note {i}", content=f"stripes {i}", source="sdk-py")
        for i in range(5)
    ]
    page = c.items.list(collection=env.name, limit=2)
    assert len(page.data) == 2 and page.next_cursor
    ids = [i.id for i in c.items.iterate(collection=env.name, source="sdk-py", limit=2)]
    assert sorted(ids) == sorted(i.id for i in made)
    assert len(c.items.search("zebr", collection_id=env.col_id).data) == 5
    bulk = c.items.bulk_action([made[0].id, made[1].id], "reject", feedback="bulk")
    assert all(r.ok for r in bulk)
    assert "approve" in [a.name for a in c.items.list_actions(made[2].id)]
    c.items.update(made[2].id, title="renamed")
    assert c.items.versions(made[2].id)
    c.items.delete(made[4].id)
    with pytest.raises(NotFoundError):
        c.items.get(made[4].id)


def test_events(c: Staqly, env: Env) -> None:
    assert len(c.events.list(collection_id=env.col_id, type="item.created", limit=3).data) == 3
    events = list(c.events.iterate(collection_id=env.col_id, type="item.created", limit=3))
    assert len(events) >= 8
    assert {e.type for e in events} == {"item.created"}


def test_webhooks_end_to_end(
    c: Staqly, env: Env, receiver: tuple[str, list[tuple[dict[str, str], bytes]]]
) -> None:
    url, received = receiver
    hook = c.webhooks.create(url, events=["*"], description="sdk-py live")
    assert hook.secret.startswith("whsec_")
    assert c.webhooks.get(hook.id).url == url
    delivery = c.webhooks.test(hook.id)
    assert delivery.event_type == "webhook.test"
    status = "pending"
    for _ in range(40):
        time.sleep(0.25)
        status = next(d.status for d in c.webhooks.deliveries.list(webhook_id=hook.id) if d.id == delivery.id)
        if status != "pending":
            break
    if status == "succeeded":
        headers, raw = next((h, b) for h, b in received if h.get("X-Staqly-Delivery") == delivery.id)
        event = verify_webhook(hook.secret, raw, headers)
        assert event.type == "webhook.test"
        assert event.delivery_id == delivery.id
        assert c.webhooks.deliveries.retry(delivery.id).status == "pending"
    else:
        pytest.skip(f"delivery {status}: server does not deliver to loopback")
    rotated = c.webhooks.rotate_secret(hook.id)
    assert rotated.secret != hook.secret
    assert c.webhooks.update(hook.id, active=False).active is False
    c.webhooks.delete(hook.id)


def test_watchers(c: Staqly, env: Env) -> None:
    w = c.watchers.create(
        "example", kind="url_diff", url="https://example.com/", collection=env.name, every="1h"
    )
    assert w.collection_id == env.col_id
    assert c.watchers.pause(w.id).status == "paused"
    assert c.watchers.resume(w.id).status != "paused"
    assert w.id in [x.id for x in c.watchers.list()]
    c.watchers.delete(w.id)


def test_notification_channels(
    c: Staqly, env: Env, receiver: tuple[str, list[tuple[dict[str, str], bytes]]]
) -> None:
    url, _ = receiver
    ch = c.notification_channels.create(
        "sdk phone",
        type="ntfy",
        config={
            "topic": f"sdk-{uuid.uuid4().hex[:6]}",
            "server_url": url.removesuffix("/hook"),
            "access_token": "secret-token",
        },
        events=["item.created"],
        collection_ids=[env.col_id],
    )
    assert ch.type == "ntfy"
    assert "secret-token" not in json.dumps(ch.config)  # secrets come back masked
    assert c.notification_channels.get(ch.id).name == "sdk phone"
    assert ch.id in [x.id for x in c.notification_channels.list()]
    assert isinstance(c.notification_channels.test(ch.id).ok, bool)
    assert isinstance(c.notification_channels.deliveries(ch.id, limit=10), list)
    assert c.notification_channels.disable(ch.id).enabled is False
    assert c.notification_channels.update(ch.id, digest_window_seconds=60).digest_window_seconds == 60
    c.notification_channels.delete(ch.id)
    with pytest.raises(NotFoundError):
        c.notification_channels.get(ch.id)


def test_exports_and_purge(c: Staqly, env: Env, tmp_path: Path) -> None:
    events = list(c.exports.events(collection_id=env.col_id, type="item.created"))
    assert len(events) >= 8
    assert events[0].created_at <= events[-1].created_at  # oldest first
    csv = c.exports.save_events(tmp_path / "events.csv", format="csv", collection_id=env.col_id)
    assert csv.read_text().startswith("created_at,id,type")

    owner = env.admin or c  # workspace export and purge need admin
    job = owner.exports.export_workspace(tmp_path / "ws.zip", timeout=60, poll_interval=0.25)
    assert job.status == "ready"
    assert zipfile.is_zipfile(tmp_path / "ws.zip")
    assert "workspace.json" in zipfile.ZipFile(tmp_path / "ws.zip").namelist()

    doomed = c.items.create(env.col_id, title="to purge")
    purged = owner.items.purge(doomed.id)
    assert purged.item_id == doomed.id
    assert purged.events_deleted >= 1
    with pytest.raises(NotFoundError):
        c.items.get(doomed.id)
    assert c.ready().status == "ok"


async def test_async_exports(env: Env) -> None:
    async with AsyncStaqly(api_key=env.api_key or "") as ac:
        n = 0
        async for e in ac.exports.events(collection_id=env.col_id, type_prefix="item."):
            assert e.type.startswith("item.")
            n += 1
        assert n > 0


async def test_async_wait_and_approve_concurrently(env: Env) -> None:
    async with AsyncStaqly(api_key=env.api_key or "") as ac:
        item = await ac.items.create(env.col_id, title="async gate", content="draft")

        async def approve_later() -> None:
            await asyncio.sleep(1.0)
            await ac.items.approve(item.id, feedback="async ok")

        decided, _ = await asyncio.gather(ac.items.wait_for_decision(item.id, timeout=30), approve_later())
        assert decided.status == "approved"
        assert decided.feedback == "async ok"
        ids = [i.id async for i in ac.items.iterate(collection_id=env.col_id, status="approved")]
        assert item.id in ids
        done = await ac.items.report_result(item.id, status="failed", error="network down")
        assert done.status == "failed"
        assert done.metadata["error"] == "network down"


def test_requires_approval_decorator(c: Staqly, env: Env) -> None:
    @c.requires_approval(env.col_id, title=lambda caption: f"Publish {caption}?", expires_in=120)
    def publish(caption: str) -> dict[str, str]:
        return {"post_url": f"https://x/{caption}"}

    def approve_pending() -> None:
        for _ in range(50):
            time.sleep(0.2)
            with Staqly(api_key=env.api_key or "") as other:
                page = other.items.list(collection_id=env.col_id, q="sunset", status="pending")
                for it in page.data:
                    other.items.approve(it.id)
                    return

    t = threading.Thread(target=approve_pending)
    t.start()
    assert publish("sunset") == {"post_url": "https://x/sunset"}
    t.join()
    found = c.items.search("sunset", collection_id=env.col_id).data[0]
    assert found.status == "completed"
    assert found.metadata["result"] == {"post_url": "https://x/sunset"}


def test_cloud_plan_limits_and_scopes(c: Staqly, env: Env) -> None:
    if env.mode != "cloud":
        pytest.skip("cloud only")
    hooks = []
    try:
        limit = c.workspace.usage().plan.limits.webhooks
        for _ in range(len(c.webhooks.list()), limit):
            hooks.append(c.webhooks.create("https://example.com/hook"))
        with pytest.raises(PlanLimitError) as exc:
            c.webhooks.create("https://example.com/hook")
        e = exc.value
        assert (e.status, e.limit, e.max, e.plan) == (402, "webhooks", limit, "free")
    finally:
        for h in hooks:
            c.webhooks.delete(h.id)
    with pytest.raises(PermissionDeniedError):
        c.api_keys.list()
    if env.admin is not None:
        assert len(env.admin.api_keys.list()) >= 2
        extra = env.admin.api_keys.create("temp", scopes=["items:read"], expires_at="2099-01-01")
        rotated = env.admin.api_keys.rotate(extra.id)
        assert rotated.key and rotated.key.startswith("stk_") and rotated.id != extra.id
        with Staqly(api_key=rotated.key) as ro:
            with pytest.raises(PermissionDeniedError):
                ro.collections.create("nope")
            env.admin.api_keys.revoke(rotated.id)
            with pytest.raises(AuthenticationError):
                ro.items.list()
        assert env.admin.workspace.rename("SDK live renamed").name == "SDK live renamed"
