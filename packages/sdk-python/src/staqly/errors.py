"""Exception hierarchy.

::

    StaqlyError
    ├── APIError                      any non-2xx (status, code, message, fields, details, request_id)
    │   ├── BadRequestError           400
    │   ├── AuthenticationError       401
    │   ├── PlanLimitError            402 plan_limit_exceeded (limit, max, used, plan)
    │   ├── PermissionDeniedError     403 (incl. code "not_local")
    │   ├── NotFoundError             404
    │   ├── ConflictError             409
    │   ├── GoneError                 410
    │   ├── PayloadTooLargeError      413
    │   ├── UnsupportedMediaTypeError 415
    │   ├── ValidationError           422 (fields)
    │   ├── RateLimitError            429 (retry_after)
    │   └── InternalServerError       5xx
    ├── APIConnectionError            network failure, no response
    │   └── TimeoutError              request timed out (also a builtins.TimeoutError)
    ├── WaitTimeoutError              wait_for_decision gave up (also a builtins.TimeoutError)
    ├── ExportFailedError             a workspace export job failed or expired
    ├── RejectedError                 a gated call was rejected / expired
    └── WebhookVerificationError      bad webhook signature or timestamp
"""

from __future__ import annotations

import builtins
import email.utils
import time
from typing import TYPE_CHECKING, Any

import httpx

if TYPE_CHECKING:
    from .models import Item

__all__ = [
    "APIConnectionError",
    "APIError",
    "AuthenticationError",
    "BadRequestError",
    "ConflictError",
    "ExportFailedError",
    "GoneError",
    "InternalServerError",
    "NotFoundError",
    "PayloadTooLargeError",
    "PermissionDeniedError",
    "PlanLimitError",
    "RateLimitError",
    "RejectedError",
    "StaqlyError",
    "TimeoutError",
    "UnsupportedMediaTypeError",
    "ValidationError",
    "WaitTimeoutError",
    "WebhookVerificationError",
]


class StaqlyError(Exception):
    """Base class of every error raised by the SDK."""


class APIError(StaqlyError):
    """The API answered with a non-2xx status."""

    status: int
    code: str
    message: str
    fields: dict[str, str]
    details: dict[str, Any]
    request_id: str | None
    body: Any
    response: httpx.Response | None

    def __init__(
        self,
        status: int,
        code: str,
        message: str,
        *,
        fields: dict[str, str] | None = None,
        details: dict[str, Any] | None = None,
        body: Any = None,
        response: httpx.Response | None = None,
    ) -> None:
        super().__init__(f"{status} {code}: {message}")
        self.status = status
        self.code = code
        self.message = message
        self.fields = fields or {}
        self.details = details or {}
        self.body = body
        self.response = response
        self.request_id = response.headers.get("x-request-id") if response is not None else None

    @property
    def status_code(self) -> int:
        """Alias of :attr:`status`."""
        return self.status

    @property
    def headers(self) -> httpx.Headers:
        return self.response.headers if self.response is not None else httpx.Headers()


class BadRequestError(APIError):
    """400 — malformed request."""


class AuthenticationError(APIError):
    """401 — missing, invalid, expired or revoked API key."""


class PermissionDeniedError(APIError):
    """403 — missing scope, or (``code == "not_local"``) a keyless request that a
    local-mode server refused because it does not come from this machine."""

    @property
    def is_not_local(self) -> bool:
        return self.code == "not_local"


class NotFoundError(APIError):
    """404 — no such resource in this workspace."""


class ConflictError(APIError):
    """409 — e.g. the item was already decided or ``expected_version`` did not match."""


class GoneError(APIError):
    """410 — the resource is gone."""


class PayloadTooLargeError(APIError):
    """413 — upload or body too large."""


class UnsupportedMediaTypeError(APIError):
    """415 — file type not accepted (e.g. SVG/HTML)."""


class ValidationError(APIError):
    """422 — validation failed; :attr:`fields` maps field names to messages."""


class PlanLimitError(APIError):
    """402 ``plan_limit_exceeded`` — the workspace plan does not allow this (cloud)."""

    @property
    def limit(self) -> str | None:
        v = self.details.get("limit")
        return v if isinstance(v, str) else None

    @property
    def max(self) -> int | None:
        v = self.details.get("max")
        return v if isinstance(v, int) else None

    @property
    def used(self) -> int | None:
        v = self.details.get("used")
        return v if isinstance(v, int) else None

    @property
    def plan(self) -> str | None:
        v = self.details.get("plan")
        return v if isinstance(v, str) else None


class RateLimitError(APIError):
    """429 — too many requests. :attr:`retry_after` is in seconds when known."""

    @property
    def retry_after(self) -> float | None:
        return parse_retry_after(self.headers.get("retry-after"))


class InternalServerError(APIError):
    """5xx — retried automatically before being raised."""


class APIConnectionError(StaqlyError):
    """The request never got a response (DNS, refused, reset, …)."""


class TimeoutError(APIConnectionError, builtins.TimeoutError):
    """The request exceeded the client ``timeout``."""


class WaitTimeoutError(StaqlyError, builtins.TimeoutError):
    """:meth:`items.wait_for_decision` gave up while the item was still pending."""

    def __init__(self, item_id: str, waited: float, item: Item | None) -> None:
        super().__init__(f"Item {item_id} was not decided within {waited:.0f}s")
        self.item_id = item_id
        self.waited = waited
        self.item = item


class ExportFailedError(StaqlyError):
    """A workspace export job ended ``failed`` or ``expired``; see :attr:`job`."""

    def __init__(self, job: Any) -> None:
        error = f": {job.error}" if getattr(job, "error", None) else ""
        super().__init__(f"Workspace export {job.id} is {job.status}{error}")
        self.job = job


class RejectedError(StaqlyError):
    """A reviewer rejected (or let expire) a gated call — see :func:`requires_approval`."""

    def __init__(self, item: Item) -> None:
        reason = f": {item.feedback}" if item.feedback else ""
        super().__init__(f"Item {item.id} was {item.status}{reason}")
        self.item = item

    @property
    def feedback(self) -> str:
        return self.item.feedback

    @property
    def status(self) -> str:
        return self.item.status


class WebhookVerificationError(StaqlyError):
    """A webhook request failed signature or timestamp verification."""


def parse_retry_after(value: str | None) -> float | None:
    """Parses ``Retry-After`` (seconds or an HTTP date) into seconds."""
    if not value:
        return None
    try:
        return max(0.0, float(value))
    except ValueError:
        pass
    parsed = email.utils.parsedate_to_datetime(value) if value else None
    if parsed is None:
        return None
    return max(0.0, parsed.timestamp() - time.time())


_BY_STATUS: dict[int, type[APIError]] = {
    400: BadRequestError,
    401: AuthenticationError,
    402: PlanLimitError,
    403: PermissionDeniedError,
    404: NotFoundError,
    409: ConflictError,
    410: GoneError,
    413: PayloadTooLargeError,
    415: UnsupportedMediaTypeError,
    422: ValidationError,
    429: RateLimitError,
}


def make_api_error(response: httpx.Response) -> APIError:
    """Builds the typed error for a failed response."""
    status = response.status_code
    body: Any
    try:
        body = response.json()
    except ValueError:
        body = response.text
    err = body.get("error") if isinstance(body, dict) else None
    if isinstance(err, dict) and isinstance(err.get("code"), str):
        code, message = err["code"], str(err.get("message", ""))
        fields, details = err.get("fields"), err.get("details")
    else:
        code = "http_error"
        message = body[:500] if isinstance(body, str) and body else f"HTTP {status}"
        fields = details = None
    if code == "plan_limit_exceeded":
        cls: type[APIError] = PlanLimitError
    elif status >= 500:
        cls = InternalServerError
    else:
        cls = _BY_STATUS.get(status, APIError)
    return cls(
        status,
        code,
        message,
        fields=fields if isinstance(fields, dict) else None,
        details=details if isinstance(details, dict) else None,
        body=body,
        response=response,
    )
