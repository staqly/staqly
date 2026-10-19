from __future__ import annotations

import hashlib
import hmac
import json
from typing import Any

import pytest

from staqly import WebhookVerificationError, compute_signature, is_valid_webhook, verify_webhook

SECRET = "whsec_test"
NOW = 1_800_000_000
BODY = json.dumps(
    {
        "id": "evt_1",
        "type": "post.published",
        "created_at": "2026-01-01T00:00:00.000000Z",
        "workspace_id": "ws_1",
        "delivery_id": "dlv_1",
        "attempt": 1,
        "actor": {"type": "local", "id": "local", "name": "Local"},
        "data": {
            "post": {
                "id": "pst_1",
                "collection_id": "col_1",
                "content": "Hello",
                "status": "published",
                "version": 2,
                "created_at": "2026-01-01T00:00:00Z",
                "updated_at": "2026-01-01T00:00:00Z",
            },
            "collection": {"id": "col_1", "name": "C"},
            "channel": {"id": "ch_1", "name": "Brand", "provider": "mastodon"},
        },
    }
)


def sign(secret: str, ts: int, body: str) -> str:
    return "sha256=" + hmac.new(secret.encode(), f"{ts}.{body}".encode(), hashlib.sha256).hexdigest()


def headers(sig: str | None = None, ts: int | str = NOW) -> dict[str, str]:
    return {
        "X-Staqly-Signature": sig if sig is not None else sign(SECRET, NOW, BODY),
        "X-Staqly-Timestamp": str(ts),
        "X-Staqly-Event": "post.published",
    }


def test_valid_delivery_returns_typed_event() -> None:
    ev = verify_webhook(SECRET, BODY.encode(), headers(), now=NOW)
    assert ev.type == "post.published"
    assert ev.post is not None and ev.post.status == "published"
    assert ev.collection is not None and ev.collection.name == "C"
    assert ev.data["channel"]["provider"] == "mastodon"


def test_accepts_str_body_and_lowercase_headers() -> None:
    h = {k.lower(): v for k, v in headers().items()}
    assert verify_webhook(SECRET, BODY, h, now=NOW).id == "evt_1"


def test_accepts_case_insensitive_mapping() -> None:
    import httpx

    assert verify_webhook(SECRET, BODY, httpx.Headers(headers()), now=NOW).id == "evt_1"


def test_compute_signature_matches_server_scheme() -> None:
    assert compute_signature(SECRET, NOW, BODY) == sign(SECRET, NOW, BODY)


BAD: list[tuple[str, Any]] = [
    ("tampered body", lambda: verify_webhook(SECRET, BODY + " ", headers(), now=NOW)),
    ("wrong secret", lambda: verify_webhook("whsec_other", BODY, headers(), now=NOW)),
    (
        "expired",
        lambda: verify_webhook(SECRET, BODY, headers(sign(SECRET, NOW - 301, BODY), NOW - 301), now=NOW),
    ),
    (
        "future",
        lambda: verify_webhook(SECRET, BODY, headers(sign(SECRET, NOW + 301, BODY), NOW + 301), now=NOW),
    ),
    ("timestamp swapped", lambda: verify_webhook(SECRET, BODY, headers(ts=NOW - 1), now=NOW)),
    ("missing signature", lambda: verify_webhook(SECRET, BODY, {"X-Staqly-Timestamp": str(NOW)}, now=NOW)),
    ("missing timestamp", lambda: verify_webhook(SECRET, BODY, {"X-Staqly-Signature": "sha256=00"}, now=NOW)),
    ("bad timestamp", lambda: verify_webhook(SECRET, BODY, headers(ts="abc"), now=NOW)),
    ("no prefix", lambda: verify_webhook(SECRET, BODY, headers(sign(SECRET, NOW, BODY)[7:]), now=NOW)),
    ("empty secret", lambda: verify_webhook("", BODY, headers(), now=NOW)),
    ("not json", lambda: verify_webhook(SECRET, "nope", headers(sign(SECRET, NOW, "nope")), now=NOW)),
    ("not an event", lambda: verify_webhook(SECRET, "[]", headers(sign(SECRET, NOW, "[]")), now=NOW)),
]


@pytest.mark.parametrize("call", [c for _, c in BAD], ids=[n for n, _ in BAD])
def test_rejects(call: Any) -> None:
    with pytest.raises(WebhookVerificationError):
        call()


def test_tolerance_can_be_widened_or_disabled() -> None:
    old = NOW - 3600
    h = headers(sign(SECRET, old, BODY), old)
    assert verify_webhook(SECRET, BODY, h, now=NOW, tolerance=7200).id == "evt_1"
    assert verify_webhook(SECRET, BODY, h, now=NOW, tolerance=None).id == "evt_1"


def test_default_uses_current_time() -> None:
    with pytest.raises(WebhookVerificationError, match="tolerance"):
        verify_webhook(SECRET, BODY, headers())  # NOW is far in the future


def test_is_valid_webhook() -> None:
    assert is_valid_webhook(SECRET, BODY, headers(), now=NOW)
    assert not is_valid_webhook(SECRET, "x", headers(), now=NOW)


@pytest.mark.parametrize("ts", ["²", "١٢٣", "1²", "+1", "-1", "1.0", "1_000", "", "0x10"])
def test_non_ascii_or_malformed_timestamps_are_rejected(ts: str) -> None:
    h = {"X-Staqly-Signature": sign(SECRET, NOW, BODY), "X-Staqly-Timestamp": ts}
    with pytest.raises(WebhookVerificationError, match="X-Staqly-Timestamp"):
        verify_webhook(SECRET, BODY, h, now=NOW, tolerance=None)
    assert is_valid_webhook(SECRET, BODY, h, now=NOW) is False


@pytest.mark.parametrize(
    "args",
    [
        (SECRET, BODY, {"X-Staqly-Signature": "sha256=00", "X-Staqly-Timestamp": "²"}),
        (SECRET, object(), headers()),
        (SECRET, BODY, None),
        (None, BODY, headers()),
        (SECRET, BODY, {"X-Staqly-Signature": object(), "X-Staqly-Timestamp": str(NOW)}),
    ],
)
def test_is_valid_webhook_never_raises(args: tuple[Any, Any, Any]) -> None:
    assert is_valid_webhook(*args, now=NOW) is False
