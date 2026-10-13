"""Official Python SDK for staq.ly — the local-first workspace where AI agents
store generated content and humans review it.

::

    from staqly import Staqly

    staqly = Staqly()  # local mode: http://localhost:3333, no key
    item = staqly.items.create("Drafts", title="Hello", content="First post")
    decided = staqly.items.wait_for_decision(item.id)
"""

from . import models
from ._client_async import AsyncStaqly
from ._client_sync import Staqly
from ._http import (
    DEFAULT_BASE_URL,
    DEFAULT_MAX_RETRIES,
    DEFAULT_TIMEOUT,
    FileInput,
    RequestOptions,
)
from ._version import __version__
from .errors import (
    APIConnectionError,
    APIError,
    AuthenticationError,
    BadRequestError,
    ConflictError,
    ExportFailedError,
    GoneError,
    InternalServerError,
    NotFoundError,
    PayloadTooLargeError,
    PermissionDeniedError,
    PlanLimitError,
    RateLimitError,
    RejectedError,
    StaqlyError,
    TimeoutError,
    UnsupportedMediaTypeError,
    ValidationError,
    WaitTimeoutError,
    WebhookVerificationError,
)
from .models import (
    APIKey,
    Collection,
    Decision,
    Delivery,
    Event,
    Item,
    ItemPage,
    Media,
    NotificationChannel,
    Rule,
    Watcher,
    Webhook,
    WebhookEvent,
    Workspace,
    WorkspaceExport,
)
from .webhooks import compute_signature, is_valid_webhook, verify_webhook

APITimeoutError = TimeoutError

__all__ = [
    "DEFAULT_BASE_URL",
    "DEFAULT_MAX_RETRIES",
    "DEFAULT_TIMEOUT",
    "APIConnectionError",
    "APIError",
    "APIKey",
    "APITimeoutError",
    "AsyncStaqly",
    "AuthenticationError",
    "BadRequestError",
    "Collection",
    "ConflictError",
    "Decision",
    "Delivery",
    "Event",
    "ExportFailedError",
    "FileInput",
    "GoneError",
    "InternalServerError",
    "Item",
    "ItemPage",
    "Media",
    "NotFoundError",
    "NotificationChannel",
    "PayloadTooLargeError",
    "PermissionDeniedError",
    "PlanLimitError",
    "RateLimitError",
    "RejectedError",
    "RequestOptions",
    "Rule",
    "Staqly",
    "StaqlyError",
    "TimeoutError",
    "UnsupportedMediaTypeError",
    "ValidationError",
    "WaitTimeoutError",
    "Watcher",
    "Webhook",
    "WebhookEvent",
    "WebhookVerificationError",
    "Workspace",
    "WorkspaceExport",
    "__version__",
    "compute_signature",
    "is_valid_webhook",
    "models",
    "verify_webhook",
]
