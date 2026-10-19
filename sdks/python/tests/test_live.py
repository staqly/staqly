"""Live integration tests against a real server. Skipped unless STAQLY_LIVE=1.

    STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3333 pytest tests/test_live.py   # local mode (keyless)
    STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3334 pytest tests/test_live.py   # cloud: new workspace
    STAQLY_LIVE=1 STAQLY_API_URL=https://... STAQLY_API_KEY=stk_... pytest tests/test_live.py  # your key

Publishing goes through a channel of provider ``custom`` (your own automation), so no platform
credentials are needed: the test reports the result itself.
"""

from __future__ import annotations

import base64
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
    ConflictError,
    NotFoundError,
    PermissionDeniedError,
    PlanLimitError,
    Staqly,
    verify_webhook,
)
from staqly.models import ScheduleRule

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
    "collections:delete",
    "posts:read",
    "posts:write",
    "posts:delete",
    "posts:publish",
    "channels:read",
    "channels:manage",
    "schedules:manage",
    "media:read",
    "media:write",
    "webhooks:manage",
    "alerts:manage",
    "events:read",
]


class Env:
    mode: str
    api_key: str | None
    admin: Staqly | None
    name: str
    col_id: str
    channel_id: str


@pytest.fixture(scope="module")
def env() -> Iterator[Env]:
    e = Env()
    e.admin = None
    e.api_key = os.environ.get("STAQLY_API_KEY")
    with Staqly(api_key="") as probe:
        e.mode = probe.request("GET", "/v1/setup")["mode"]
        if e.mode == "cloud" and not e.api_key:
            r = httpx.post(probe.base_url + "/v1/workspaces", json={"name": f"sdk-py-{uuid.uuid4().hex[:6]}"})
            assert r.status_code == 201, r.text
            created = r.json()
            cookie = "; ".join(f"{k}={v}" for k, v in r.cookies.items())
            e.admin = Staqly(
                api_key="", default_headers={"Cookie": cookie, "X-CSRF-Token": created["csrf_token"]}
            )
            e.api_key = e.admin.api_keys.create("sdk live", scopes=SCOPES).key
    e.name = f"SDK-PY {uuid.uuid4().hex[:8]}"
    with Staqly(api_key=e.api_key or "") as c:
        e.col_id = c.collections.create(
            e.name, description="SDK live test", processing_timeout_seconds=600
        ).id
        e.channel_id = c.channels.connect("custom", name=e.name).id
        yield e
        c.channels.remove(e.channel_id)
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
    if env.mode == "local":
        assert usage.plan.id == "unlocked"
    assert "free" in [p.id for p in c.plans.list()]


def test_collections_and_channels(c: Staqly, env: Env) -> None:
    assert c.collections.get(env.name).id == env.col_id
    assert c.collections.update(env.col_id, color="#00ff00").color == "#00ff00"
    with pytest.raises(NotFoundError):
        c.collections.get("no such collection 123")
    assert "custom" in [p.id for p in c.providers.list()]
    ch = c.channels.get(env.channel_id)
    assert ch.provider == "custom" and ch.status == "connected"
    assert c.channels.test(env.channel_id).ok


def test_publish_flow(c: Staqly, env: Env) -> None:
    """Draft with media → schedule → cancel → publish → our automation reports the result."""
    media = c.media.upload(PNG, filename="a.png", content_type="image/png")
    draft = c.posts.create(
        env.name,
        content="Hello from the SDK",
        media=[media.id],
        targets=[{"channel_id": env.channel_id}],
        tags=["sdk"],
        source="sdk-py",
    )
    assert draft.status == "draft" and draft.media_items[0].id == media.id
    scheduled = c.posts.schedule(draft.id, "2099-01-01T09:00:00Z")
    assert scheduled.status == "scheduled"
    assert draft.id in [p.id for p in c.posts.list(collection_id=env.col_id, status="scheduled").data]
    assert c.posts.cancel(draft.id).status == "draft"

    assert c.posts.publish(draft.id).status == "publishing"
    for _ in range(40):
        if any(w.kind == "target" and w.post is not None and w.post.id == draft.id for w in c.work.list()):
            break
        time.sleep(0.25)
    else:
        pytest.fail("the target never showed up under /v1/work")
    done = c.posts.report_result(draft.id, env.channel_id, status="published", url="https://example.com/p/1")
    assert done.is_published and done.targets[0].external_url == "https://example.com/p/1"
    assert c.posts.wait_for_published(draft.id, timeout=5).is_published
    assert "post.published" in {e.type for e in c.posts.history(draft.id).data}
    with pytest.raises(ConflictError):
        c.posts.update(draft.id, content="late edit")
    copy = c.posts.duplicate(draft.id)
    assert copy.status == "draft"
    c.posts.delete(copy.id)
    with pytest.raises(NotFoundError):
        c.posts.get(copy.id)


def test_failed_target_retry_iterate_tags(c: Staqly, env: Env) -> None:
    p = c.posts.create(
        env.col_id,
        content="will fail",
        targets=[{"channel_id": env.channel_id}],
        publish_now=True,
        tags=["sdk"],
    )
    assert c.posts.report_result(p.id, env.channel_id, status="failed", error="nope").is_failed
    assert c.posts.retry_target(p.id, env.channel_id).status == "publishing"
    c.posts.report_result(p.id, env.channel_id, status="published")
    assert len(list(c.posts.iterate(collection_id=env.col_id, tag="sdk", limit=1))) >= 2
    assert "sdk" in [t.name for t in c.posts.tags()]
    assert c.posts.preview(p.id).targets[0].ok


def test_schedules_and_jobs(c: Staqly, env: Env) -> None:
    s = c.schedules.create(
        env.col_id,
        name="SDK mornings",
        kind="recurring",
        rule=ScheduleRule(times=["09:00"], weekdays=[1, 2, 3, 4, 5], tz="Europe/Warsaw"),
        channels=[env.channel_id],
        pick="next",
        source="agent",
    )
    assert s.next_run_at is not None
    assert s.id in [x.id for x in c.schedules.list(collection_id=env.col_id)]
    assert c.schedules.disable(s.id).enabled is False
    assert c.schedules.update(s.id, rule={"every_minutes": 360}).rule.every_minutes == 360
    c.schedules.run(s.id)
    assert c.jobs.stats().queued >= 0
    assert len(c.jobs.list(kind="run_schedule", limit=5).data) >= 1
    c.schedules.delete(s.id)
    with pytest.raises(NotFoundError):
        c.schedules.get(s.id)


def test_events(c: Staqly, env: Env) -> None:
    assert len(c.events.list(collection_id=env.col_id, type="post.created", limit=2).data) == 2
    events = list(c.events.iterate(collection_id=env.col_id, type_prefix="post.", limit=3))
    assert len(events) >= 4
    assert all(e.type.startswith("post.") for e in events)


def test_webhooks_end_to_end(
    c: Staqly, env: Env, receiver: tuple[str, list[tuple[dict[str, str], bytes]]]
) -> None:
    url, received = receiver
    hook = c.webhooks.create(url, events=["*"], description="sdk-py live")
    assert hook.secret.startswith("whsec_")
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
    else:
        pytest.skip(f"delivery {status}: server does not deliver to loopback")
    assert c.webhooks.rotate_secret(hook.id).secret != hook.secret
    c.webhooks.delete(hook.id)


def test_alerts(c: Staqly, env: Env, receiver: tuple[str, list[tuple[dict[str, str], bytes]]]) -> None:
    url, _ = receiver
    a = c.alerts.create(
        "sdk phone",
        type="ntfy",
        config={
            "topic": f"sdk-{uuid.uuid4().hex[:6]}",
            "server_url": url.removesuffix("/hook"),
            "access_token": "s3",
        },
        events=["post.published"],
        collection_ids=[env.col_id],
    )
    assert "s3" not in str(a.config)  # secrets come back masked
    assert c.alerts.get(a.id).name == "sdk phone"
    assert isinstance(c.alerts.test(a.id).ok, bool)
    assert isinstance(c.alerts.deliveries(a.id, limit=10), list)
    assert c.alerts.disable(a.id).enabled is False
    c.alerts.delete(a.id)
    with pytest.raises(NotFoundError):
        c.alerts.get(a.id)


def test_exports_and_purge(c: Staqly, env: Env, tmp_path: Path) -> None:
    events = list(c.exports.events(collection_id=env.col_id, type="post.created"))
    assert len(events) >= 2
    assert events[0].created_at <= events[-1].created_at  # oldest first
    owner = env.admin or c  # workspace export and purge need admin
    job = owner.exports.export_workspace(tmp_path / "ws.zip", timeout=60, poll_interval=0.25)
    assert job.status == "ready"
    assert zipfile.is_zipfile(tmp_path / "ws.zip")
    doomed = c.posts.create(env.col_id, content="to purge")
    assert owner.posts.purge(doomed.id).post_id == doomed.id
    with pytest.raises(NotFoundError):
        c.posts.get(doomed.id)
    assert c.ready().status == "ok"


async def test_async_client(env: Env) -> None:
    async with AsyncStaqly(api_key=env.api_key or "") as ac:
        p = await ac.posts.create(
            env.col_id, content="async", targets=[{"channel_id": env.channel_id}], publish_now=True
        )
        await ac.posts.report_result(p.id, env.channel_id, status="published")
        assert (await ac.posts.wait_for_published(p.id, timeout=5)).is_published
        n = 0
        async for e in ac.exports.events(collection_id=env.col_id, type_prefix="post."):
            assert e.type.startswith("post.")
            n += 1
        assert n > 0


def test_cloud_plan_limits_and_scopes(c: Staqly, env: Env) -> None:
    if env.mode != "cloud":
        pytest.skip("cloud only")
    with pytest.raises(PermissionDeniedError):
        c.api_keys.list()
    if env.admin is not None:
        extra = env.admin.api_keys.create("temp", scopes=["posts:read"], expires_at="2099-01-01")
        with Staqly(api_key=extra.key) as ro, pytest.raises(PermissionDeniedError):
            ro.posts.create(env.col_id, content="nope")
        env.admin.api_keys.revoke(extra.id)
        assert env.admin.workspace.set_plan("free").plan.id == "free"
    if c.workspace.usage().plan.id != "free":
        return
    hooks = []
    try:
        limit = c.workspace.usage().plan.limits.webhooks
        for _ in range(len(c.webhooks.list()), limit):
            hooks.append(c.webhooks.create("https://example.com/hook"))
        with pytest.raises(PlanLimitError) as exc:
            c.webhooks.create("https://example.com/hook")
        assert (exc.value.status, exc.value.limit) == (402, "webhooks")
    finally:
        for h in hooks:
            c.webhooks.delete(h.id)
