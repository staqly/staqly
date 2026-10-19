"""Official Python SDK for staqly — the local-first scheduler for posts your agents write.

::

    from staqly import Staqly

    staqly = Staqly()  # local mode: http://localhost:3333, no key
    channel = staqly.channels.list()[0]
    post = staqly.posts.create(
        "Blog posts", content="Hello", targets=[{"channel_id": channel.id}], publish_now=True
    )
    done = staqly.posts.wait_for_published(post.id)
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
    StaqlyError,
    TimeoutError,
    UnsupportedMediaTypeError,
    ValidationError,
    WaitTimeoutError,
    WebhookVerificationError,
)
from .models import (
    Alert,
    APIKey,
    Channel,
    Collection,
    Delivery,
    Event,
    Job,
    Media,
    Post,
    PostPage,
    PostTarget,
    PostTargetInput,
    Provider,
    Reply,
    Schedule,
    ScheduleRule,
    ThreadPart,
    ThreadPartOut,
    Webhook,
    WebhookEvent,
    WorkItem,
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
    "Alert",
    "AsyncStaqly",
    "AuthenticationError",
    "BadRequestError",
    "Channel",
    "Collection",
    "ConflictError",
    "Delivery",
    "Event",
    "ExportFailedError",
    "FileInput",
    "GoneError",
    "InternalServerError",
    "Job",
    "Media",
    "NotFoundError",
    "PayloadTooLargeError",
    "PermissionDeniedError",
    "PlanLimitError",
    "Post",
    "PostPage",
    "PostTarget",
    "PostTargetInput",
    "Reply",
    "ThreadPart",
    "ThreadPartOut",
    "Provider",
    "RateLimitError",
    "RequestOptions",
    "Schedule",
    "ScheduleRule",
    "Staqly",
    "StaqlyError",
    "TimeoutError",
    "UnsupportedMediaTypeError",
    "ValidationError",
    "WaitTimeoutError",
    "Webhook",
    "WebhookEvent",
    "WebhookVerificationError",
    "WorkItem",
    "Workspace",
    "WorkspaceExport",
    "__version__",
    "compute_signature",
    "is_valid_webhook",
    "models",
    "verify_webhook",
]
