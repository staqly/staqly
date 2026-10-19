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
    "XBundle",
    "XPosts",
    "Channel",
    "ChannelActivity",
    "ChannelGroup",
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
    "MediaLibraryItem",
    "MediaPage",
    "MediaUse",
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
#: What a channel does with the links in a post's text at publish time (the post keeps its text).
LinksMode = Literal["keep", "remove", "defang"]
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
    #: Workspace exports (backups) and event exports; ``False`` on Starter.
    data_export: bool = True
    #: Posts to X included per month (a post with a live link counts 14); ``-1`` = unlimited.
    x_posts_per_period: int = -1


class LimitBonus(StaqlyModel):
    """What one entitlement (an add-on, a deal) added to a limit."""

    limit: str
    #: Added to the limit (bytes for storage and upload sizes).
    add: int = 0
    #: The entitlement lifts the limit (or switches ``data_export`` on).
    unlimited: bool = False
    #: The feature's name, e.g. ``"Channels pack"``.
    #: ``from`` is a Python keyword, hence the alias.
    from_: str = Field(alias="from")


class Plan(StaqlyModel):
    id: str
    name: str
    #: Per user per month, billed monthly, in whole units of ``currency`` (e.g. 29).
    price_month: int
    #: Per user per year, billed yearly: twelve whole months at 20 % off or more (e.g. 276).
    price_year: int = 0
    #: ISO 4217 code, lower case (``"eur"``).
    currency: str = "eur"
    highlight: bool
    limits: PlanLimits
    features: list[str] = Field(default_factory=list)
    #: A workspace's plan only: Stripe features its subscription grants (lookup keys), e.g. ``"ai_pack"``.
    entitlements: list[str] = Field(default_factory=list)
    #: The plan's own limits when entitlements changed ``limits``.
    base_limits: PlanLimits | None = None
    limit_bonuses: list[LimitBonus] = Field(default_factory=list)


class Workspace(StaqlyModel):
    id: str
    name: str
    plan: str
    recovery_rotated_at: OptionalDatetime = None
    created_at: datetime


class Usage(StaqlyModel):
    posts_per_month: int = 0
    channels: int = 0
    seats: int = 0
    alerts: int = 0
    webhooks: int = 0
    api_keys: int = 0
    storage_bytes: int = 0
    #: Posts to X this month (a post with a live link counts 14).
    x_posts_per_period: int = 0


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
    #: People billed: every workspace member, whatever the role (plans are priced per user).
    seats: int = 1
    #: Whether choosing Starter now starts its 7-day trial (card required): ``available``, ``used``
    #: (the workspace had a subscription before), ``other_workspace`` (another workspace of the account is
    #: in its trial, so this one is charged today) or ``none`` (billing off, or already paying).
    trial: str = "none"


class BillingSubscription(StaqlyModel):
    """The current Stripe subscription (``billing.details()``)."""

    #: Stripe's status: active, trialing, past_due, canceled, unpaid, incomplete, ...
    status: str
    current_period_start: OptionalDatetime = None
    #: End of the billing period: the next renewal.
    current_period_end: OptionalDatetime = None
    #: The subscription ends at the end of this period.
    cancel_at_period_end: bool = False
    cancel_at: OptionalDatetime = None
    canceled_at: OptionalDatetime = None
    trial_end: OptionalDatetime = None
    #: Price per seat (user) per ``interval`` in the smallest currency unit (cents).
    amount: int = 0
    currency: str = ""
    interval: str = ""
    #: Seats billed: the workspace's members.
    quantity: int = 1


class PaymentMethod(StaqlyModel):
    """A card on file: brand and last four digits only."""

    brand: str
    last4: str
    exp_month: int
    exp_year: int
    #: The card the subscription is charged to.
    default: bool


class Invoice(StaqlyModel):
    """An issued Stripe invoice; amounts in the smallest currency unit (cents)."""

    id: str
    number: str
    created: OptionalDatetime = None
    period_start: OptionalDatetime = None
    period_end: OptionalDatetime = None
    total: int
    amount_paid: int
    amount_due: int
    currency: str
    #: paid, open, void or uncollectible.
    status: str
    hosted_invoice_url: str
    invoice_pdf: str


class BillingDetails(StaqlyModel):
    """Subscription period, cards on file and invoices, read from Stripe (``billing.details()``)."""

    enabled: bool
    subscription: BillingSubscription | None = None
    payment_methods: list[PaymentMethod]
    invoices: list[Invoice]
    #: Older invoices exist: pass the last invoice's id as ``starting_after``.
    has_more_invoices: bool


class XBundle(StaqlyModel):
    """A one-time bundle of extra posts to X."""

    posts: int
    #: In whole units of ``XPosts.currency``.
    price: int


class XPosts(StaqlyModel):
    """Posts to X this period (``billing.x_posts()``): included, bought and what's left."""

    #: The month the included posts count in (``YYYY-MM``) and when they reset.
    period: str
    period_end: OptionalDatetime = None
    #: No allowance applies (a local install).
    unlimited: bool = False
    included: int
    #: Posts counted this month (included, bought and any overage).
    used: int
    included_left: int
    #: Bought posts not used yet, and when the soonest of them expire.
    credits_left: int
    credits_expire_at: OptionalDatetime = None
    left: int
    bundles: list[XBundle] = []
    currency: str = "eur"
    #: How many posts a post with a live link counts as.
    link_post_weight: int = 14


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


class User(StaqlyModel):
    """A person: their Google name, address and picture (cloud accounts), or the computer's user (local)."""

    id: str = ""
    name: str
    email: str = ""
    avatar_url: str = ""


class MyWorkspace(StaqlyModel):
    """A workspace the caller can open, with their role in it."""

    id: str
    name: str
    role: str
    current: bool = False


class Member(StaqlyModel):
    """A person in the workspace and their role (owner, admin, editor or viewer)."""

    user_id: str
    name: str
    email: str
    avatar_url: str = ""
    role: str
    #: Owns the account the workspace belongs to (always an owner, cannot be removed).
    account_owner: bool = False
    you: bool = False
    created_at: datetime


class Whoami(StaqlyModel):
    mode: str
    workspace: Workspace
    principal: Principal
    csrf_token: str = ""
    user: User | None = None
    #: The caller's role in this workspace (owner, admin, editor, viewer).
    role: str = ""
    account_owner: bool = False
    accounts_enabled: bool = False
    workspaces: list[MyWorkspace] = Field(default_factory=list)
    #: The dashboard's welcome wizard is done for this person (or local install).
    onboarded: bool = True


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
    #: Cloud accounts: people sign in with Google.
    google_sign_in: bool = False
    #: Cloud accounts: people sign in with a code sent to their email address.
    email_sign_in: bool = False
    #: Cloud: the sign-in from before accounts (admin API key, recovery key) works here.
    legacy_sign_in: bool = False
    #: Cloud accounts: people sign in with a passkey.
    passkey_sign_in: bool = False


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
    alt_text: str = ""
    """Describes a picture for people who can't see it; platforms that take alt text receive it."""
    width: int = 0
    """Pixels; 0 when unknown (pictures are measured on upload, videos by the uploading browser)."""
    height: int = 0
    duration_ms: int = 0
    """A video's or audio file's length; 0 when unknown."""
    folder_id: str = ""
    """The library folder the file sits in; empty at the top level."""
    tags: list[str] = Field(default_factory=list)
    deleted_at: OptionalDatetime = None
    """Set while the file is in Deleted."""
    purge_at: OptionalDatetime = None
    """When a file in Deleted is deleted for good."""
    duplicate_of: str | None = None
    """The oldest other file with the same content, if any."""


class MediaLibraryItem(Media):
    """A file in the media library, with how many posts use it."""

    uses: int = 0


class MediaPage(StaqlyModel):
    """One page of the media library; ``next_offset`` is where the next page starts."""

    data: list[MediaLibraryItem] = []
    next_offset: int | None = None


class MediaFolder(StaqlyModel):
    """A virtual folder of the media library."""

    id: str
    parent_id: str = ""
    """The folder it sits in; empty at the top level."""
    name: str
    files: int = 0
    """Files directly in this folder (not in its subfolders)."""
    created_at: OptionalDatetime = None


class MediaTagCount(StaqlyModel):
    """A file tag and how many files carry it."""

    tag: str
    files: int = 0


class MediaFolders(StaqlyModel):
    """The library's folders, file counts and tags."""

    data: list[MediaFolder] = []
    root_files: int = 0
    total_files: int = 0
    tags: list[MediaTagCount] = []
    deleted_files: int = 0
    duplicate_files: int = 0


class MediaUse(StaqlyModel):
    """A post that uses a file (in its files or a comment)."""

    post_id: str
    collection_id: str
    title: str
    status: str
    publish_at: OptionalDatetime = None


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
    #: Who created the post: a person, or the API key (agent, automation) that did; empty when unknown.
    created_by: str = ""
    #: Reviewers' comment threads not resolved yet.
    open_comments: int = 0

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
    #: The text this target will carry, with the channel's links mode applied.
    text: str = ""
    #: The channel's links mode: keep, remove or defang (see :data:`LinksMode`).
    links: str = "keep"
    #: The links the channel's links mode removes or defangs, as written.
    links_found: list[str] = Field(default_factory=list)
    #: What the provider objects to (too long, media missing ...) and what happens to links_found.
    warnings: list[str] = Field(default_factory=list)
    ok: bool = True


class PostPreview(StaqlyModel):
    targets: list[PostPreviewTarget] = Field(default_factory=list)


class PostMetrics(StaqlyModel):
    """A post's numbers on one platform; None is a number the platform does not give."""

    impressions: int | None = None
    likes: int | None = None
    reposts: int | None = None
    replies: int | None = None
    quotes: int | None = None
    bookmarks: int | None = None
    link_clicks: int | None = None


class TargetMetrics(StaqlyModel):
    channel_id: str
    channel_name: str = ""
    provider: str = ""
    status: str = ""
    external_url: str = ""
    #: The platform reports numbers (or your automation sends them).
    available: bool = False
    metrics: PostMetrics | None = None
    updated_at: datetime | None = None
    #: Why the last read failed.
    error: str = ""


class PreviewLink(StaqlyModel):
    id: str
    post_id: str
    created_at: datetime
    expires_at: datetime | None = None
    revoked_at: datetime | None = None
    active: bool = True
    #: Only when created.
    url: str = ""
    token: str = ""


class ChannelLink(StaqlyModel):
    """A connect link: someone without access opens it and connects a channel to the workspace."""

    id: str
    note: str = ""
    providers: list[str] = []
    group_id: str = ""
    max_uses: int = 1
    uses: int = 0
    status: Literal["open", "used", "expired", "revoked"] | str = "open"
    created_by: str = ""
    created_at: datetime
    expires_at: datetime
    revoked_at: datetime | None = None
    #: Only when created.
    url: str = ""
    token: str = ""


class PostComment(StaqlyModel):
    """One comment of a feedback thread; a thread's first comment may mark a passage of the post."""

    id: str
    post_id: str
    #: The thread's first comment; empty for that first comment.
    parent_id: str = ""
    author_name: str
    #: Only for the team.
    author_email: str = ""
    #: ``reviewer`` (left on a preview page) or ``team``.
    author_kind: str = "reviewer"
    body: str
    #: The marked words (empty for a general comment and for replies).
    quote: str = ""
    #: The channel's version the passage is in; empty: the shared text.
    channel_id: str = ""
    #: 0: the post's text; n: the n-th comment of its thread.
    part: int = 0
    #: Where the passage is in that text now (UTF-16 code units); -1 when there is none.
    anchor_start: int = -1
    anchor_end: int = -1
    #: The marked words are no longer in the text.
    text_changed: bool = False
    resolved_at: datetime | None = None
    resolved_by: str = ""
    created_at: datetime


class Tag(StaqlyModel):
    name: str
    #: How many posts carry it.
    count: int = 0


# ---- channels & providers ---------------------------------------------------


class ChannelActivity(StaqlyModel):
    """Read from a channel's targets."""

    last_published_at: datetime | None = None
    last_post_id: str | None = None
    next_publish_at: datetime | None = None
    #: Posts published on the channel in the last 30 days.
    published_30d: int = 0


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
    #: Provider-level defaults for new targets; ``links`` sets what the channel does with links.
    settings: dict[str, Any] = Field(default_factory=dict)
    #: What this channel does with the links in a post: keep, remove or defang (``example[.]com``).
    links: str = "keep"
    #: The channel group it belongs to (every channel is in exactly one; Default unless moved).
    group_id: str = ""
    last_error: str = ""
    created_at: datetime
    updated_at: datetime
    #: When the channel last published and its next scheduled post; only from list and get.
    activity: ChannelActivity | None = None


class ChannelGroup(StaqlyModel):
    """The one group a channel sits in (one per client, say). The Default group always exists."""

    id: str
    name: str
    color: str = ""
    position: int = 0
    default: bool = False
    #: Channels in the group (from :meth:`Channels.list_groups`).
    channels: int = 0
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
    #: text, secret, url, select, textarea, boolean, number; list (comma-separated values such as tags) or
    #: channels (comma-separated ids of other channels of the same platform, e.g. X ``reposters``).
    kind: str
    options: list[ProviderFieldOption] | None = None
    required: bool = False
    default: str | None = None
    placeholder: str | None = None
    when: ProviderFieldCondition | None = None
    #: At most this many characters (text, textarea, url).
    max_length: int | None = None
    #: At most this many values (list, channels).
    max_items: int | None = None


class ImageRules(StaqlyModel):
    """What a platform refuses in a picture (its API's documented limits); 0 or empty = no limit."""

    max_bytes: int = 0
    #: MIME types it takes; empty = any picture.
    formats: list[str] = Field(default_factory=list)
    #: width / height.
    min_ratio: float = 0
    max_ratio: float = 0
    min_width: int = 0
    max_pixels: int = 0
    note: str = ""


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
    #: What a channel does with links unless its settings say otherwise.
    links: str = "keep"
    #: The links modes a channel may choose (X: remove, defang).
    link_modes: list[str] = Field(default_factory=lambda: ["keep", "remove", "defang"])
    #: The platform's limits for one picture; None when it states none.
    image: ImageRules | None = None


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
    #: repost: the channel that reposts.
    repost_by: str | None = None
    #: notify_people, notify_person: the event to tell people about.
    event_id: str | None = None
    #: notify_person: who gets the email.
    user_id: str | None = None


class Job(StaqlyModel):
    """One unit of background work."""

    id: str
    #: publish_post, publish_target, target_timeout, run_schedule, refresh_channel, repeat_post,
    #: publish_reply or repost.
    kind: str
    #: Unique per subject (``publish_post:<post id>``); queueing the same key again replaces a queued job.
    key: str = ""
    #: queued, running, done, failed or cancelled.
    status: str
    run_at: datetime
    attempts: int = 0
    max_attempts: int = 3
    #: Deprecated: always ``None`` (the queue no longer uses leases).
    lease_until: OptionalDatetime = None
    #: The worker that ran the last attempt.
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
    """False for a data-only export (Starter plan): the ZIP has no media files."""


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
