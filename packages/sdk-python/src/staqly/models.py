"""Typed response models (pydantic v2), mirroring ``apps/api/openapi.yaml``.

Hand-written for readability; ``tests/test_models_contract.py`` checks every
model against the OpenAPI schema so they cannot drift silently. Models accept
unknown fields (``extra="allow"``) so a newer server never breaks an older SDK,
and enum-like fields are plain ``str`` for the same reason — compare them with
the ``Literal`` aliases below (``ItemStatus`` …) in your own code.
"""

from __future__ import annotations

from datetime import datetime
from typing import Annotated, Any, Literal

from pydantic import BaseModel, BeforeValidator, ConfigDict, Field

__all__ = [
    "APIKey",
    "ActionDef",
    "Actor",
    "BulkActionResult",
    "Collection",
    "CollectionCover",
    "CollectionRef",
    "Decision",
    "Delivery",
    "ErrorBody",
    "Event",
    "EventPage",
    "Health",
    "Item",
    "ItemPage",
    "ItemStatus",
    "ItemType",
    "ItemVersion",
    "ItemWaitResult",
    "Media",
    "MediaRef",
    "NotificationChannel",
    "NotificationDelivery",
    "NotificationTestResult",
    "PurgeResult",
    "Readiness",
    "WorkspaceExport",
    "Plan",
    "PlanLimits",
    "Principal",
    "Rule",
    "RuleAction",
    "RuleCondition",
    "RuleMatch",
    "RuleParams",
    "Scope",
    "StaqlyModel",
    "Usage",
    "Watcher",
    "WatcherPreset",
    "WatcherPresetParam",
    "WatcherRunResult",
    "Webhook",
    "WebhookEvent",
    "Whoami",
    "Workspace",
    "WorkspaceCreated",
    "WorkspaceUsage",
]

ItemStatus = Literal[
    "draft", "pending", "approved", "rejected", "expired", "processing", "completed", "failed", "archived"
]
ItemType = Literal["image", "video", "text", "link", "json", "file"]
Scope = Literal[
    "collections:read",
    "collections:write",
    "items:read",
    "items:write",
    "items:review",
    "actions:execute",
    "webhooks:manage",
    "rules:manage",
    "watchers:manage",
    "channels:manage",
    "admin",
]
RuleAction = Literal["auto_approve", "auto_reject", "set_status", "expire", "escalate"]


def _empty_to_none(v: Any) -> Any:
    return None if v == "" else v


def _none_to_list(v: Any) -> Any:
    return [] if v is None else v


#: A timestamp that may be null or an empty string on the wire.
OptionalDatetime = Annotated[datetime | None, BeforeValidator(_empty_to_none)]


class StaqlyModel(BaseModel):
    """Base of all models: unknown fields are kept, models are mutable plain data."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)


class ErrorBody(StaqlyModel):
    code: str
    message: str
    fields: dict[str, str] | None = None
    details: dict[str, Any] | None = None


# ---- workspace & auth -------------------------------------------------------


class PlanLimits(StaqlyModel):
    """Plan limits; ``-1`` means unlimited."""

    items_per_month: int
    webhooks: int
    api_keys: int
    storage_bytes: int
    max_upload_bytes: int
    rules: int
    watchers: int
    min_watcher_interval_seconds: int
    notification_channels: int = -1


class Plan(StaqlyModel):
    id: str
    name: str
    price_usd_month: int
    highlight: bool
    limits: PlanLimits
    features: list[str] = Field(default_factory=list)


class Workspace(StaqlyModel):
    id: str
    name: str
    plan: str
    recovery_rotated_at: OptionalDatetime = None
    created_at: datetime


class Usage(StaqlyModel):
    items_per_month: int = 0
    webhooks: int = 0
    api_keys: int = 0
    storage_bytes: int = 0
    rules: int = 0
    watchers: int = 0
    notification_channels: int = 0


class WorkspaceUsage(StaqlyModel):
    plan: Plan
    period: str
    usage: Usage


class APIKey(StaqlyModel):
    id: str
    name: str
    prefix: str
    scopes: list[str]
    created_at: datetime
    last_used_at: OptionalDatetime = None
    expires_at: OptionalDatetime = None
    revoked_at: OptionalDatetime = None
    key: str | None = None
    """Raw secret (``stk_…``); only present in create/rotate responses."""


class WorkspaceCreated(StaqlyModel):
    """Result of ``workspaces.create()``; ``api_key.key`` and ``recovery_key`` are shown only once."""

    workspace: Workspace
    api_key: APIKey
    recovery_key: str
    csrf_token: str


class Principal(StaqlyModel):
    type: str
    id: str
    name: str
    scopes: list[str] = Field(default_factory=list)


class Whoami(StaqlyModel):
    mode: str
    workspace: Workspace
    principal: Principal
    csrf_token: str = ""


class Health(StaqlyModel):
    status: str | None = None
    version: str | None = None


# ---- collections ------------------------------------------------------------


class ActionDef(StaqlyModel):
    """An action reviewers can run on items (``approve``/``reject`` are built in)."""

    name: str
    label: str = ""
    event: str = ""
    requires_feedback: bool = False
    set_status: str | None = None
    allowed_statuses: list[str] = Field(default_factory=list)
    builtin: bool = False


class CollectionCover(StaqlyModel):
    media_id: str | None = None
    url: str | None = None


class Collection(StaqlyModel):
    id: str
    name: str
    description: str = ""
    icon: str = ""
    color: str = ""
    default_view: str = "gallery"
    actions: list[ActionDef] = Field(default_factory=list)
    archived: bool = False
    archived_at: OptionalDatetime = None
    created_at: datetime
    updated_at: datetime
    item_count: int = 0
    status_counts: dict[str, int] = Field(default_factory=dict)
    cover: CollectionCover | None = None


class CollectionRef(StaqlyModel):
    id: str
    name: str


# ---- items & media ----------------------------------------------------------


class MediaRef(StaqlyModel):
    id: str
    mime_type: str
    size_bytes: int
    original_name: str
    url: str
    """Absolute URL; requires the same authentication as the API."""


class Media(MediaRef):
    sha256: str | None = None
    created_at: OptionalDatetime = None


class Item(StaqlyModel):
    """A piece of agent output awaiting (or past) human review."""

    id: str
    collection_id: str
    title: str
    type: str
    content: str
    """Text, or compact JSON text for ``json`` items (see :attr:`content_json`)."""
    url: str = ""
    media: MediaRef | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)
    status: str
    source: str = ""
    external_ref: str = ""
    feedback: str = ""
    version: int
    decided_at: OptionalDatetime = None
    expires_at: OptionalDatetime = None
    editable: list[str] = Field(default_factory=list)
    created_at: datetime
    updated_at: datetime

    @property
    def is_pending(self) -> bool:
        return self.status == "pending"

    @property
    def is_approved(self) -> bool:
        return self.status == "approved"

    @property
    def is_rejected(self) -> bool:
        return self.status == "rejected"

    @property
    def content_json(self) -> Any:
        """``content`` parsed as JSON (raises ``ValueError`` if it is not JSON)."""
        import json

        return json.loads(self.content)


class ItemPage(StaqlyModel):
    data: list[Item]
    next_cursor: str | None = None


class ItemVersion(StaqlyModel):
    version: int
    snapshot: Item
    actor: str
    created_at: datetime


class ItemWaitResult(StaqlyModel):
    decided: bool
    item: Item


class BulkActionResult(StaqlyModel):
    id: str
    ok: bool
    error: ErrorBody | None = None
    item: Item | None = None


class Decision(StaqlyModel):
    """Outcome of :meth:`Staqly.request_approval`."""

    item: Item
    status: str
    approved: bool
    """True when a reviewer (or an auto-approve rule) approved the item."""
    expired: bool
    """True when nobody decided before ``expires_in`` passed (not approved)."""
    feedback: str


# ---- events & webhooks ------------------------------------------------------


class Actor(StaqlyModel):
    type: str
    id: str
    name: str


class Event(StaqlyModel):
    id: str
    seq: int
    type: str
    collection_id: str | None = None
    item_id: str | None = None
    actor: Actor
    data: dict[str, Any] = Field(default_factory=dict)
    created_at: datetime


class EventPage(StaqlyModel):
    data: list[Event]
    next_cursor: str | None = None


class Webhook(StaqlyModel):
    id: str
    url: str
    description: str = ""
    secret: str
    """HMAC signing key for :func:`staqly.verify_webhook`."""
    events: list[str] = Field(default_factory=list)
    collection_ids: list[str] = Field(default_factory=list)
    active: bool = True
    timeout_ms: int
    max_attempts: int
    created_at: datetime
    updated_at: datetime


class Delivery(StaqlyModel):
    id: str
    webhook_id: str
    event_id: str
    event_type: str
    status: str
    attempts: int
    next_attempt_at: OptionalDatetime = None
    last_status_code: int | None = None
    last_error: str = ""
    last_response: str = ""
    last_attempt_at: OptionalDatetime = None
    delivered_at: OptionalDatetime = None
    created_at: datetime


class WebhookEvent(StaqlyModel):
    """Body of a webhook delivery, returned by :func:`staqly.verify_webhook`."""

    id: str
    """Event id; de-duplicate on it (deliveries are at-least-once)."""
    type: str
    created_at: datetime
    workspace_id: str = ""
    delivery_id: str = ""
    attempt: int = 0
    actor: Actor | None = None
    data: dict[str, Any] = Field(default_factory=dict)

    @property
    def item(self) -> Item | None:
        """The item snapshot carried by ``item.*`` events."""
        raw = self.data.get("item")
        return Item.model_validate(raw) if isinstance(raw, dict) else None

    @property
    def collection(self) -> CollectionRef | None:
        raw = self.data.get("collection")
        return CollectionRef.model_validate(raw) if isinstance(raw, dict) else None


# ---- rules & watchers -------------------------------------------------------


class RuleCondition(StaqlyModel):
    path: str
    """Dot path into item metadata, e.g. ``score`` or ``post.likes``."""
    op: str
    """eq, ne, lt, lte, gt, gte, contains, in, not_in, exists."""
    value: Any = None


class RuleMatch(StaqlyModel):
    collection_ids: list[str] | None = None
    types: list[str] | None = None
    source_pattern: str | None = None
    text_contains: str | None = None
    conditions: list[RuleCondition] | None = None


class RuleParams(StaqlyModel):
    feedback: str | None = None
    status: str | None = None
    after_seconds: int | None = None
    note: str | None = None
    channel_id: str | None = None


class Rule(StaqlyModel):
    id: str
    name: str
    priority: int
    enabled: bool
    match: RuleMatch
    action: str
    params: RuleParams
    times_applied: int
    created_at: datetime
    updated_at: datetime


class Watcher(StaqlyModel):
    id: str
    name: str
    kind: str
    url: str
    collection_id: str
    keywords: list[str] = Field(default_factory=list)
    exclude_keywords: list[str] = Field(default_factory=list)
    every: str
    every_seconds: int
    preset_id: str | None = None
    status: str
    last_error: str = ""
    last_run_at: OptionalDatetime = None
    next_run_at: OptionalDatetime = None
    items_created: int = 0
    fail_count: int = 0
    created_at: datetime
    updated_at: datetime


class WatcherPresetParam(StaqlyModel):
    name: str
    label: str
    required: bool
    default: str | None = None
    placeholder: str | None = None


class WatcherPreset(StaqlyModel):
    id: str
    title: str
    category: str
    kind: str
    description: str
    default_every: str
    params: Annotated[list[WatcherPresetParam], BeforeValidator(_none_to_list)] = Field(default_factory=list)


class WatcherRunResult(StaqlyModel):
    status: str | None = None
    new_items: int | None = None
    entries_seen: int | None = None
    baseline: bool | None = None
    error: str | None = None


# ---- notifications, exports, system -----------------------------------------


class NotificationChannel(StaqlyModel):
    """A Telegram/Slack/Discord/ntfy/email channel that pushes review requests."""

    id: str
    name: str
    type: str
    enabled: bool
    config: dict[str, Any] = Field(default_factory=dict)
    """Settings; secrets are masked (e.g. ``"••••abcd"``)."""
    events: list[str] = Field(default_factory=list)
    collection_ids: list[str] = Field(default_factory=list)
    digest_window_seconds: int = 0
    fail_count: int = 0
    last_error: str = ""
    disabled_reason: str | None = None
    last_sent_at: OptionalDatetime = None
    interactive: bool = False
    integration_url: str | None = None
    created_at: datetime
    updated_at: datetime


class NotificationDelivery(StaqlyModel):
    id: str | None = None
    channel_id: str | None = None
    event_id: str | None = None
    event_type: str | None = None
    item_id: str | None = None
    status: str | None = None
    attempts: int | None = None
    next_attempt_at: OptionalDatetime = None
    last_error: str | None = None
    sent_at: OptionalDatetime = None
    created_at: OptionalDatetime = None


class NotificationTestResult(StaqlyModel):
    ok: bool
    error: str | None = None


class WorkspaceExport(StaqlyModel):
    """A workspace export job (ZIP archive, kept 24 h once ready)."""

    id: str
    status: str
    """pending, running, ready, failed or expired."""
    size_bytes: int = 0
    error: str | None = None
    created_at: datetime
    started_at: OptionalDatetime = None
    finished_at: OptionalDatetime = None
    expires_at: OptionalDatetime = None
    download_url: str | None = None


class PurgeResult(StaqlyModel):
    item_id: str
    events_deleted: int
    media_deleted: bool


class ReadinessChecks(StaqlyModel):
    database: str | None = None
    migrations: str | None = None
    workers: str | None = None


class Readiness(StaqlyModel):
    status: str
    checks: ReadinessChecks = Field(default_factory=ReadinessChecks)
