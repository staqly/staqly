"""Request shape of every resource method, for both the sync and async client."""

from __future__ import annotations

import inspect
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any

import httpx
import pytest

from staqly import AsyncStaqly, Staqly, models

from .conftest import Mock, body, item, jsonr, query

TS = "2026-01-01T00:00:00Z"
COL = {"id": "col_1", "name": "A", "created_at": TS, "updated_at": TS}
WEBHOOK = {
    "id": "wh_1",
    "url": "https://h",
    "secret": "whsec_x",
    "timeout_ms": 1000,
    "max_attempts": 3,
    "created_at": TS,
    "updated_at": TS,
}
DELIVERY = {
    "id": "dlv_1",
    "webhook_id": "wh_1",
    "event_id": "evt_1",
    "event_type": "webhook.test",
    "status": "pending",
    "attempts": 0,
    "created_at": TS,
}
WATCHER = {
    "id": "wat_1",
    "name": "w",
    "kind": "rss",
    "url": "https://h",
    "collection_id": "col_1",
    "every": "1h0m0s",
    "every_seconds": 3600,
    "status": "active",
    "created_at": TS,
    "updated_at": TS,
}
PLAN = {
    "id": "free",
    "name": "Free",
    "price_usd_month": 0,
    "highlight": False,
    "features": [],
    "limits": {
        "items_per_month": 100,
        "webhooks": 3,
        "api_keys": 3,
        "storage_bytes": 1,
        "max_upload_bytes": 1,
        "watchers": 3,
        "min_watcher_interval_seconds": 900,
    },
}
WORKSPACE = {"id": "ws_1", "name": "W", "plan": "free", "recovery_rotated_at": "", "created_at": TS}
KEY = {"id": "key_1", "name": "k", "prefix": "stk_ab", "scopes": ["items:read"], "created_at": TS}
MEDIA = {
    "id": "med_1",
    "mime_type": "image/png",
    "size_bytes": 3,
    "original_name": "a.png",
    "url": "http://x/m",
}
CHANNEL = {
    "id": "nch_1",
    "name": "Phone",
    "type": "ntfy",
    "enabled": True,
    "config": {"topic": "t"},
    "created_at": TS,
    "updated_at": TS,
}
EXPORT = {
    "id": "exp_1",
    "status": "pending",
    "size_bytes": 0,
    "created_at": TS,
    "started_at": None,
    "finished_at": None,
    "expires_at": TS,
}
SESSION = {
    "id": "ses_1",
    "user_agent": "Mozilla/5.0",
    "created_at": TS,
    "last_seen_at": TS,
    "expires_at": TS,
    "current": True,
}


def lst(*xs: Any) -> dict[str, Any]:
    return {"data": list(xs)}


@dataclass
class Case:
    name: str
    call: Callable[[Any], Any]
    method: str
    path: str
    reply: Any = field(default_factory=lambda: item())
    status: int = 200
    query: dict[str, str] = field(default_factory=dict)
    body: Any = None
    check: Callable[[Any], bool] | None = None


CASES = [
    # collections
    Case(
        "collections.list",
        lambda c: c.collections.list(),
        "GET",
        "/v1/collections",
        lst(COL),
        check=lambda r: isinstance(r[0], models.Collection),
    ),
    Case(
        "collections.list archived",
        lambda c: c.collections.list(include_archived=True),
        "GET",
        "/v1/collections",
        lst(),
        query={"include_archived": "true"},
    ),
    Case(
        "collections.get name",
        lambda c: c.collections.get("Instagram posts"),
        "GET",
        "/v1/collections/Instagram%20posts",
        COL,
    ),
    Case(
        "collections.create",
        lambda c: c.collections.create("A", color="#fff", actions=[{"name": "publish", "auto_run": True}]),
        "POST",
        "/v1/collections",
        COL,
        201,
        body={"name": "A", "color": "#fff", "actions": [{"name": "publish", "auto_run": True}]},
    ),
    Case(
        "collections.create with ActionFlow",
        lambda c: c.collections.create(
            "A",
            actions=[
                models.ActionFlow(name="regenerate", allowed_statuses=["rejected"]),
                models.ActionFlow(name="publish", auto_run=True),
            ],
        ),
        "POST",
        "/v1/collections",
        COL,
        201,
        body={
            "name": "A",
            "actions": [
                {"name": "regenerate", "allowed_statuses": ["rejected"]},
                {"name": "publish", "auto_run": True},
            ],
        },
    ),
    Case(
        "collections.create with ActionDef (round trip of a GET)",
        lambda c: c.collections.create(
            "A", actions=[models.ActionDef(name="publish", label="Publish", set_status="processing")]
        ),
        "POST",
        "/v1/collections",
        COL,
        201,
        body={
            "name": "A",
            "actions": [
                {
                    "name": "publish",
                    "label": "Publish",
                    "event": "",
                    "requires_feedback": False,
                    "set_status": "processing",
                    "allowed_statuses": [],
                    "builtin": False,
                    "auto_run": False,
                }
            ],
        },
    ),
    Case(
        "collections.update",
        lambda c: c.collections.update("col_1", name="B"),
        "PATCH",
        "/v1/collections/col_1",
        COL,
        body={"name": "B"},
    ),
    Case(
        "collections.archive",
        lambda c: c.collections.archive("col_1"),
        "PATCH",
        "/v1/collections/col_1",
        COL,
        body={"archived": True},
    ),
    Case(
        "collections.unarchive",
        lambda c: c.collections.unarchive("col_1"),
        "PATCH",
        "/v1/collections/col_1",
        COL,
        body={"archived": False},
    ),
    Case(
        "collections.set_actions",
        lambda c: c.collections.set_actions("col_1", []),
        "PATCH",
        "/v1/collections/col_1",
        COL,
        body={"actions": []},
    ),
    Case(
        "collections.delete",
        lambda c: c.collections.delete("col_1", confirm="A"),
        "DELETE",
        "/v1/collections/col_1",
        None,
        204,
        query={"confirm": "A"},
        check=lambda r: r is None,
    ),
    # items
    Case(
        "items.list",
        lambda c: c.items.list(status="pending", limit=10, created_after="2026-01-01"),
        "GET",
        "/v1/items",
        {"data": [item()], "next_cursor": "n"},
        query={"status": "pending", "limit": "10", "created_after": "2026-01-01"},
        check=lambda r: r.next_cursor == "n" and r.data[0].id == "itm_1",
    ),
    Case(
        "items.list decision, run, tag, archived",
        lambda c: c.items.list(
            decision=["approved", "rejected"], run="failed", tag=["a", "b"], archived=False
        ),
        "GET",
        "/v1/items",
        {"data": []},
        query={"decision": "approved,rejected", "run": "failed", "tag": "a,b", "archived": "false"},
    ),
    Case(
        "items.list collection id",
        lambda c: c.items.list(collection="col_9"),
        "GET",
        "/v1/items",
        {"data": []},
        query={"collection_id": "col_9"},
    ),
    Case(
        "items.search",
        lambda c: c.items.search("sunset", type="image"),
        "GET",
        "/v1/items",
        {"data": []},
        query={"q": "sunset", "type": "image"},
    ),
    Case(
        "items.get",
        lambda c: c.items.get("itm_1"),
        "GET",
        "/v1/items/itm_1",
        check=lambda r: isinstance(r, models.Item),
    ),
    Case(
        "items.create",
        lambda c: c.items.create(
            "Posts", title="x", content={"a": 1}, metadata={"k": 1}, expires_in="24h", editable=["content"]
        ),
        "POST",
        "/v1/collections/Posts/items",
        item(),
        201,
        body={
            "title": "x",
            "content": {"a": 1},
            "metadata": {"k": 1},
            "expires_in": "24h",
            "editable": ["content"],
        },
    ),
    Case(
        "items.update",
        lambda c: c.items.update("itm_1", status="completed", metadata_patch={"post_url": "u"}),
        "PATCH",
        "/v1/items/itm_1",
        body={"status": "completed", "metadata_patch": {"post_url": "u"}},
    ),
    Case("items.delete", lambda c: c.items.delete("itm_1"), "DELETE", "/v1/items/itm_1", None, 204),
    Case(
        "items.approve",
        lambda c: c.items.approve("itm_1", feedback="ok", edits={"title": "T"}),
        "POST",
        "/v1/items/itm_1/actions/approve",
        body={"feedback": "ok", "edits": {"title": "T"}},
    ),
    Case(
        "items.approve bare",
        lambda c: c.items.approve("itm_1"),
        "POST",
        "/v1/items/itm_1/actions/approve",
        body={},
    ),
    Case(
        "items.reject",
        lambda c: c.items.reject("itm_1", feedback="too dark"),
        "POST",
        "/v1/items/itm_1/actions/reject",
        body={"feedback": "too dark"},
    ),
    Case(
        "items.action",
        lambda c: c.items.action(
            "itm_1", "regenerate", feedback="b", payload={"seed": 1}, expected_version=2
        ),
        "POST",
        "/v1/items/itm_1/actions/regenerate",
        body={"feedback": "b", "payload": {"seed": 1}, "expected_version": 2},
    ),
    Case(
        "items.list_actions",
        lambda c: c.items.list_actions("itm_1"),
        "GET",
        "/v1/items/itm_1/actions",
        lst({"name": "approve", "label": "Approve", "event": "item.approved", "requires_feedback": False}),
        check=lambda r: r[0].name == "approve",
    ),
    Case(
        "items.bulk_action",
        lambda c: c.items.bulk_action(["a", "b"], "reject", feedback="no"),
        "POST",
        "/v1/items/bulk-actions",
        lst({"id": "a", "ok": True, "item": item(id="a")}),
        body={"ids": ["a", "b"], "action": "reject", "feedback": "no"},
        check=lambda r: r[0].ok,
    ),
    Case(
        "items.history",
        lambda c: c.items.history("itm_1", limit=5),
        "GET",
        "/v1/items/itm_1/events",
        {"data": []},
        query={"limit": "5"},
    ),
    Case(
        "items.versions",
        lambda c: c.items.versions("itm_1"),
        "GET",
        "/v1/items/itm_1/versions",
        lst({"version": 1, "snapshot": item(), "actor": "local:local", "created_at": TS}),
        check=lambda r: r[0].snapshot.id == "itm_1",
    ),
    Case(
        "items.wait",
        lambda c: c.items.wait("itm_1", timeout=5),
        "GET",
        "/v1/items/itm_1/wait",
        {"decided": False, "item": item()},
        query={"timeout": "5"},
        check=lambda r: r.decided is False,
    ),
    Case(
        "items.reject follow_up",
        lambda c: c.items.reject("itm_1", feedback="redo", follow_up="regenerate"),
        "POST",
        "/v1/items/itm_1/actions/reject",
        body={"feedback": "redo", "follow_up": "regenerate"},
    ),
    Case(
        "items.update tags",
        lambda c: c.items.update("itm_1", tags=["x"]),
        "PATCH",
        "/v1/items/itm_1",
        body={"tags": ["x"]},
    ),
    Case(
        "items.unarchive",
        lambda c: c.items.unarchive("itm_1"),
        "POST",
        "/v1/items/itm_1/unarchive",
    ),
    Case(
        "items.report_result",
        lambda c: c.items.report_result("itm_1", status="completed", result={"url": "u"}),
        "POST",
        "/v1/items/itm_1/result",
        body={"status": "completed", "result": {"url": "u"}},
    ),
    # media
    Case("media.delete", lambda c: c.media.delete("med_1"), "DELETE", "/v1/media/med_1", None, 204),
    # events
    Case(
        "events.list",
        lambda c: c.events.list(type="item.approved", actor_type="api_key"),
        "GET",
        "/v1/events",
        {
            "data": [
                {
                    "id": "e",
                    "seq": 1,
                    "type": "item.approved",
                    "actor": {"type": "local", "id": "l", "name": "L"},
                    "created_at": TS,
                }
            ]
        },
        query={"type": "item.approved", "actor_type": "api_key"},
        check=lambda r: r.data[0].seq == 1,
    ),
    # webhooks
    Case("webhooks.list", lambda c: c.webhooks.list(), "GET", "/v1/webhooks", lst(WEBHOOK)),
    Case("webhooks.get", lambda c: c.webhooks.get("wh_1"), "GET", "/v1/webhooks/wh_1", WEBHOOK),
    Case(
        "webhooks.create",
        lambda c: c.webhooks.create("https://h", events=["item.*"]),
        "POST",
        "/v1/webhooks",
        WEBHOOK,
        201,
        body={"url": "https://h", "events": ["item.*"]},
        check=lambda r: r.secret == "whsec_x",
    ),
    Case(
        "webhooks.update",
        lambda c: c.webhooks.update("wh_1", active=False),
        "PATCH",
        "/v1/webhooks/wh_1",
        WEBHOOK,
        body={"active": False},
    ),
    Case(
        "webhooks.rotate_secret",
        lambda c: c.webhooks.rotate_secret("wh_1"),
        "PATCH",
        "/v1/webhooks/wh_1",
        WEBHOOK,
        body={"rotate_secret": True},
    ),
    Case("webhooks.delete", lambda c: c.webhooks.delete("wh_1"), "DELETE", "/v1/webhooks/wh_1", None, 204),
    Case("webhooks.test", lambda c: c.webhooks.test("wh_1"), "POST", "/v1/webhooks/wh_1/test", DELIVERY, 202),
    Case(
        "webhooks.deliveries.list",
        lambda c: c.webhooks.deliveries.list(webhook_id="wh_1", status="failed"),
        "GET",
        "/v1/webhook-deliveries",
        lst(DELIVERY),
        query={"webhook_id": "wh_1", "status": "failed"},
    ),
    Case(
        "webhooks.deliveries.retry",
        lambda c: c.webhooks.deliveries.retry("dlv_1"),
        "POST",
        "/v1/webhook-deliveries/dlv_1/retry",
        DELIVERY,
        202,
    ),
    # watchers
    Case("watchers.list", lambda c: c.watchers.list(), "GET", "/v1/watchers", lst(WATCHER)),
    Case("watchers.get", lambda c: c.watchers.get("wat_1"), "GET", "/v1/watchers/wat_1", WATCHER),
    Case(
        "watchers.create",
        lambda c: c.watchers.create("HN", kind="rss", url="https://h/rss", collection="News", every="1h"),
        "POST",
        "/v1/watchers",
        WATCHER,
        201,
        body={"name": "HN", "kind": "rss", "url": "https://h/rss", "collection": "News", "every": "1h"},
    ),
    Case(
        "watchers.update",
        lambda c: c.watchers.update("wat_1", keywords=["ai"]),
        "PATCH",
        "/v1/watchers/wat_1",
        WATCHER,
        body={"keywords": ["ai"]},
    ),
    Case(
        "watchers.pause",
        lambda c: c.watchers.pause("wat_1"),
        "PATCH",
        "/v1/watchers/wat_1",
        WATCHER,
        body={"status": "paused"},
    ),
    Case(
        "watchers.resume",
        lambda c: c.watchers.resume("wat_1"),
        "PATCH",
        "/v1/watchers/wat_1",
        WATCHER,
        body={"status": "active"},
    ),
    Case("watchers.delete", lambda c: c.watchers.delete("wat_1"), "DELETE", "/v1/watchers/wat_1", None, 204),
    Case(
        "watchers.run",
        lambda c: c.watchers.run("wat_1"),
        "POST",
        "/v1/watchers/wat_1/run",
        {"status": "ok", "new_items": 2},
        check=lambda r: r.new_items == 2,
    ),
    # workspace, plans, keys
    Case(
        "workspace.get",
        lambda c: c.workspace.get(),
        "GET",
        "/v1/workspace",
        WORKSPACE,
        check=lambda r: r.recovery_rotated_at is None,
    ),
    Case(
        "workspace.rename",
        lambda c: c.workspace.rename("Acme"),
        "PATCH",
        "/v1/workspace",
        WORKSPACE,
        body={"name": "Acme"},
    ),
    Case(
        "workspace.usage",
        lambda c: c.workspace.usage(),
        "GET",
        "/v1/workspace/usage",
        {"plan": PLAN, "period": "2026-10", "usage": {"items_per_month": 4}},
        check=lambda r: r.usage.items_per_month == 4 and r.plan.limits.webhooks == 3,
    ),
    Case("plans.list", lambda c: c.plans.list(), "GET", "/v1/plans", lst(PLAN)),
    Case(
        "api_keys.list",
        lambda c: c.api_keys.list(),
        "GET",
        "/v1/api-keys",
        {"data": [KEY], "available_scopes": []},
        check=lambda r: r[0].id == "key_1",
    ),
    Case(
        "api_keys.create",
        lambda c: c.api_keys.create("bot", scopes=["items:read"], expires_at="2027-01-01"),
        "POST",
        "/v1/api-keys",
        KEY,
        201,
        body={"name": "bot", "scopes": ["items:read"], "expires_at": "2027-01-01"},
    ),
    Case("api_keys.revoke", lambda c: c.api_keys.revoke("key_1"), "DELETE", "/v1/api-keys/key_1", None, 204),
    Case(
        "api_keys.rotate", lambda c: c.api_keys.rotate("key_1"), "POST", "/v1/api-keys/key_1/rotate", KEY, 201
    ),
    # notification channels
    Case(
        "notification_channels.list",
        lambda c: c.notification_channels.list(),
        "GET",
        "/v1/notification-channels",
        lst(CHANNEL),
        check=lambda r: r[0].type == "ntfy",
    ),
    Case(
        "notification_channels.get",
        lambda c: c.notification_channels.get("nch_1"),
        "GET",
        "/v1/notification-channels/nch_1",
        CHANNEL,
    ),
    Case(
        "notification_channels.create",
        lambda c: c.notification_channels.create(
            "Phone", type="ntfy", config={"topic": "t"}, events=["item.created"]
        ),
        "POST",
        "/v1/notification-channels",
        CHANNEL,
        201,
        body={"name": "Phone", "type": "ntfy", "config": {"topic": "t"}, "events": ["item.created"]},
    ),
    Case(
        "notification_channels.update",
        lambda c: c.notification_channels.update("nch_1", digest_window_seconds=60),
        "PATCH",
        "/v1/notification-channels/nch_1",
        CHANNEL,
        body={"digest_window_seconds": 60},
    ),
    Case(
        "notification_channels.enable",
        lambda c: c.notification_channels.enable("nch_1"),
        "PATCH",
        "/v1/notification-channels/nch_1",
        CHANNEL,
        body={"enabled": True},
    ),
    Case(
        "notification_channels.disable",
        lambda c: c.notification_channels.disable("nch_1"),
        "PATCH",
        "/v1/notification-channels/nch_1",
        CHANNEL,
        body={"enabled": False},
    ),
    Case(
        "notification_channels.delete",
        lambda c: c.notification_channels.delete("nch_1"),
        "DELETE",
        "/v1/notification-channels/nch_1",
        None,
        204,
    ),
    Case(
        "notification_channels.test",
        lambda c: c.notification_channels.test("nch_1"),
        "POST",
        "/v1/notification-channels/nch_1/test",
        {"ok": False, "error": "x"},
        check=lambda r: r.error == "x",
    ),
    Case(
        "notification_channels.deliveries",
        lambda c: c.notification_channels.deliveries("nch_1", limit=5),
        "GET",
        "/v1/notification-channels/nch_1/deliveries",
        lst({"id": "nd_1", "status": "sent"}),
        query={"limit": "5"},
        check=lambda r: r[0].status == "sent",
    ),
    # exports, purge, readiness
    Case(
        "exports.create_workspace_export",
        lambda c: c.exports.create_workspace_export(),
        "POST",
        "/v1/workspace/export",
        EXPORT,
        202,
    ),
    Case(
        "exports.get_workspace_export",
        lambda c: c.exports.get_workspace_export("exp_1"),
        "GET",
        "/v1/workspace/exports/exp_1",
        EXPORT,
    ),
    Case(
        "exports.download_workspace_export",
        lambda c: c.exports.download_workspace_export("exp_1"),
        "GET",
        "/v1/workspace/exports/exp_1/download",
        None,
        200,
    ),
    Case(
        "items.purge",
        lambda c: c.items.purge("itm_1"),
        "POST",
        "/v1/items/itm_1/purge",
        {"item_id": "itm_1", "events_deleted": 3, "media_deleted": False},
        check=lambda r: r.events_deleted == 3,
    ),
    Case(
        "ready",
        lambda c: c.ready(),
        "GET",
        "/readyz",
        {"status": "ok", "checks": {"database": "ok"}},
        check=lambda r: r.checks.database == "ok",
    ),
    # misc
    Case(
        "whoami",
        lambda c: c.whoami(),
        "GET",
        "/v1/session",
        {
            "mode": "local",
            "workspace": WORKSPACE,
            "principal": {"type": "local", "id": "l", "name": "L", "scopes": ["admin"]},
            "csrf_token": "",
        },
        check=lambda r: r.mode == "local",
    ),
    Case("health", lambda c: c.health(), "GET", "/health", {"status": "ok"}),
    Case("live", lambda c: c.live(), "GET", "/livez", {"status": "ok"}, check=lambda r: r.status == "ok"),
    Case(
        "setup",
        lambda c: c.setup(),
        "GET",
        "/v1/setup",
        {"initialized": True, "mode": "cloud", "signup_open": True, "setup_token_required": False},
        check=lambda r: r.signup_open and r.public_api_url is None,
    ),
    # collection items
    Case(
        "collections.list_items",
        lambda c: c.collections.list_items("col_1", status="pending", sort="created_asc", limit=10),
        "GET",
        "/v1/collections/col_1/items",
        {"data": [item()], "next_cursor": None},
        query={"status": "pending", "sort": "created_asc", "limit": "10"},
        check=lambda r: r.data[0].id == "itm_1",
    ),
    # workspace lifecycle
    Case(
        "workspace.set_plan",
        lambda c: c.workspace.set_plan(),
        "POST",
        "/v1/workspace/plan",
        {"plan": PLAN, "period": "2026-01", "usage": {}},
        body={"plan": "free"},
        check=lambda r: r.plan.id == "free",
    ),
    Case(
        "workspace.delete",
        lambda c: c.workspace.delete(confirm="W"),
        "DELETE",
        "/v1/workspace",
        None,
        204,
        body={"confirm": "W"},
    ),
    Case(
        "workspace.rotate_recovery_key",
        lambda c: c.workspace.rotate_recovery_key(),
        "POST",
        "/v1/recovery-key/rotate",
        {"recovery_key": "rk_new"},
        check=lambda r: r.recovery_key == "rk_new",
    ),
    # sessions & device links
    Case(
        "sessions.list",
        lambda c: c.sessions.list(),
        "GET",
        "/v1/sessions",
        lst(SESSION),
        check=lambda r: r[0].current and isinstance(r[0], models.Session),
    ),
    Case(
        "sessions.current",
        lambda c: c.sessions.current(),
        "GET",
        "/v1/session",
        {
            "mode": "cloud",
            "workspace": WORKSPACE,
            "principal": {"type": "session", "id": "ses_1", "name": "Browser", "scopes": ["admin"]},
            "csrf_token": "csrf_1",
        },
        check=lambda r: r.csrf_token == "csrf_1",
    ),
    Case(
        "billing.get",
        lambda c: c.billing.get(),
        "GET",
        "/v1/billing",
        {
            "enabled": True,
            "plan": "pro",
            "subscription_status": "active",
            "plan_expires_at": TS,
            "suspended": False,
            "has_customer": True,
        },
        check=lambda r: r.subscription_status == "active" and r.plan_expires_at is not None,
    ),
    Case(
        "billing.checkout",
        lambda c: c.billing.checkout(),
        "POST",
        "/v1/billing/checkout",
        {"url": "https://checkout.stripe.com/c/pay/cs_test_a1b2c3"},
        check=lambda r: r.url.startswith("https://checkout.stripe.com/"),
    ),
    Case(
        "billing.portal",
        lambda c: c.billing.portal(),
        "POST",
        "/v1/billing/portal",
        {"url": "https://billing.stripe.com/p/session/test_a1b2c3"},
        check=lambda r: r.url.startswith("https://billing.stripe.com/"),
    ),
    Case("sessions.revoke", lambda c: c.sessions.revoke("ses_1"), "DELETE", "/v1/sessions/ses_1", None, 204),
    Case("sessions.logout", lambda c: c.sessions.logout(), "DELETE", "/v1/session", None, 204),
    Case(
        "sessions.recover",
        lambda c: c.sessions.recover("rk_secret"),
        "POST",
        "/v1/sessions/recover",
        {
            "csrf_token": "csrf_1",
            "api_key": {
                "id": "key_1",
                "name": "Admin key",
                "prefix": "stk_new",
                "scopes": ["admin"],
                "created_at": TS,
                "key": "stk_new_secret",
            },
            "recovery_key": "rk_next",
        },
        201,
        body={"recovery_key": "rk_secret"},
        check=lambda r: (
            r.csrf_token == "csrf_1"
            and r.api_key.key == "stk_new_secret"
            and r.api_key.scopes == ["admin"]
            and r.recovery_key == "rk_next"
        ),
    ),
    Case(
        "sessions.sign_in_with_api_key",
        lambda c: c.sessions.sign_in_with_api_key("stk_admin"),
        "POST",
        "/v1/sessions/api-key",
        {"csrf_token": "csrf_2"},
        201,
        body={"api_key": "stk_admin"},
        check=lambda r: r.csrf_token == "csrf_2",
    ),
    Case(
        "device_links.create",
        lambda c: c.device_links.create(),
        "POST",
        "/v1/device-links",
        {"url": "https://h/connect#t", "expires_at": TS, "reachable": False, "hint": "localhost"},
        201,
        check=lambda r: not r.reachable and r.hint == "localhost",
    ),
    Case(
        "device_links.redeem",
        lambda c: c.device_links.redeem("tok_1"),
        "POST",
        "/v1/device-links/redeem",
        {"csrf_token": "csrf_2"},
        201,
        body={"token": "tok_1"},
        check=lambda r: r.csrf_token == "csrf_2",
    ),
    # one-tap decision links
    Case(
        "decision_links.decide",
        lambda c: c.decision_links.decide("dtk_1", feedback="No"),
        "POST",
        "/v1/decide/dtk_1",
        {"message": "Rejected", "status": "rejected", "item_id": "itm_1", "action": "reject"},
        body={"feedback": "No"},
        check=lambda r: r.status == "rejected" and r.item_id == "itm_1",
    ),
]


def _verify(case: Case, mock: Mock, result: Any) -> None:
    assert len(mock.requests) == 1
    req = mock.last
    assert req.method == case.method
    assert req.url.raw_path.decode().split("?")[0] == case.path
    assert query(req) == case.query
    if case.body is not None:
        assert body(req) == case.body
    elif case.method == "GET":
        assert not req.content
    if case.check is not None:
        assert case.check(result)


def _reply(case: Case) -> httpx.Response:
    return httpx.Response(case.status) if case.reply is None else jsonr(case.reply, case.status)


@pytest.mark.parametrize("case", CASES, ids=[c.name for c in CASES])
def test_sync(case: Case, client: Staqly, mock: Mock) -> None:
    mock.reply(_reply(case))
    _verify(case, mock, case.call(client))


@pytest.mark.parametrize("case", CASES, ids=[c.name for c in CASES])
async def test_async(case: Case, aclient: AsyncStaqly, mock: Mock) -> None:
    mock.reply(_reply(case))
    _verify(case, mock, await case.call(aclient))


def test_sync_and_async_surfaces_match() -> None:
    def surface(obj: Any, prefix: str = "") -> dict[str, list[str]]:
        out: dict[str, list[str]] = {}
        for name, member in inspect.getmembers(obj):
            if name.startswith("_") or name in {"aclose", "close"}:
                continue
            if inspect.ismethod(member):
                out[prefix + name] = list(inspect.signature(member).parameters)
            elif member.__class__.__module__.startswith("staqly._client"):
                out.update(surface(member, f"{prefix}{name}."))
        return out

    with Staqly(base_url="http://x") as s:
        sync = surface(s)
    a = AsyncStaqly(base_url="http://x")
    assert surface(a) == sync
    assert len(sync) > 70


class TestItemHelpers:
    def test_collection_name_is_resolved(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(COL | {"id": "col_7"}), jsonr({"data": []}))
        client.items.list(collection="Posts", status="approved")
        assert [str(r.url).removeprefix("http://api.test") for r in mock.requests] == [
            "/v1/collections/Posts",
            "/v1/items?collection_id=col_7&status=approved",
        ]

    def test_list_items_resolves_collection_name(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(COL | {"id": "col_7"}), jsonr({"data": [], "next_cursor": None}))
        client.collections.list_items("Posts", status="approved")
        assert [str(r.url).removeprefix("http://api.test") for r in mock.requests] == [
            "/v1/collections/Posts",
            "/v1/collections/col_7/items?status=approved",
        ]

    @pytest.mark.parametrize("kind", ["bytes", "path", "fileobj"])
    def test_upload_multipart(self, client: Staqly, mock: Mock, tmp_path: Any, kind: str) -> None:
        png = b"\x89PNG\r\n\x1a\n"
        f = tmp_path / "a.png"
        f.write_bytes(png)
        source: Any = {"bytes": png, "path": f, "fileobj": f.open("rb")}[kind]
        mock.reply(jsonr(item(type="image"), 201))
        out = client.items.upload(
            "Posts", source, filename="a.png" if kind == "bytes" else None, title="Avatar", metadata={"k": 1}
        )
        if kind == "fileobj":
            source.close()
        assert out.type == "image"
        req = mock.last
        assert req.url.path == "/v1/collections/Posts/items"
        assert req.headers["content-type"].startswith("multipart/form-data; boundary=")
        raw = req.content
        assert b'name="item"' in raw
        assert b'{"title": "Avatar", "metadata": {"k": 1}}' in raw
        assert b'name="file"; filename="a.png"' in raw
        assert b"Content-Type: image/png" in raw
        assert png in raw

    async def test_async_upload(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(jsonr(MEDIA, 201))
        m = await aclient.media.upload(b"abc", filename="n.txt")
        assert m.id == "med_1"
        assert b'filename="n.txt"' in mock.last.content
        assert b"Content-Type: text/plain" in mock.last.content

    def test_media_download_and_url(self, client: Staqly, mock: Mock) -> None:
        mock.reply(httpx.Response(200, content=b"\x01\x02\x03", headers={"Content-Type": "image/png"}))
        assert client.media.download("med_1") == b"\x01\x02\x03"
        assert mock.last.url.path == "/v1/media/med_1"
        assert client.media.url("med_1") == "http://api.test/v1/media/med_1"

    def test_ids_are_url_encoded(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(item()))
        client.items.action("a/b", "do it")
        assert mock.last.url.raw_path == b"/v1/items/a%2Fb/actions/do%20it"

    def test_item_helpers(self) -> None:
        it = models.Item.model_validate(
            item(status="approved", type="json", content='{"a":1}', extra_field=1)
        )
        assert it.is_approved and not it.is_pending and not it.is_rejected
        assert it.content_json == {"a": 1}
        assert it.model_extra == {"extra_field": 1}
