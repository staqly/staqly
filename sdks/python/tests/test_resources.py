"""Request shape of every resource method, for both the sync and async client."""

from __future__ import annotations

import inspect
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any

import httpx
import pytest

from staqly import AsyncStaqly, Staqly, models
from staqly.models import PostTargetInput, ScheduleRule, ThreadPart

from .conftest import Mock, body, jsonr, post, query

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
PLAN = {
    "id": "free",
    "name": "Free",
    "price_month": 0,
    "highlight": False,
    "features": [],
    "limits": {
        "posts_per_month": 100,
        "channels": 2,
        "alerts": 2,
        "webhooks": 3,
        "api_keys": 3,
        "storage_bytes": 1,
        "max_upload_bytes": 1,
    },
}
WORKSPACE = {"id": "ws_1", "name": "W", "plan": "free", "recovery_rotated_at": "", "created_at": TS}
KEY = {"id": "key_1", "name": "k", "prefix": "stk_ab", "scopes": ["posts:read"], "created_at": TS}
CHANNEL = {
    "id": "ch_1",
    "provider": "mastodon",
    "name": "Brand",
    "handle": "@brand",
    "avatar_url": "",
    "profile_url": "",
    "status": "connected",
    "settings": {},
    "last_error": "",
    "created_at": TS,
    "updated_at": TS,
}
PROVIDER = {
    "id": "telegram",
    "name": "Telegram",
    "auth": "token",
    "status": "available",
    "limits": {
        "max_chars": 4096,
        "max_media": 10,
        "needs_title": False,
        "needs_media": False,
        "needs_link": False,
    },
    "credential_fields": [{"key": "bot_token", "label": "Bot token", "kind": "secret", "required": True}],
    "settings_fields": [],
    "app_configured": False,
    "docs_url": "https://docs.staqly.ai/channels/telegram",
}
SCHEDULE = {
    "id": "sch_1",
    "collection_id": "col_1",
    "collection_name": "A",
    "name": "Mornings",
    "channels": ["ch_1"],
    "channel_names": ["Brand"],
    "kind": "recurring",
    "run_at": None,
    "rule": {"times": ["09:00"], "weekdays": [1, 2, 3, 4, 5], "tz": "Europe/Warsaw"},
    "pick": "next",
    "post_ids": [],
    "source": "agent",
    "endpoint_url": "",
    "enabled": True,
    "next_run_at": TS,
    "last_run_at": None,
    "last_result": "",
    "created_at": TS,
    "updated_at": TS,
}
JOB = {
    "id": "job_1",
    "kind": "publish_post",
    "key": "publish_post:pst_1",
    "status": "failed",
    "run_at": TS,
    "attempts": 1,
    "max_attempts": 3,
    "lease_until": None,
    "leased_by": "",
    "last_error": "token expired",
    "payload": {"post_id": "pst_1"},
    "created_at": TS,
    "updated_at": TS,
    "done_at": None,
}
ALERT = {
    "id": "nch_1",
    "name": "Phone",
    "type": "ntfy",
    "enabled": True,
    "config": {"topic": "t"},
    "events": ["post.failed"],
    "collection_ids": [],
    "digest_window_seconds": 0,
    "fail_count": 0,
    "last_error": "",
    "disabled_reason": "",
    "last_sent_at": None,
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
    reply: Any = field(default_factory=lambda: post())
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
        lambda c: c.collections.get("Blog posts"),
        "GET",
        "/v1/collections/Blog%20posts",
        COL,
    ),
    Case(
        "collections.create",
        lambda c: c.collections.create("A", description="d", processing_timeout_seconds=3600),
        "POST",
        "/v1/collections",
        COL,
        201,
        body={"name": "A", "description": "d", "processing_timeout_seconds": 3600},
    ),
    Case(
        "collections.update",
        lambda c: c.collections.update("col_1", name="B", clear_processing_timeout=True),
        "PATCH",
        "/v1/collections/col_1",
        COL,
        body={"name": "B", "processing_timeout_seconds": None},
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
        "collections.delete",
        lambda c: c.collections.delete("col_1", confirm="A"),
        "DELETE",
        "/v1/collections/col_1",
        None,
        204,
        query={"confirm": "A"},
        check=lambda r: r is None,
    ),
    Case(
        "collections.list_posts",
        lambda c: c.collections.list_posts("Blog posts", status="draft", limit=10),
        "GET",
        "/v1/collections/Blog%20posts/posts",
        {"data": [post()], "next_cursor": None},
        query={"status": "draft", "limit": "10"},
        check=lambda r: r.data[0].id == "pst_1",
    ),
    # posts
    Case(
        "posts.list",
        lambda c: c.posts.list(
            collection_id="Posts", status="scheduled", channel_id="ch_1", tag="launch", limit=10
        ),
        "GET",
        "/v1/posts",
        {"data": [post()], "next_cursor": "n"},
        query={
            "collection_id": "Posts",
            "status": "scheduled",
            "channel_id": "ch_1",
            "tag": "launch",
            "limit": "10",
        },
        check=lambda r: r.next_cursor == "n" and r.data[0].id == "pst_1",
    ),
    Case(
        "posts.list calendar",
        lambda c: c.posts.list(publish_after="2026-11-01", publish_before="2026-11-08", sort="publish_asc"),
        "GET",
        "/v1/posts",
        {"data": []},
        query={"publish_after": "2026-11-01", "publish_before": "2026-11-08", "sort": "publish_asc"},
    ),
    Case(
        "posts.search",
        lambda c: c.posts.search("launch", status="draft"),
        "GET",
        "/v1/posts",
        {"data": []},
        query={"q": "launch", "status": "draft"},
    ),
    Case(
        "posts.get",
        lambda c: c.posts.get("pst_1"),
        "GET",
        "/v1/posts/pst_1",
        check=lambda r: isinstance(r, models.Post) and r.targets[0].channel_id == "ch_1",
    ),
    Case(
        "posts.create draft",
        lambda c: c.posts.create(
            "Posts",
            content="Hello",
            media=["med_1"],
            targets=[
                {"channel_id": "ch_1"},
                PostTargetInput(channel_id="ch_2", content="Hi", settings={"lang": "en"}),
            ],
            tags=["launch"],
            metadata={"campaign": "q4"},
            source="sdk-py",
        ),
        "POST",
        "/v1/posts",
        post(),
        201,
        body={
            "collection": "Posts",
            "content": "Hello",
            "media": ["med_1"],
            "targets": [
                {"channel_id": "ch_1"},
                {"channel_id": "ch_2", "content": "Hi", "settings": {"lang": "en"}},
            ],
            "tags": ["launch"],
            "metadata": {"campaign": "q4"},
            "source": "sdk-py",
        },
    ),
    Case(
        "posts.create scheduled",
        lambda c: c.posts.create(
            "col_1",
            content="x",
            targets=[{"channel_id": "ch_1"}],
            status="scheduled",
            publish_at="2026-11-03T09:00:00Z",
        ),
        "POST",
        "/v1/posts",
        post(status="scheduled"),
        201,
        body={
            "collection": "col_1",
            "content": "x",
            "targets": [{"channel_id": "ch_1"}],
            "status": "scheduled",
            "publish_at": "2026-11-03T09:00:00Z",
        },
    ),
    Case(
        "posts.create publish now",
        lambda c: c.posts.create("col_1", content="x", targets=[{"channel_id": "ch_1"}], publish_now=True),
        "POST",
        "/v1/posts",
        post(status="publishing"),
        201,
        body={
            "collection": "col_1",
            "content": "x",
            "targets": [{"channel_id": "ch_1"}],
            "publish_now": True,
        },
    ),
    Case(
        "posts.update",
        lambda c: c.posts.update("pst_1", content="Edited", tags=["a"], expected_version=3),
        "PATCH",
        "/v1/posts/pst_1",
        body={"content": "Edited", "tags": ["a"], "expected_version": 3},
    ),
    Case(
        "posts.update unschedule and move",
        lambda c: c.posts.update("pst_1", clear_publish_at=True, collection="Archive", targets=[]),
        "PATCH",
        "/v1/posts/pst_1",
        body={"publish_at": None, "collection": "Archive", "targets": []},
    ),
    Case(
        "posts.create repeating",
        lambda c: c.posts.create("col_1", content="x", repeat_every="1w"),
        "POST",
        "/v1/posts",
        post(repeat_every="1w"),
        201,
        body={"collection": "col_1", "content": "x", "repeat_every": "1w"},
        check=lambda r: r.repeat_every == "1w",
    ),
    Case(
        "posts.update stop repeating",
        lambda c: c.posts.update("pst_1", repeat_every=""),
        "PATCH",
        "/v1/posts/pst_1",
        body={"repeat_every": ""},
    ),
    Case(
        "posts.create with a thread",
        lambda c: c.posts.create(
            "col_1",
            content="x",
            thread=[ThreadPart(content="first reply"), {"content": "second", "delay_seconds": 300}],
        ),
        "POST",
        "/v1/posts",
        post(
            thread=[{"content": "first reply", "media": [], "delay_seconds": 0, "media_items": []}],
            targets=[
                {
                    "channel_id": "ch_1",
                    "status": "published",
                    "replies": [
                        {
                            "index": 0,
                            "status": "published",
                            "external_id": "r1",
                            "external_url": "https://x.test/r1",
                            "error": "",
                            "published_at": None,
                        }
                    ],
                }
            ],
        ),
        201,
        body={
            "collection": "col_1",
            "content": "x",
            "thread": [
                {"content": "first reply", "media": [], "delay_seconds": 0},
                {"content": "second", "delay_seconds": 300},
            ],
        },
        check=lambda r: (
            r.thread[0].content == "first reply" and r.targets[0].replies[0].status == "published"
        ),
    ),
    Case(
        "posts.update clear the thread",
        lambda c: c.posts.update("pst_1", thread=[]),
        "PATCH",
        "/v1/posts/pst_1",
        body={"thread": []},
    ),
    Case("posts.delete", lambda c: c.posts.delete("pst_1"), "DELETE", "/v1/posts/pst_1", None, 204),
    Case(
        "posts.purge",
        lambda c: c.posts.purge("pst_1"),
        "POST",
        "/v1/posts/pst_1/purge",
        {"post_id": "pst_1", "events_deleted": 3, "media_deleted": 1},
        check=lambda r: r.events_deleted == 3 and r.media_deleted == 1,
    ),
    Case(
        "posts.schedule",
        lambda c: c.posts.schedule("pst_1", "2026-11-03T09:00:00Z"),
        "POST",
        "/v1/posts/pst_1/schedule",
        post(status="scheduled"),
        body={"publish_at": "2026-11-03T09:00:00Z"},
    ),
    Case(
        "posts.publish",
        lambda c: c.posts.publish("pst_1"),
        "POST",
        "/v1/posts/pst_1/publish",
        post(status="publishing"),
        202,
    ),
    Case("posts.cancel", lambda c: c.posts.cancel("pst_1"), "POST", "/v1/posts/pst_1/cancel"),
    Case(
        "posts.duplicate",
        lambda c: c.posts.duplicate("pst_1"),
        "POST",
        "/v1/posts/pst_1/duplicate",
        post(id="pst_2"),
        201,
    ),
    Case(
        "posts.metrics",
        lambda c: c.posts.metrics("pst_1", refresh=True),
        "GET",
        "/v1/posts/pst_1/metrics",
        {"data": [{"channel_id": "ch_1", "available": True, "metrics": {"impressions": 9}}]},
        query={"refresh": "true"},
        check=lambda r: r[0].metrics.impressions == 9,
    ),
    Case(
        "posts.report_metrics",
        lambda c: c.posts.report_metrics("pst_1", "ch_1", {"likes": 3}),
        "PUT",
        "/v1/posts/pst_1/targets/ch_1/metrics",
        {"channel_id": "ch_1", "available": True, "metrics": {"likes": 3}},
        body={"likes": 3},
    ),
    Case(
        "posts.create_preview_link",
        lambda c: c.posts.create_preview_link("pst_1", expires_in_days=7),
        "POST",
        "/v1/posts/pst_1/previews",
        {
            "id": "prv_1",
            "post_id": "pst_1",
            "created_at": "2026-10-20T10:00:00Z",
            "url": "http://x/preview#t",
        },
        201,
        body={"expires_in_days": 7},
        check=lambda r: r.url.endswith("#t"),
    ),
    Case(
        "posts.preview_links",
        lambda c: c.posts.preview_links("pst_1"),
        "GET",
        "/v1/posts/pst_1/previews",
        {"data": []},
    ),
    Case(
        "posts.revoke_preview_link",
        lambda c: c.posts.revoke_preview_link("pst_1", "prv_1"),
        "DELETE",
        "/v1/posts/pst_1/previews/prv_1",
        None,
        204,
    ),
    Case(
        "posts.delete_comment",
        lambda c: c.posts.delete_comment("pst_1", "cmt_1"),
        "DELETE",
        "/v1/posts/pst_1/comments/cmt_1",
        None,
        204,
    ),
    Case(
        "posts.comments",
        lambda c: c.posts.comments("pst_1"),
        "GET",
        "/v1/posts/pst_1/comments",
        {
            "data": [
                {
                    "id": "cmt_1",
                    "post_id": "pst_1",
                    "author_name": "Ana",
                    "body": "Hi",
                    "created_at": "2026-10-20T10:00:00Z",
                }
            ]
        },
        check=lambda r: r[0].author_name == "Ana",
    ),
    Case(
        "posts.retry_target",
        lambda c: c.posts.retry_target("pst_1", "ch_1"),
        "POST",
        "/v1/posts/pst_1/targets/ch_1/retry",
        post(status="publishing"),
        202,
    ),
    Case(
        "posts.report_result published",
        lambda c: c.posts.report_result(
            "pst_1", "ch_1", status="published", url="https://x/1", external_id="42"
        ),
        "POST",
        "/v1/posts/pst_1/targets/ch_1/result",
        post(status="published"),
        body={"status": "published", "url": "https://x/1", "external_id": "42"},
    ),
    Case(
        "posts.report_result failed",
        lambda c: c.posts.report_result("pst_1", "ch_1", status="failed", error="API 500"),
        "POST",
        "/v1/posts/pst_1/targets/ch_1/result",
        post(status="failed"),
        body={"status": "failed", "error": "API 500"},
    ),
    Case(
        "posts.preview",
        lambda c: c.posts.preview("pst_1"),
        "GET",
        "/v1/posts/pst_1/preview",
        {"targets": [{"channel_id": "ch_1", "text": "Hello", "warnings": ["too long"], "ok": False}]},
        check=lambda r: r.targets[0].warnings == ["too long"] and not r.targets[0].ok,
    ),
    Case(
        "posts.history",
        lambda c: c.posts.history("pst_1", limit=5),
        "GET",
        "/v1/posts/pst_1/events",
        {"data": []},
        query={"limit": "5"},
    ),
    Case(
        "posts.tags",
        lambda c: c.posts.tags(),
        "GET",
        "/v1/tags",
        lst({"name": "launch", "count": 3}),
        check=lambda r: r[0].name == "launch" and r[0].count == 3,
    ),
    # work
    Case(
        "work.list",
        lambda c: c.work.list(),
        "GET",
        "/v1/work",
        lst({"kind": "target", "post": post(status="publishing"), "channel": CHANNEL, "requested_at": TS}),
        check=lambda r: r[0].kind == "target" and r[0].post is not None and r[0].channel is not None,
    ),
    # media
    Case("media.delete", lambda c: c.media.delete("med_1"), "DELETE", "/v1/media/med_1", None, 204),
    Case(
        "media.list",
        lambda c: c.media.list(kind="video", used="unused", sort="size_desc", limit=10),
        "GET",
        "/v1/media",
        {
            "data": [
                {
                    "id": "med_1",
                    "mime_type": "image/png",
                    "size_bytes": 4,
                    "original_name": "a.png",
                    "url": "u",
                    "width": 1080,
                    "height": 1350,
                    "uses": 2,
                }
            ],
            "next_offset": 10,
        },
        query={"kind": "video", "used": "unused", "sort": "size_desc", "limit": "10"},
        check=lambda r: r.data[0].uses == 2 and r.data[0].width == 1080 and r.next_offset == 10,
    ),
    Case(
        "media.info",
        lambda c: c.media.info("med_1"),
        "GET",
        "/v1/media/med_1/info",
        {
            "id": "med_1",
            "mime_type": "image/png",
            "size_bytes": 4,
            "original_name": "a.png",
            "url": "u",
            "width": 1080,
            "height": 1350,
        },
        check=lambda r: r.height == 1350,
    ),
    Case(
        "media.update",
        lambda c: c.media.update("med_1", alt_text="A runner"),
        "PATCH",
        "/v1/media/med_1",
        {
            "id": "med_1",
            "mime_type": "image/png",
            "size_bytes": 4,
            "original_name": "a.png",
            "url": "u",
            "width": 1080,
            "height": 1350,
            "alt_text": "A runner",
        },
        body={"alt_text": "A runner"},
        check=lambda r: r.alt_text == "A runner",
    ),
    Case(
        "media.usage",
        lambda c: c.media.usage("med_1"),
        "GET",
        "/v1/media/med_1/usage",
        lst({"post_id": "pst_1", "collection_id": "col_1", "title": "Race week", "status": "scheduled"}),
        check=lambda r: r[0].title == "Race week",
    ),
    Case(
        "posts.delete_many",
        lambda c: c.posts.delete_many(["pst_1", "pst_2"]),
        "POST",
        "/v1/posts/delete",
        {"deleted": ["pst_1"], "missing": ["pst_2"]},
        body={"ids": ["pst_1", "pst_2"]},
        check=lambda r: r["deleted"] == ["pst_1"],
    ),
    Case(
        "media.delete_many",
        lambda c: c.media.delete_many(["med_1", "med_2"]),
        "POST",
        "/v1/media/delete",
        {"deleted": ["med_1"], "missing": ["med_2"]},
        body={"ids": ["med_1", "med_2"]},
        check=lambda r: r["missing"] == ["med_2"],
    ),
    Case(
        "media.restore",
        lambda c: c.media.restore("med_1"),
        "POST",
        "/v1/media/med_1/restore",
        {"id": "med_1", "mime_type": "image/png", "size_bytes": 1, "original_name": "a.png", "url": "u"},
        check=lambda r: r.id == "med_1",
    ),
    Case(
        "media.keep",
        lambda c: c.media.keep("med_1"),
        "POST",
        "/v1/media/med_1/keep",
        {"deleted": ["med_2"]},
        check=lambda r: r["deleted"] == ["med_2"],
    ),
    Case(
        "media.organize",
        lambda c: c.media.organize(["med_1"], folder_id="mfd_1", add_tags=["hero"]),
        "POST",
        "/v1/media/bulk",
        {"updated": ["med_1"], "missing": []},
        body={"ids": ["med_1"], "folder_id": "mfd_1", "add_tags": ["hero"]},
        check=lambda r: r["updated"] == ["med_1"],
    ),
    Case(
        "media.folders",
        lambda c: c.media.folders(),
        "GET",
        "/v1/media-folders",
        {
            "data": [{"id": "mfd_1", "parent_id": "", "name": "Clients", "files": 2}],
            "root_files": 1,
            "total_files": 3,
            "tags": [{"tag": "hero", "files": 2}],
        },
        check=lambda r: r.data[0].name == "Clients" and r.tags[0].tag == "hero",
    ),
    Case(
        "media.create_folder",
        lambda c: c.media.create_folder("ACME", parent_id="mfd_1"),
        "POST",
        "/v1/media-folders",
        {"id": "mfd_2", "parent_id": "mfd_1", "name": "ACME", "files": 0},
        body={"name": "ACME", "parent_id": "mfd_1"},
        check=lambda r: r.parent_id == "mfd_1",
    ),
    Case(
        "media.update_folder",
        lambda c: c.media.update_folder("mfd_2", parent_id=""),
        "PATCH",
        "/v1/media-folders/mfd_2",
        {"id": "mfd_2", "parent_id": "", "name": "ACME", "files": 0},
        body={"parent_id": ""},
        check=lambda r: r.parent_id == "",
    ),
    # events
    Case(
        "events.list",
        lambda c: c.events.list(
            type="post.published", post_id="pst_1", channel_id="ch_1", agent="claude-code"
        ),
        "GET",
        "/v1/events",
        {
            "data": [
                {
                    "id": "e",
                    "seq": 1,
                    "type": "post.published",
                    "post_id": "pst_1",
                    "actor": {"type": "system", "id": "jobs", "name": "Jobs", "agent": None},
                    "created_at": TS,
                }
            ]
        },
        query={"type": "post.published", "post_id": "pst_1", "channel_id": "ch_1", "agent": "claude-code"},
        check=lambda r: r.data[0].seq == 1 and r.data[0].post_id == "pst_1",
    ),
    # channels and providers
    Case(
        "providers.list",
        lambda c: c.providers.list(),
        "GET",
        "/v1/providers",
        lst(PROVIDER),
        check=lambda r: r[0].credential_fields[0].key == "bot_token" and r[0].limits.max_chars == 4096,
    ),
    Case(
        "providers.set_app",
        lambda c: c.providers.set_app("mastodon", client_id="a", client_secret="b"),
        "PUT",
        "/v1/providers/mastodon/app",
        None,
        204,
        body={"client_id": "a", "client_secret": "b"},
    ),
    Case(
        "providers.delete_app",
        lambda c: c.providers.delete_app("mastodon"),
        "DELETE",
        "/v1/providers/mastodon/app",
        None,
        204,
    ),
    Case(
        "channels.list",
        lambda c: c.channels.list(),
        "GET",
        "/v1/channels",
        lst(CHANNEL),
        check=lambda r: r[0].handle == "@brand",
    ),
    Case("channels.get", lambda c: c.channels.get("ch_1"), "GET", "/v1/channels/ch_1", CHANNEL),
    Case(
        "channels.connect token platform",
        lambda c: c.channels.connect(
            "telegram",
            name="News",
            fields={"bot_token": "1:a", "chat_id": "@news"},
            settings={"silent": True},
        ),
        "POST",
        "/v1/channels",
        CHANNEL,
        201,
        body={
            "provider": "telegram",
            "name": "News",
            "fields": {"bot_token": "1:a", "chat_id": "@news"},
            "settings": {"silent": True},
        },
    ),
    Case(
        "channels.connect custom",
        lambda c: c.channels.connect("custom", name="Pipeline"),
        "POST",
        "/v1/channels",
        CHANNEL,
        201,
        body={"provider": "custom", "name": "Pipeline"},
    ),
    Case(
        "channels.update",
        lambda c: c.channels.update(
            "ch_1", name="Brand", settings={"visibility": "unlisted"}, fields={"bot_token": "new"}
        ),
        "PATCH",
        "/v1/channels/ch_1",
        CHANNEL,
        body={"name": "Brand", "settings": {"visibility": "unlisted"}, "fields": {"bot_token": "new"}},
    ),
    Case(
        "channels.disable",
        lambda c: c.channels.disable("ch_1"),
        "PATCH",
        "/v1/channels/ch_1",
        CHANNEL,
        body={"status": "disabled"},
    ),
    Case(
        "channels.enable",
        lambda c: c.channels.enable("ch_1"),
        "PATCH",
        "/v1/channels/ch_1",
        CHANNEL,
        body={"status": "connected"},
    ),
    Case("channels.remove", lambda c: c.channels.remove("ch_1"), "DELETE", "/v1/channels/ch_1", None, 204),
    Case(
        "channels.test",
        lambda c: c.channels.test("ch_1"),
        "POST",
        "/v1/channels/ch_1/test",
        {"ok": True, "profile": {"handle": "@brand"}},
        check=lambda r: r.ok and r.profile == {"handle": "@brand"},
    ),
    Case(
        "channels.oauth_start",
        lambda c: c.channels.oauth_start("mastodon", instance="mastodon.social"),
        "GET",
        "/v1/channels/oauth/mastodon/start",
        {"url": "https://mastodon.social/oauth/authorize?x=1"},
        query={"instance": "mastodon.social"},
        check=lambda r: r == "https://mastodon.social/oauth/authorize?x=1",
    ),
    # schedules
    Case(
        "schedules.list",
        lambda c: c.schedules.list(collection_id="Posts"),
        "GET",
        "/v1/schedules",
        lst(SCHEDULE),
        query={"collection_id": "Posts"},
        check=lambda r: r[0].rule.weekdays == [1, 2, 3, 4, 5],
    ),
    Case("schedules.get", lambda c: c.schedules.get("sch_1"), "GET", "/v1/schedules/sch_1", SCHEDULE),
    Case(
        "schedules.create recurring",
        lambda c: c.schedules.create(
            "Posts",
            name="Mornings",
            kind="recurring",
            rule=ScheduleRule(times=["09:00"], weekdays=[1, 2, 3, 4, 5], tz="Europe/Warsaw"),
            channels=["ch_1"],
            pick="next",
            source="agent",
        ),
        "POST",
        "/v1/schedules",
        SCHEDULE,
        201,
        body={
            "collection": "Posts",
            "name": "Mornings",
            "kind": "recurring",
            "rule": {"times": ["09:00"], "weekdays": [1, 2, 3, 4, 5], "tz": "Europe/Warsaw"},
            "channels": ["ch_1"],
            "pick": "next",
            "source": "agent",
        },
    ),
    Case(
        "schedules.create once from an endpoint",
        lambda c: c.schedules.create(
            "col_1",
            name="Launch",
            kind="once",
            run_at="2026-11-03T09:00:00Z",
            pick="all",
            source="endpoint",
            endpoint_url="https://my.app/posts",
            endpoint_secret="s",
            enabled=False,
        ),
        "POST",
        "/v1/schedules",
        SCHEDULE,
        201,
        body={
            "collection": "col_1",
            "name": "Launch",
            "kind": "once",
            "run_at": "2026-11-03T09:00:00Z",
            "pick": "all",
            "source": "endpoint",
            "endpoint_url": "https://my.app/posts",
            "endpoint_secret": "s",
            "enabled": False,
        },
    ),
    Case(
        "schedules.update",
        lambda c: c.schedules.update(
            "sch_1", rule={"every_minutes": 360}, pick="selected", post_ids=["pst_1"]
        ),
        "PATCH",
        "/v1/schedules/sch_1",
        SCHEDULE,
        body={"rule": {"every_minutes": 360}, "pick": "selected", "post_ids": ["pst_1"]},
    ),
    Case(
        "schedules.disable",
        lambda c: c.schedules.disable("sch_1"),
        "PATCH",
        "/v1/schedules/sch_1",
        SCHEDULE,
        body={"enabled": False},
    ),
    Case(
        "schedules.enable",
        lambda c: c.schedules.enable("sch_1"),
        "PATCH",
        "/v1/schedules/sch_1",
        SCHEDULE,
        body={"enabled": True},
    ),
    Case(
        "schedules.delete", lambda c: c.schedules.delete("sch_1"), "DELETE", "/v1/schedules/sch_1", None, 204
    ),
    Case(
        "schedules.run", lambda c: c.schedules.run("sch_1"), "POST", "/v1/schedules/sch_1/run", SCHEDULE, 202
    ),
    # jobs
    Case(
        "jobs.list",
        lambda c: c.jobs.list(status="failed", kind="publish_post", limit=5),
        "GET",
        "/v1/jobs",
        {"data": [JOB], "next_cursor": "n"},
        query={"status": "failed", "kind": "publish_post", "limit": "5"},
        check=lambda r: r.data[0].payload.post_id == "pst_1" and r.next_cursor == "n",
    ),
    Case(
        "jobs.stats",
        lambda c: c.jobs.stats(),
        "GET",
        "/v1/jobs/stats",
        {
            "queued": 1,
            "running": 0,
            "failed": 0,
            "done_24h": 9,
            "by_kind": {"publish_post": {"queued": 1, "running": 0, "failed": 0}},
        },
        check=lambda r: r.done_24h == 9 and r.by_kind["publish_post"].queued == 1,
    ),
    Case("jobs.get", lambda c: c.jobs.get("job_1"), "GET", "/v1/jobs/job_1", JOB),
    Case(
        "jobs.retry",
        lambda c: c.jobs.retry("job_1"),
        "POST",
        "/v1/jobs/job_1/retry",
        JOB | {"status": "queued"},
        202,
    ),
    Case(
        "jobs.cancel",
        lambda c: c.jobs.cancel("job_1"),
        "POST",
        "/v1/jobs/job_1/cancel",
        JOB | {"status": "cancelled"},
    ),
    # webhooks
    Case("webhooks.list", lambda c: c.webhooks.list(), "GET", "/v1/webhooks", lst(WEBHOOK)),
    Case("webhooks.get", lambda c: c.webhooks.get("wh_1"), "GET", "/v1/webhooks/wh_1", WEBHOOK),
    Case(
        "webhooks.create",
        lambda c: c.webhooks.create("https://h", events=["post.*"]),
        "POST",
        "/v1/webhooks",
        WEBHOOK,
        201,
        body={"url": "https://h", "events": ["post.*"]},
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
        lambda c: c.webhooks.deliveries.list(webhook_id="wh_1", status="failed", post_id="pst_1"),
        "GET",
        "/v1/webhook-deliveries",
        lst(DELIVERY),
        query={"webhook_id": "wh_1", "status": "failed", "post_id": "pst_1"},
    ),
    Case(
        "webhooks.deliveries.retry",
        lambda c: c.webhooks.deliveries.retry("dlv_1"),
        "POST",
        "/v1/webhook-deliveries/dlv_1/retry",
        DELIVERY,
        202,
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
        {"plan": PLAN, "period": "2026-10", "usage": {"posts_per_month": 4}, "posts_by_status": {"draft": 2}},
        check=lambda r: (
            r.usage.posts_per_month == 4 and r.plan.limits.channels == 2 and r.posts_by_status["draft"] == 2
        ),
    ),
    Case(
        "workspace.members",
        lambda c: c.workspace.members(),
        "GET",
        "/v1/members",
        lst(
            {
                "user_id": "usr_1",
                "name": "Ana",
                "email": "ana@example.com",
                "avatar_url": "",
                "role": "owner",
                "you": False,
                "created_at": "2026-10-19T00:00:00Z",
            }
        ),
        check=lambda r: r[0].role == "owner" and not r[0].you,
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
        lambda c: c.api_keys.create("bot", scopes=["posts:read", "posts:write"], expires_at="2027-01-01"),
        "POST",
        "/v1/api-keys",
        KEY,
        201,
        body={"name": "bot", "scopes": ["posts:read", "posts:write"], "expires_at": "2027-01-01"},
    ),
    Case("api_keys.revoke", lambda c: c.api_keys.revoke("key_1"), "DELETE", "/v1/api-keys/key_1", None, 204),
    Case(
        "api_keys.rotate", lambda c: c.api_keys.rotate("key_1"), "POST", "/v1/api-keys/key_1/rotate", KEY, 201
    ),
    # alerts
    Case(
        "alerts.list",
        lambda c: c.alerts.list(),
        "GET",
        "/v1/alerts",
        lst(ALERT),
        check=lambda r: r[0].type == "ntfy",
    ),
    Case("alerts.get", lambda c: c.alerts.get("nch_1"), "GET", "/v1/alerts/nch_1", ALERT),
    Case(
        "alerts.create",
        lambda c: c.alerts.create("Phone", type="ntfy", config={"topic": "t"}, events=["post.failed"]),
        "POST",
        "/v1/alerts",
        ALERT,
        201,
        body={"name": "Phone", "type": "ntfy", "config": {"topic": "t"}, "events": ["post.failed"]},
    ),
    Case(
        "alerts.update",
        lambda c: c.alerts.update("nch_1", config={"priority": 4, "access_token": None}),
        "PATCH",
        "/v1/alerts/nch_1",
        ALERT,
        body={"config": {"priority": 4, "access_token": None}},
    ),
    Case(
        "alerts.enable",
        lambda c: c.alerts.enable("nch_1"),
        "PATCH",
        "/v1/alerts/nch_1",
        ALERT,
        body={"enabled": True},
    ),
    Case(
        "alerts.disable",
        lambda c: c.alerts.disable("nch_1"),
        "PATCH",
        "/v1/alerts/nch_1",
        ALERT,
        body={"enabled": False},
    ),
    Case("alerts.delete", lambda c: c.alerts.delete("nch_1"), "DELETE", "/v1/alerts/nch_1", None, 204),
    Case(
        "alerts.test",
        lambda c: c.alerts.test("nch_1"),
        "POST",
        "/v1/alerts/nch_1/test",
        {"ok": False, "error": "HTTP 401"},
        check=lambda r: not r.ok and r.error == "HTTP 401",
    ),
    Case(
        "alerts.deliveries",
        lambda c: c.alerts.deliveries("nch_1", limit=5),
        "GET",
        "/v1/alerts/nch_1/deliveries",
        lst({"id": "ntd_1", "channel_id": "nch_1", "status": "sent"}),
        query={"limit": "5"},
        check=lambda r: r[0].status == "sent",
    ),
    # exports, readiness
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
    # workspace lifecycle
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
        "workspace.access_token",
        lambda c: c.workspace.access_token(),
        "GET",
        "/v1/access-token",
        {"token": "stk_local_x"},
        check=lambda r: r.token == "stk_local_x",
    ),
    Case(
        "workspace.rotate_access_token",
        lambda c: c.workspace.rotate_access_token(),
        "POST",
        "/v1/access-token/rotate",
        {"token": "stk_local_y", "sessions_revoked": 2},
        check=lambda r: r.token == "stk_local_y" and r.sessions_revoked == 2,
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
        lambda c: c.billing.checkout(plan="plus", interval="year"),
        "POST",
        "/v1/billing/checkout",
        {"url": "https://checkout.stripe.com/c/pay/cs_test_a1b2c3"},
        body={"plan": "plus", "interval": "year"},
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
            r.csrf_token == "csrf_1" and r.api_key.key == "stk_new_secret" and r.recovery_key == "rk_next"
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
    assert len(sync) > 80


class TestPostHelpers:
    def test_schedule_accepts_datetime(self, client: Staqly, mock: Mock) -> None:
        from datetime import datetime, timezone

        mock.reply(jsonr(post(status="scheduled")))
        client.posts.schedule("pst_1", datetime(2026, 11, 3, 9, 0, tzinfo=timezone.utc))
        assert body(mock.last) == {"publish_at": "2026-11-03T09:00:00+00:00"}

    def test_media_upload(self, client: Staqly, mock: Mock) -> None:
        mock.reply(
            jsonr(
                {
                    "id": "med_1",
                    "mime_type": "image/png",
                    "size_bytes": 3,
                    "original_name": "a.png",
                    "url": "u",
                },
                201,
            )
        )
        out = client.media.upload(b"\x89PNG", filename="a.png", content_type="image/png")
        assert out.id == "med_1"
        req = mock.last
        assert req.url.path == "/v1/media"
        assert req.headers["content-type"].startswith("multipart/form-data; boundary=")
        assert b'filename="a.png"' in req.content
        assert "idempotency-key" not in req.headers

    def test_webhook_event_post(self) -> None:
        ev = models.WebhookEvent.model_validate(
            {
                "id": "e",
                "type": "post.published",
                "created_at": TS,
                "data": {"post": post(status="published"), "collection": COL},
            }
        )
        assert ev.post is not None and ev.post.is_published
        assert ev.collection is not None and ev.collection.name == "A"


def test_comments_follow_every_page(client: Staqly, mock: Mock) -> None:
    def comment(i: int) -> dict[str, Any]:
        return {
            "id": f"cmt_{i}",
            "post_id": "pst_1",
            "author_name": "Ana",
            "body": f"#{i}",
            "created_at": "2026-10-20T10:00:00Z",
        }

    mock.reply(
        jsonr({"data": [comment(1), comment(2)], "next_cursor": "c2"}),
        jsonr({"data": [comment(3)]}),
    )
    got = client.posts.comments("pst_1")
    assert [c.body for c in got] == ["#1", "#2", "#3"]
    assert "cursor" not in query(mock.requests[0])
    assert query(mock.requests[1])["cursor"] == "c2"
