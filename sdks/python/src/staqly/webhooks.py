"""Webhook signature verification.

Every delivery is a ``POST`` with a JSON body and these headers::

    X-Staqly-Signature: sha256=<hex HMAC-SHA256(secret, f"{timestamp}.{raw_body}")>
    X-Staqly-Timestamp: <unix seconds>
    X-Staqly-Event:     post.published
    X-Staqly-Event-Id:  evt_...   (stable across retries — de-duplicate on it)
    X-Staqly-Delivery:  dlv_...

Pass the **raw** body bytes exactly as received; re-serialised JSON won't match.
"""

from __future__ import annotations

import hashlib
import hmac
import json
import re
import time
from collections.abc import Mapping
from typing import Any

from .errors import WebhookVerificationError
from .models import WebhookEvent

__all__ = [
    "DEFAULT_TOLERANCE",
    "DELIVERY_HEADER",
    "EVENT_HEADER",
    "EVENT_ID_HEADER",
    "SIGNATURE_HEADER",
    "TIMESTAMP_HEADER",
    "WebhookVerificationError",
    "compute_signature",
    "is_valid_webhook",
    "verify_webhook",
]

SIGNATURE_HEADER = "x-staqly-signature"
TIMESTAMP_HEADER = "x-staqly-timestamp"
EVENT_HEADER = "x-staqly-event"
EVENT_ID_HEADER = "x-staqly-event-id"
DELIVERY_HEADER = "x-staqly-delivery"
#: Default maximum age (and clock skew) of a delivery, in seconds.
DEFAULT_TOLERANCE = 300

# ASCII digits only: str.isdigit() also accepts superscripts such as "²" (which
# int() then rejects with ValueError) and non-ASCII decimal digits.
_TIMESTAMP_RE = re.compile(r"[0-9]+")


def _header(headers: Mapping[str, Any], name: str) -> str | None:
    getter = getattr(headers, "get", None)
    # Case-insensitive mappings (Starlette, Werkzeug, httpx) answer directly.
    if callable(getter):
        value = getter(name)
        if value is None:
            value = getter(name.title())
        if value is not None:
            return value[0] if isinstance(value, (list, tuple)) else str(value)
    for key, value in headers.items():
        if key.lower() == name:
            return value[0] if isinstance(value, (list, tuple)) else str(value)
    return None


def _to_bytes(body: bytes | bytearray | memoryview | str) -> bytes:
    return body.encode("utf-8") if isinstance(body, str) else bytes(body)


def compute_signature(secret: str, timestamp: int | str, body: bytes | bytearray | memoryview | str) -> str:
    """Returns the ``X-Staqly-Signature`` value for ``body`` (useful in your tests)."""
    mac = hmac.new(secret.encode("utf-8"), f"{timestamp}.".encode() + _to_bytes(body), hashlib.sha256)
    return "sha256=" + mac.hexdigest()


def verify_webhook(
    secret: str,
    raw_body: bytes | bytearray | memoryview | str,
    headers: Mapping[str, Any],
    *,
    tolerance: int | None = DEFAULT_TOLERANCE,
    now: float | None = None,
) -> WebhookEvent:
    """Verifies a delivery and returns the parsed :class:`~staqly.models.WebhookEvent`.

    Raises :class:`~staqly.WebhookVerificationError` when the signature is missing or
    wrong (constant-time comparison) or the timestamp is more than ``tolerance``
    seconds away from now (``None`` or ``0`` disables the check).
    """
    if not secret:
        raise WebhookVerificationError("Webhook secret is empty")
    signature = (_header(headers, SIGNATURE_HEADER) or "").strip()
    timestamp = (_header(headers, TIMESTAMP_HEADER) or "").strip()
    if not signature:
        raise WebhookVerificationError("Missing X-Staqly-Signature header")
    if not _TIMESTAMP_RE.fullmatch(timestamp):
        raise WebhookVerificationError("Missing or invalid X-Staqly-Timestamp header")
    if tolerance:
        current = time.time() if now is None else now
        if abs(current - int(timestamp)) > tolerance:
            raise WebhookVerificationError(f"Webhook timestamp is outside the {tolerance}s tolerance")
    body = _to_bytes(raw_body)
    expected = compute_signature(secret, timestamp, body)
    candidates = [s.strip() for s in signature.split(",") if s.strip().startswith("sha256=")]
    if not candidates:
        raise WebhookVerificationError("Malformed X-Staqly-Signature header")
    if not any(hmac.compare_digest(expected.encode(), c.encode()) for c in candidates):
        raise WebhookVerificationError("Webhook signature does not match")
    try:
        payload = json.loads(body)
    except ValueError as exc:
        raise WebhookVerificationError("Webhook body is not valid JSON") from exc
    try:
        return WebhookEvent.model_validate(payload)
    except ValueError as exc:
        raise WebhookVerificationError(f"Unexpected webhook payload: {exc}") from exc


def is_valid_webhook(
    secret: str,
    raw_body: bytes | bytearray | memoryview | str,
    headers: Mapping[str, Any],
    *,
    tolerance: int | None = DEFAULT_TOLERANCE,
    now: float | None = None,
) -> bool:
    """Boolean form of :func:`verify_webhook`. Never raises: anything that cannot be
    verified (including malformed input of the wrong type) is ``False``."""
    try:
        verify_webhook(secret, raw_body, headers, tolerance=tolerance, now=now)
    except Exception:  # a boolean check must not raise on hostile input
        return False
    return True
