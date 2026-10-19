"""Typed response models (pydantic v2), mirroring ``apps/api/openapi.yaml``.

Hand-written for readability; ``tests/test_models_contract.py`` checks every
model against the OpenAPI schema so they cannot drift silently. Models accept
unknown fields (``extra="allow"``) so a newer server never breaks an older SDK,
and enum-like fields are plain ``str`` for the same reason — compare them with
the ``Literal`` aliases below (``PostStatus`` ...) in your own code.
"""

from __future__ import annotations

from datetime import datetime
from typing import Annotated, Any, Literal

from pydantic import BaseModel, BeforeValidator, ConfigDict, Field

__all__ = [
    "APIKey",
    "Actor",
    "Agent",
    "Alert",
    "AlertDelivery",
    "AlertTestResult",
    "Billing",
    "BillingURL",
    "Channel",
    "ChannelStatus",
    "ChannelTestResult",
    "Collection",
    "CollectionRef",
    "Delivery",
    "DeviceLink",
    "ErrorBody",
    "Event",
    "EventPage",
    "Health",
    "Job",
    "JobKindStats",
    "JobPage",
    "JobPayload",
    "JobStats",
    "Liveness",
    "Media",
    "Plan",
    "PlanLimits",
    "Post",
    "PostPage",
    "PostPreview",
    "PostPreviewTarget",
    "PostStatus",
    "PostTarget",
    "PostTargetInput",
    "Reply",
    "ThreadPart",
    "ThreadPartOut",
    "Principal",
    "Provider",
    "ProviderField",
    "ProviderFieldOption",
    "ProviderLimits",
    "PurgeResult",
    "Readiness",
    "RecoveryKey",
    "Schedule",
    "ScheduleRule",
    "Scope",
    "Session",
    "SessionCreated",
    "SessionRecovered",
    "SetupStatus",
    "StaqlyModel",
    "Tag",
    "TargetStatus",
    "Usage",
    "Webhook",
    "WebhookEvent",
    "Whoami",
    "WorkItem",
    "Workspace",
    "WorkspaceCreated",
    "WorkspaceExport",
    "WorkspaceUsage",
]

PostStatus = Literal["draft", "scheduled", "publishing", "published", "failed"]
TargetStatus = Literal["pending", "publishing", "published", "failed"]
ChannelStatus = Literal["connected", "needs_reconnect", "disabled"]
Scope = Literal[
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
    "admin",
]


def _empty_to_none(v: Any) -> Any:
    return None if v == "" else v


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

    #: Posts created per calendar month (people, agents and schedules).
    posts_per_month: int
    #: Connected channels.
    channels: int
    alerts: int
    webhooks: int
    api_keys: int
    storage_bytes: int
    max_upload_bytes: int
    #: Workspace exports (backups) and event exports; ``False`` on Free.
    data_export: bool = True


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
    posts_per_month: int = 0
    channels: int = 0
    alerts: int = 0
    webhooks: int = 0
    api_keys: int = 0
    storage_bytes: int = 0


class WorkspaceUsage(StaqlyModel):
    plan: Plan
    period: str
    usage: Usage
    #: End of the Pro trial or paid period (``None`` = no end).
    plan_expires_at: OptionalDatetime = None
    #: The Pro trial or paid period ended unpaid: the workspace is read-only.
    suspended: bool = False
    #: Posts created this period, by their current status.
    posts_by_status: dict[str, int] = {}
    #: Webhook deliveries right now: ``queued``, ``retrying`` and ``failed`` (gave up).
    webhook_deliveries: dict[str, int] = {}


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
    """Raw secret (``stk_...``); only present in create/rotate responses."""


class Billing(StaqlyModel):
    """Subscription status (``billing.get()``)."""

    #: Billing is configured on this instance (cloud mode with Stripe).
    enabled: bool
    plan: str
    #: Stripe's status (active, trialing, past_due, canceled, unpaid, incomplete);
    #: ``None`` = never subscribed.
    subscription_status: str | None = None
    #: End of the trial or paid period (``None`` = no end).
    plan_expires_at: OptionalDatetime = None
    #: The period ended unpaid; the workspace is read-only.
    suspended: bool
    #: A Stripe customer exists (the portal is available).
    has_customer: bool


class BillingURL(StaqlyModel):
    """A Stripe Checkout or customer-portal URL to open in a browser."""

    url: str


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


class Session(StaqlyModel):
    """An active browser (dashboard) session."""

    id: str
    user_agent: str
    created_at: datetime
    last_seen_at: datetime
    expires_at: datetime
    #: True for the session making the request.
    current: bool


class SessionCreated(StaqlyModel):
    """A new session cookie was set; send ``csrf_token`` as ``X-CSRF-Token`` on unsafe requests."""

    csrf_token: str = ""


class SessionRecovered(StaqlyModel):
    """Result of ``sessions.recover()``: a new session cookie was set, plus a new admin API key
    (``api_key.key``) and the next recovery key. Both secrets are shown only once; the recovery
    key that was just used stops working."""

    csrf_token: str
    api_key: APIKey
    recovery_key: str


class DeviceLink(StaqlyModel):
    """One-time "connect a device" link (single use, valid for 5 minutes)."""

    url: str
    expires_at: datetime
    #: ``False`` when the URL points at localhost (other devices cannot open it).
    reachable: bool
    hint: str | None = None


class RecoveryKey(StaqlyModel):
    """A newly issued recovery key, shown only once."""

    recovery_key: str


class SetupStatus(StaqlyModel):
    """Result of ``setup()``: server mode and whether a workspace can be created."""

    #: Local mode only: the workspace exists.
    initialized: bool
    mode: str
    #: Cloud: anyone may create a workspace.
    signup_open: bool
    setup_token_required: bool
    #: Cloud with Stripe configured: paid plans can be bought.
    billing_enabled: bool = False
    #: ``PUBLIC_API_URL``: the API base URL for agents and tools.
    public_api_url: str | None = None


class Liveness(StaqlyModel):
    status: str


class Health(StaqlyModel):
    status: str | None = None
    version: str | None = None


# ---- collections ------------------------------------------------------------


class Collection(StaqlyModel):
    id: str
    name: str
    description: str = ""
    icon: str = ""
    color: str = ""
    #: How long a target on your own automation may wait for a result before it fails, in
    #: seconds; ``None`` = the server default.
    processing_timeout_seconds: int | None = None
    #: The workspace's Default collection: it can be renamed but not archived or deleted.
    is_default: bool = False
    archived: bool = False
    archived_at: OptionalDatetime = None
    created_at: datetime
    updated_at: datetime
    post_count: int = 0
    status_counts: dict[str, int] = Field(default_factory=dict)


class CollectionRef(StaqlyModel):
    id: str
    name: str


# ---- media, posts & targets -------------------------------------------------


class Media(StaqlyModel):
    """An uploaded file."""

    id: str
    mime_type: str
    size_bytes: int
    original_name: str
    url: str
    """Absolute URL; requires the same authentication as the API."""
    sha256: str | None = None
    created_at: OptionalDatetime = None


class ThreadPart(StaqlyModel):
    """A reply published under the post: text and/or media, ``delay_seconds`` after the part before
    (the first after the post). Platforms that take replies publish it (X); your own automation gets
    the thread with its request."""

    content: str = ""
    media: list[str] = Field(default_factory=list)
    delay_seconds: int = 0


class ThreadPartOut(StaqlyModel):
    """A thread part as returned, with its media resolved."""

    content: str = ""
    media: list[str] = Field(default_factory=list)
    delay_seconds: int = 0
    media_items: list[Media] = Field(default_factory=list)


class Reply(StaqlyModel):
    """How one thread part went on one channel."""

    index: int
    #: pending, publishing, published, failed or skipped (the platform takes no replies, or an
    #: earlier part failed).
    status: str = "pending"
    external_id: str = ""
    external_url: str = ""
    error: str = ""
    published_at: OptionalDatetime = None


class PostTarget(StaqlyModel):
    """One channel a post goes to, with the result of publishing there."""

    channel_id: str
    channel_name: str = ""
    provider: str = ""
    avatar_url: str = ""
    #: Text for this channel instead of the post's content (``None`` uses the post's).
    content: str | None = None
    #: The provider's settings by key (title, subreddit, visibility ...).
    settings: dict[str, str] = Field(default_factory=dict)
    status: str = "pending"
    #: The platform's id of the published post.
    external_id: str = ""
    #: Where the post lives on the platform.
    external_url: str = ""
    error: str = ""
    attempts: int = 0
    published_at: OptionalDatetime = None
    #: How each thread part went on this channel, one per part.
    replies: list[Reply] = Field(default_factory=list)


class PostTargetInput(StaqlyModel):
    """A target to create: the channel, optional text of its own and provider settings."""

    channel_id: str
    content: str | None = None
    settings: dict[str, str] | None = None


class Post(StaqlyModel):
    """Something that goes out: text, media and the channels it goes to."""

    id: str
    collection_id: str
    collection_name: str = ""
    content: str = ""
    #: Media ids, in order.
    media: list[str] = Field(default_factory=list)
    #: The same media, resolved.
    media_items: list[Media] = Field(default_factory=list)
    #: draft, scheduled, publishing, published or failed (see :data:`PostStatus`).
    status: str
    #: When it goes out (scheduled), or the time it kept as a note (draft after cancel).
    publish_at: OptionalDatetime = None
    published_at: OptionalDatetime = None
    tags: list[str] = Field(default_factory=list)
    metadata: dict[str, Any] = Field(default_factory=dict)
    #: Who created it: a client name, an agent, a schedule.
    source: str = ""
    external_ref: str = ""
    #: Repeat after publishing: ``""`` (no repeat), ``1d``..``6d``, ``1w``, ``2w`` or ``1mo``.
    #: Once the post is published, staqly schedules a copy that much later; the copy repeats too.
    repeat_every: str = ""
    #: Replies published under the post after it, in order.
    thread: list[ThreadPartOut] = Field(default_factory=list)
    #: Grows with every edit; see ``expected_version`` on :meth:`Posts.update`.
    version: int = 1
    targets: list[PostTarget] = Field(default_factory=list)
    created_at: datetime
    updated_at: datetime

    @property
    def is_published(self) -> bool:
        return self.status == "published"

    @property
    def is_failed(self) -> bool:
        return self.status == "failed"


class PostPage(StaqlyModel):
    data: list[Post]
    next_cursor: str | None = None


class PostPreviewTarget(StaqlyModel):
    channel_id: str
    #: The text this target will carry.
    text: str = ""
    #: What the provider objects to (too long, media missing ...).
    warnings: list[str] = Field(default_factory=list)
    ok: bool = True


class PostPreview(StaqlyModel):
    targets: list[PostPreviewTarget] = Field(default_factory=list)


class Tag(StaqlyModel):
    name: str
    #: How many posts carry it.
    count: int = 0


# ---- channels & providers ---------------------------------------------------


class Channel(StaqlyModel):
    """A connected account. Credentials are stored sealed and never returned."""

    id: str
    #: Provider id (see :meth:`Providers.list`).
    provider: str
    name: str
    #: The account's handle on the platform (@name, a channel id).
    handle: str = ""
    avatar_url: str = ""
    profile_url: str = ""
    #: connected, needs_reconnect or disabled (see :data:`ChannelStatus`).
    status: str
    #: Provider-level defaults for new targets.
    settings: dict[str, Any] = Field(default_factory=dict)
    last_error: str = ""
    created_at: datetime
    updated_at: datetime


class ChannelTestResult(StaqlyModel):
    ok: bool
    error: str | None = None
    #: What the platform knows the account as (handle, name, avatar_url, profile_url).
    profile: dict[str, Any] | None = None


class ProviderFieldOption(StaqlyModel):
    value: str
    label: str


class ProviderFieldCondition(StaqlyModel):
    """The field shows (and ``required`` applies) only while field ``key`` holds ``value``."""

    key: str
    value: str


class ProviderField(StaqlyModel):
    """A form field a provider needs: a credential to connect, or a setting per target."""

    key: str
    label: str
    help: str | None = None
    #: text, secret, url, select, textarea, boolean or number.
    kind: str
    options: list[ProviderFieldOption] | None = None
    required: bool = False
    default: str | None = None
    placeholder: str | None = None
    when: ProviderFieldCondition | None = None


class ProviderLimits(StaqlyModel):
    """What the composer and the publish validation apply."""

    #: 0 = no limit.
    max_chars: int = 0
    #: 0 = no media.
    max_media: int = 0
    media_kinds: list[str] = Field(default_factory=list)
    needs_title: bool = False
    needs_media: bool = False
    needs_link: bool = False


class Provider(StaqlyModel):
    """A platform staqly can publish to."""

    id: str
    name: str
    #: oauth2, token or none.
    auth: str
    #: available (can be connected) or soon.
    status: str
    limits: ProviderLimits
    #: What :meth:`Channels.connect` needs in ``fields`` (token platforms).
    credential_fields: list[ProviderField] = Field(default_factory=list)
    #: Per-target settings a post may set.
    settings_fields: list[ProviderField] = Field(default_factory=list)
    #: OAuth platforms: a developer app is set (the workspace's or the server's).
    app_configured: bool = False
    docs_url: str = ""


# ---- schedules & jobs -------------------------------------------------------


class ScheduleRule(StaqlyModel):
    """When a recurring schedule runs: ``times`` (HH:MM) on ``weekdays`` in ``tz``, or every
    ``every_minutes``. Weekdays: 0 = Sunday ... 6 = Saturday; empty = every day."""

    times: list[str] | None = None
    weekdays: list[int] | None = None
    tz: str | None = None
    every_minutes: int | None = None


class Schedule(StaqlyModel):
    id: str
    collection_id: str
    collection_name: str = ""
    name: str
    #: Channel ids the chosen posts go to (a post's own targets win).
    channels: list[str] = Field(default_factory=list)
    channel_names: list[str] = Field(default_factory=list)
    #: once or recurring.
    kind: str
    run_at: OptionalDatetime = None
    rule: ScheduleRule = Field(default_factory=ScheduleRule)
    #: next (the oldest waiting draft), selected (``post_ids``) or all.
    pick: str = "next"
    post_ids: list[str] = Field(default_factory=list)
    #: When nothing waits: none, agent (``schedule.posts_requested``) or endpoint (``endpoint_url``).
    source: str = "none"
    endpoint_url: str = ""
    enabled: bool = True
    next_run_at: OptionalDatetime = None
    last_run_at: OptionalDatetime = None
    #: What the last run did, or why it failed.
    last_result: str = ""
    created_at: datetime
    updated_at: datetime


class JobPayload(StaqlyModel):
    post_id: str | None = None
    channel_id: str | None = None
    schedule_id: str | None = None
    #: publish_reply: the thread part and the platform id it replies to.
    index: int | None = None
    parent_id: str | None = None


class Job(StaqlyModel):
    """One unit of background work."""

    id: str
    #: publish_post, publish_target, target_timeout, run_schedule or refresh_channel.
    kind: str
    #: Unique per subject (``publish_post:<post id>``); queueing the same key again replaces the job.
    key: str = ""
    #: queued, running, done, failed or cancelled.
    status: str
    run_at: datetime
    attempts: int = 0
    max_attempts: int = 3
    lease_until: OptionalDatetime = None
    #: The server instance running it.
    leased_by: str = ""
    last_error: str = ""
    payload: JobPayload = Field(default_factory=JobPayload)
    created_at: datetime
    updated_at: datetime
    done_at: OptionalDatetime = None


class JobPage(StaqlyModel):
    data: list[Job]
    next_cursor: str | None = None


class JobKindStats(StaqlyModel):
    queued: int = 0
    running: int = 0
    failed: int = 0


class JobStats(StaqlyModel):
    queued: int = 0
    running: int = 0
    failed: int = 0
    #: Finished in the last 24 hours.
    done_24h: int = 0
    by_kind: dict[str, JobKindStats] = Field(default_factory=dict)


class WorkItem(StaqlyModel):
    """Something the workspace asks your automation for.

    ``kind="target"``: publish ``post`` on ``channel`` (your own automation) and report the result
    with :meth:`Posts.report_result`; ``kind="posts"``: ``schedule`` found nothing to publish and
    wants new posts in ``collection``.
    """

    kind: str
    post: Post | None = None
    channel: Channel | None = None
    schedule: Schedule | None = None
    collection: Collection | None = None
    requested_at: datetime


# ---- events & webhooks ------------------------------------------------------


class Actor(StaqlyModel):
    type: str
    id: str
    name: str
    #: The MCP or REST client that acted (as listed by ``workspace.agents()``), when known.
    agent: str | None = None


class Event(StaqlyModel):
    id: str
    seq: int
    type: str
    collection_id: str | None = None
    post_id: str | None = None
    channel_id: str | None = None
    schedule_id: str | None = None
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
    """When the event happened. Deliveries can arrive out of order: order by this, not by arrival."""
    workspace_id: str = ""
    delivery_id: str = ""
    attempt: int = 0
    actor: Actor | None = None
    data: dict[str, Any] = Field(default_factory=dict)

    @property
    def post(self) -> Post | None:
        """The post snapshot carried by ``post.*`` events."""
        raw = self.data.get("post")
        return Post.model_validate(raw) if isinstance(raw, dict) else None

    @property
    def collection(self) -> CollectionRef | None:
        raw = self.data.get("collection")
        return CollectionRef.model_validate(raw) if isinstance(raw, dict) else None


# ---- alerts, exports, system ------------------------------------------------


class Alert(StaqlyModel):
    """A Telegram/Slack/Discord/ntfy/email channel that gets a message when a post is published or
    fails, or a channel needs reconnecting."""

    id: str
    name: str
    type: str
    enabled: bool
    config: dict[str, Any] = Field(default_factory=dict)
    """Settings; secrets are masked (e.g. ``"••••abcd"``)."""
    events: list[str] = Field(default_factory=list)
    collection_ids: list[str] = Field(default_factory=list)
    digest_window_seconds: int = 0
    #: Consecutive failed sends; the alert is disabled at the server's limit.
    fail_count: int = 0
    last_error: str = ""
    disabled_reason: str = ""
    last_sent_at: OptionalDatetime = None
    created_at: datetime
    updated_at: datetime


class AlertDelivery(StaqlyModel):
    id: str
    channel_id: str
    event_id: str = ""
    event_type: str = ""
    post_id: str | None = None
    #: pending, sent, failed or skipped.
    status: str
    attempts: int = 0
    next_attempt_at: OptionalDatetime = None
    last_error: str = ""
    sent_at: OptionalDatetime = None
    created_at: OptionalDatetime = None


class AlertTestResult(StaqlyModel):
    ok: bool
    error: str | None = None


class Agent(StaqlyModel):
    """An MCP client or REST client seen talking to the workspace, per credential."""

    id: str
    kind: str
    """mcp or api."""
    name: str
    version: str = ""
    principal_id: str
    principal_name: str = ""
    first_seen_at: datetime
    last_seen_at: datetime
    requests: int = 0
    posts_created: int = 0


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
    includes_media: bool = True
    """False for a data-only export (Free plan): the ZIP has no media files."""


class PurgeResult(StaqlyModel):
    post_id: str
    events_deleted: int
    #: Media files deleted because no other post used them.
    media_deleted: int


class ReadinessChecks(StaqlyModel):
    database: str | None = None
    migrations: str | None = None
    workers: str | None = None


class Readiness(StaqlyModel):
    status: str
    checks: ReadinessChecks = Field(default_factory=ReadinessChecks)
