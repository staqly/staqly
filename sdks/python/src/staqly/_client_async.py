"""Async client and resources — the source of truth.

``_client_sync.py`` is GENERATED from this file by ``scripts/gen_sync.py``
(``make gen``): ``async``/``await`` are stripped and ``Async*`` names lose
their prefix, so both clients always expose the same surface. Lines ending in
``# async-only`` are dropped from the sync version.
"""

from __future__ import annotations

import builtins
import time
from collections.abc import AsyncIterator, Callable, Mapping, Sequence
from datetime import date, datetime
from os import PathLike
from pathlib import Path
from types import TracebackType
from typing import Any, TypeVar

import httpx

from ._http import (
    AsyncHTTPClient,
    FileInput,
    RequestOptions,
    RequestSpec,
    clean,
    open_for_write,
    read_file,
)
from .errors import ExportFailedError, WaitTimeoutError
from .models import (
    AccessToken,
    AccessTokenRotated,
    Agent,
    AIJob,
    AIStatus,
    Alert,
    AlertDelivery,
    AlertTestResult,
    Analytics,
    APIKey,
    Billing,
    BillingDetails,
    BillingURL,
    Channel,
    ChannelGroup,
    ChannelLink,
    ChannelOption,
    ChannelTestResult,
    Collection,
    Delivery,
    DesignBrand,
    DeviceLink,
    Event,
    EventPage,
    Health,
    Job,
    JobPage,
    JobStats,
    Liveness,
    Media,
    MediaFolder,
    MediaFolders,
    MediaPage,
    MediaProject,
    MediaUse,
    Member,
    Plan,
    Post,
    PostComment,
    PostMetrics,
    PostPage,
    PostPreview,
    PostTargetInput,
    PreviewLink,
    Provider,
    ProviderCheck,
    PurgeResult,
    Readiness,
    RecoveryKey,
    Schedule,
    ScheduleRule,
    Session,
    SessionCreated,
    SessionRecovered,
    SetupStatus,
    StaqlyModel,
    StockPage,
    Tag,
    TargetMetrics,
    TelegramConnect,
    ThreadPart,
    Webhook,
    Whoami,
    WorkItem,
    Workspace,
    WorkspaceCreated,
    WorkspaceExport,
    WorkspaceUsage,
    XPosts,
)

__all__ = ["AsyncStaqly"]

M = TypeVar("M", bound=StaqlyModel)

#: Default ``wait_for_published`` timeout in seconds.
DEFAULT_WAIT_TIMEOUT = 600.0

TimeFilter = str | datetime | date
JSONMapping = Mapping[str, Any]


def _q(value: str) -> str:
    from urllib.parse import quote

    return quote(value, safe="")


def _opts(request_options: RequestOptions | None) -> RequestOptions:
    return request_options if request_options is not None else RequestOptions()


def _merged(defaults: RequestOptions, request_options: RequestOptions | None) -> RequestOptions:
    """``defaults`` overridden by the caller's options (headers are merged, not replaced)."""
    opts = RequestOptions(**defaults)
    user = _opts(request_options)
    opts.update(user)
    if "headers" in defaults and "headers" in user:
        opts["headers"] = {**defaults["headers"], **user["headers"]}
    return opts


def _model(cls: type[M], response: httpx.Response) -> M:
    return cls.model_validate(response.json())


def _models(cls: type[M], response: httpx.Response) -> builtins.list[M]:
    return [cls.model_validate(x) for x in response.json()["data"]]


def _dump(value: Any) -> Any:
    if isinstance(value, StaqlyModel):
        return value.model_dump(exclude_none=True, mode="json")
    if isinstance(value, Sequence) and not isinstance(value, (str, bytes)):
        return [_dump(v) for v in value]
    return value


def _when(v: TimeFilter | None) -> str | None:
    """A time for a JSON body: datetimes become ISO 8601, strings pass through (``""`` clears)."""
    if isinstance(v, (datetime, date)):
        return v.isoformat()
    return v


def _day(v: str | date | None) -> str | None:
    """A calendar day as YYYY-MM-DD (a datetime keeps only its date)."""
    if isinstance(v, datetime):
        return v.date().isoformat()
    if isinstance(v, date):
        return v.isoformat()
    return v


def _csv(v: str | Sequence[str] | None) -> str | None:
    """One value or several, as the API's comma list."""
    if v is None or isinstance(v, str):
        return v
    return ",".join(v) or None


class _Resource:
    def __init__(self, client: AsyncStaqly) -> None:
        self._client = client
        self._http = client._http


# ---------------------------------------------------------------- collections


class AsyncCollections(_Resource):
    """Collections group posts (e.g. "Blog posts") and carry the schedules that publish them."""

    async def list(
        self, *, include_archived: bool = False, request_options: RequestOptions | None = None
    ) -> builtins.list[Collection]:
        r = await self._http.request(
            RequestSpec(
                "GET",
                "/v1/collections",
                params={"include_archived": include_archived or None},
                options=_opts(request_options),
            )
        )
        return _models(Collection, r)

    async def get(self, id_or_name: str, *, request_options: RequestOptions | None = None) -> Collection:
        """Gets a collection by ID or exact (case-insensitive) name."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/collections/{_q(id_or_name)}", options=_opts(request_options))
        )
        return _model(Collection, r)

    async def create(
        self,
        name: str,
        *,
        description: str | None = None,
        icon: str | None = None,
        color: str | None = None,
        processing_timeout_seconds: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> Collection:
        """Creates a collection. ``processing_timeout_seconds`` (60 to 86400) is how long a target
        on your own automation may wait for a result before it fails; without it the server default
        applies."""
        body = clean(
            {
                "name": name,
                "description": description,
                "icon": icon,
                "color": color,
                "processing_timeout_seconds": processing_timeout_seconds,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/collections", json=body, options=_opts(request_options))
        )
        return _model(Collection, r)

    async def update(
        self,
        collection_id: str,
        *,
        name: str | None = None,
        description: str | None = None,
        icon: str | None = None,
        color: str | None = None,
        processing_timeout_seconds: int | None = None,
        clear_processing_timeout: bool = False,
        archived: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> Collection:
        """Renames, re-describes, archives or changes the processing timeout of a collection;
        ``clear_processing_timeout=True`` goes back to the server default."""
        body = clean(
            {
                "name": name,
                "description": description,
                "icon": icon,
                "color": color,
                "processing_timeout_seconds": processing_timeout_seconds,
                "archived": archived,
            }
        )
        if clear_processing_timeout:
            body["processing_timeout_seconds"] = None
        r = await self._http.request(
            RequestSpec(
                "PATCH", f"/v1/collections/{_q(collection_id)}", json=body, options=_opts(request_options)
            )
        )
        return _model(Collection, r)

    async def archive(
        self, collection_id: str, *, request_options: RequestOptions | None = None
    ) -> Collection:
        return await self.update(collection_id, archived=True, request_options=request_options)

    async def unarchive(
        self, collection_id: str, *, request_options: RequestOptions | None = None
    ) -> Collection:
        return await self.update(collection_id, archived=False, request_options=request_options)

    async def delete(
        self, collection_id: str, *, confirm: str, request_options: RequestOptions | None = None
    ) -> None:
        """Permanently deletes a collection and its posts; ``confirm`` must equal its name.
        Needs ``collections:delete`` (``collections:write`` is not enough)."""
        await self._http.request(
            RequestSpec(
                "DELETE",
                f"/v1/collections/{_q(collection_id)}",
                params={"confirm": confirm},
                options=_opts(request_options),
            )
        )

    async def list_posts(
        self,
        id_or_name: str,
        *,
        status: str | None = None,
        channel_id: str | None = None,
        tag: str | None = None,
        q: str | None = None,
        publish_after: TimeFilter | None = None,
        publish_before: TimeFilter | None = None,
        sort: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> PostPage:
        """One page of a collection's posts (ID or exact name); same filters as :meth:`AsyncPosts.list`."""
        params = {
            "status": status,
            "channel_id": channel_id,
            "tag": tag,
            "q": q,
            "publish_after": publish_after,
            "publish_before": publish_before,
            "sort": sort,
            "limit": limit,
            "cursor": cursor,
        }
        r = await self._http.request(
            RequestSpec(
                "GET",
                f"/v1/collections/{_q(id_or_name)}/posts",
                params=params,
                options=_opts(request_options),
            )
        )
        return _model(PostPage, r)

    async def resolve_id(self, id_or_name: str, *, request_options: RequestOptions | None = None) -> str:
        """Returns the collection ID for an ID or name (no request when given an ID)."""
        if id_or_name.startswith("col_"):
            return id_or_name
        return (await self.get(id_or_name, request_options=request_options)).id


# ---------------------------------------------------------------------- posts


def _targets(targets: Sequence[PostTargetInput | JSONMapping] | None) -> list[dict[str, Any]] | None:
    """Targets as the JSON body expects them (unset fields left out)."""
    if targets is None:
        return None
    return [t.model_dump(exclude_none=True) if isinstance(t, PostTargetInput) else dict(t) for t in targets]


def _thread(thread: Sequence[ThreadPart | JSONMapping] | None) -> list[dict[str, Any]] | None:
    """Thread parts as the JSON body expects them ([] clears the thread)."""
    if thread is None:
        return None
    return [t.model_dump(exclude_none=True) if isinstance(t, ThreadPart) else dict(t) for t in thread]


class AsyncPosts(_Resource):
    """Posts are what goes out: text, media and the channels they go to."""

    async def list(
        self,
        *,
        collection_id: str | None = None,
        status: str | None = None,
        channel_id: str | None = None,
        tag: str | None = None,
        q: str | None = None,
        publish_after: TimeFilter | None = None,
        publish_before: TimeFilter | None = None,
        sort: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> PostPage:
        """One page of posts. ``collection_id`` takes an id or an exact name; ``channel_id`` keeps
        posts with a target on that channel; ``q`` matches words of the content.

        ``sort``: publish_asc (default: scheduled posts first, soonest first, posts without a
        time last), publish_desc, created_desc, created_asc, updated_desc. ``limit``: 1-200.
        ``publish_after`` / ``publish_before`` select posts by their publish time (the calendar).
        """
        params = {
            "collection_id": collection_id,
            "status": status,
            "channel_id": channel_id,
            "tag": tag,
            "q": q,
            "publish_after": publish_after,
            "publish_before": publish_before,
            "sort": sort,
            "limit": limit,
            "cursor": cursor,
        }
        r = await self._http.request(
            RequestSpec("GET", "/v1/posts", params=params, options=_opts(request_options))
        )
        return _model(PostPage, r)

    async def search(
        self,
        q: str,
        *,
        collection_id: str | None = None,
        status: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> PostPage:
        """Posts whose content contains these words; one page."""
        return await self.list(
            q=q,
            collection_id=collection_id,
            status=status,
            limit=limit,
            cursor=cursor,
            request_options=request_options,
        )

    async def iterate(
        self,
        *,
        collection_id: str | None = None,
        status: str | None = None,
        channel_id: str | None = None,
        tag: str | None = None,
        q: str | None = None,
        publish_after: TimeFilter | None = None,
        publish_before: TimeFilter | None = None,
        sort: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> AsyncIterator[Post]:
        """Yields every matching post, fetching pages lazily (``limit`` is the page size)."""
        while True:
            page = await self.list(
                collection_id=collection_id,
                status=status,
                channel_id=channel_id,
                tag=tag,
                q=q,
                publish_after=publish_after,
                publish_before=publish_before,
                sort=sort,
                limit=limit,
                cursor=cursor,
                request_options=request_options,
            )
            for post in page.data:
                yield post
            if not page.next_cursor:
                return
            cursor = page.next_cursor

    async def get(self, post_id: str, *, request_options: RequestOptions | None = None) -> Post:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/posts/{_q(post_id)}", options=_opts(request_options))
        )
        return _model(Post, r)

    async def create(
        self,
        collection: str,
        *,
        content: str | None = None,
        media: Sequence[str] | None = None,
        targets: Sequence[PostTargetInput | JSONMapping] | None = None,
        publish_at: TimeFilter | None = None,
        status: str | None = None,
        publish_now: bool | None = None,
        tags: Sequence[str] | None = None,
        metadata: JSONMapping | None = None,
        source: str | None = None,
        external_ref: str | None = None,
        repeat_every: str | None = None,
        thread: Sequence[ThreadPart | JSONMapping] | None = None,
        request_options: RequestOptions | None = None,
    ) -> Post:
        """Creates a post in a collection (id or exact name): a draft unless ``status="scheduled"``
        (needs ``publish_at`` in the future and a target) or ``publish_now=True``.
        ``repeat_every`` (``1d``..``6d``, ``1w``, ``2w``, ``1mo``) schedules a copy that much later
        each time the post is published. ``thread`` holds replies published under the post (each
        ``{"content", "media", "delay_seconds"}`` or :class:`~staqly.models.ThreadPart`).

        ``media`` are ids from :meth:`AsyncMediaResource.upload`; each target is a channel the post
        goes to (``{"channel_id": ...}`` or :class:`~staqly.models.PostTargetInput`, with optional
        text of its own and provider settings). Needs ``posts:write``, plus ``posts:publish`` to
        schedule or publish.
        """
        body = clean(
            {
                "collection": collection,
                "content": content,
                "media": list(media) if media is not None else None,
                "targets": _targets(targets),
                "publish_at": _when(publish_at),
                "status": status,
                "publish_now": publish_now,
                "tags": list(tags) if tags is not None else None,
                "metadata": dict(metadata) if metadata is not None else None,
                "source": source,
                "external_ref": external_ref,
                "repeat_every": repeat_every,
                "thread": _thread(thread),
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/posts", json=body, options=_opts(request_options))
        )
        return _model(Post, r)

    async def update(
        self,
        post_id: str,
        *,
        collection: str | None = None,
        content: str | None = None,
        media: Sequence[str] | None = None,
        targets: Sequence[PostTargetInput | JSONMapping] | None = None,
        publish_at: TimeFilter | None = None,
        clear_publish_at: bool = False,
        tags: Sequence[str] | None = None,
        metadata: JSONMapping | None = None,
        external_ref: str | None = None,
        repeat_every: str | None = None,
        thread: Sequence[ThreadPart | JSONMapping] | None = None,
        expected_version: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> Post:
        """Edits a draft, scheduled or failed post. Fields left out stay as they are; ``targets``,
        ``media`` and ``tags`` replace the list; ``clear_publish_at=True`` drops the time (a
        scheduled post becomes a draft); ``collection`` moves the post. ``expected_version`` makes
        the edit conditional (409 ``post_changed``). ``repeat_every=""`` stops repeating; ``thread`` replaces
        the replies (``[]`` clears them). A published
        or publishing post is not editable (409 ``not_editable``)."""
        body = clean(
            {
                "collection": collection,
                "content": content,
                "media": list(media) if media is not None else None,
                "targets": _targets(targets),
                "publish_at": _when(publish_at),
                "tags": list(tags) if tags is not None else None,
                "metadata": dict(metadata) if metadata is not None else None,
                "external_ref": external_ref,
                "repeat_every": repeat_every,
                "thread": _thread(thread),
                "expected_version": expected_version,
            }
        )
        if clear_publish_at:
            body["publish_at"] = None
        r = await self._http.request(
            RequestSpec("PATCH", f"/v1/posts/{_q(post_id)}", json=body, options=_opts(request_options))
        )
        return _model(Post, r)

    async def delete(self, post_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Deletes a draft, scheduled or failed post (its events stay as history). A post that went out
        (published, publishing, or published on any channel) is history: ``ConflictError``. Needs
        ``posts:delete``."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/posts/{_q(post_id)}", options=_opts(request_options))
        )

    async def delete_many(
        self, post_ids: Sequence[str], *, request_options: RequestOptions | None = None
    ) -> dict[str, builtins.list[str]]:
        """Deletes several posts (up to 200); returns ``{"deleted": [...], "missing": [...], "kept": [...]}``.
        Unknown ids, and ids of another workspace, are listed in ``missing``; posts that went out are history
        and listed in ``kept``. Needs ``posts:delete``."""
        r = await self._http.request(
            RequestSpec(
                "POST", "/v1/posts/delete", json={"ids": list(post_ids)}, options=_opts(request_options)
            )
        )
        out: dict[str, builtins.list[str]] = r.json()
        return out

    async def purge(self, post_id: str, *, request_options: RequestOptions | None = None) -> PurgeResult:
        """Deletes the post and its whole history (events, webhook deliveries, media no other post
        uses); only a content-free ``post.purged`` event remains. Needs ``admin``."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/posts/{_q(post_id)}/purge", options=_opts(request_options))
        )
        return _model(PurgeResult, r)

    async def schedule(
        self, post_id: str, publish_at: TimeFilter, *, request_options: RequestOptions | None = None
    ) -> Post:
        """Sets the publish time and makes the post scheduled (from draft, scheduled or failed)."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                f"/v1/posts/{_q(post_id)}/schedule",
                json={"publish_at": _when(publish_at)},
                options=_opts(request_options),
            )
        )
        return _model(Post, r)

    async def publish(self, post_id: str, *, request_options: RequestOptions | None = None) -> Post:
        """Publishes now: the post becomes ``publishing`` and a job publishes each target. It ends
        ``published`` when every target is, ``failed`` when one is not. Needs ``posts:publish``."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/posts/{_q(post_id)}/publish", options=_opts(request_options))
        )
        return _model(Post, r)

    async def cancel(self, post_id: str, *, request_options: RequestOptions | None = None) -> Post:
        """Takes a scheduled post off the schedule; it becomes a draft."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/posts/{_q(post_id)}/cancel", options=_opts(request_options))
        )
        return _model(Post, r)

    async def duplicate(self, post_id: str, *, request_options: RequestOptions | None = None) -> Post:
        """Copies content, media, tags, metadata and targets into a new draft."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/posts/{_q(post_id)}/duplicate", options=_opts(request_options))
        )
        return _model(Post, r)

    async def retry_target(
        self, post_id: str, channel_id: str, *, request_options: RequestOptions | None = None
    ) -> Post:
        """Tries a failed target once more, now."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                f"/v1/posts/{_q(post_id)}/targets/{_q(channel_id)}/retry",
                options=_opts(request_options),
            )
        )
        return _model(Post, r)

    async def report_result(
        self,
        post_id: str,
        channel_id: str,
        *,
        status: str,
        url: str | None = None,
        external_id: str | None = None,
        error: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Post:
        """Closes a target on your own automation (a channel of provider ``custom``) that waits for
        a result: ``status="published"`` (with ``url`` / ``external_id``) or ``status="failed"``
        (with ``error``). Raises :class:`~staqly.ConflictError` unless the target is publishing."""
        body = clean({"status": status, "url": url, "external_id": external_id, "error": error})
        r = await self._http.request(
            RequestSpec(
                "POST",
                f"/v1/posts/{_q(post_id)}/targets/{_q(channel_id)}/result",
                json=body,
                options=_opts(request_options),
            )
        )
        return _model(Post, r)

    async def preview(self, post_id: str, *, request_options: RequestOptions | None = None) -> PostPreview:
        """The text each target will carry and what its provider objects to."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/posts/{_q(post_id)}/preview", options=_opts(request_options))
        )
        return _model(PostPreview, r)

    async def metrics(
        self, post_id: str, *, refresh: bool = False, request_options: RequestOptions | None = None
    ) -> builtins.list[TargetMetrics]:
        """The post's numbers per channel, read again when older than ten minutes or with refresh."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                f"/v1/posts/{_q(post_id)}/metrics",
                params={"refresh": "true" if refresh else None},
                options=_opts(request_options),
            )
        )
        return _models(TargetMetrics, r)

    async def report_metrics(
        self,
        post_id: str,
        channel_id: str,
        metrics: PostMetrics | dict[str, int],
        *,
        request_options: RequestOptions | None = None,
    ) -> TargetMetrics:
        """Your own automation sends the numbers of a post it published on a custom channel."""
        r = await self._http.request(
            RequestSpec(
                "PUT",
                f"/v1/posts/{_q(post_id)}/targets/{_q(channel_id)}/metrics",
                json=_dump(metrics),
                options=_opts(request_options),
            )
        )
        return _model(TargetMetrics, r)

    async def create_preview_link(
        self, post_id: str, *, expires_in_days: int = 0, request_options: RequestOptions | None = None
    ) -> PreviewLink:
        """A link to a read-only preview page anyone can comment on; ``url`` is returned only now."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                f"/v1/posts/{_q(post_id)}/previews",
                json={"expires_in_days": expires_in_days},
                options=_opts(request_options),
            )
        )
        return _model(PreviewLink, r)

    async def preview_links(
        self, post_id: str, *, request_options: RequestOptions | None = None
    ) -> builtins.list[PreviewLink]:
        """The post's preview links, without their tokens."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/posts/{_q(post_id)}/previews", options=_opts(request_options))
        )
        return _models(PreviewLink, r)

    async def revoke_preview_link(
        self, post_id: str, preview_id: str, *, request_options: RequestOptions | None = None
    ) -> None:
        """Stops a preview link from working."""
        await self._http.request(
            RequestSpec(
                "DELETE", f"/v1/posts/{_q(post_id)}/previews/{_q(preview_id)}", options=_opts(request_options)
            )
        )

    async def comments(
        self, post_id: str, *, request_options: RequestOptions | None = None
    ) -> builtins.list[PostComment]:
        """The post's feedback threads (reviewers' comments and the team's replies), oldest first,
        every page."""
        out: builtins.list[PostComment] = []
        cursor: str | None = None
        while True:
            r = await self._http.request(
                RequestSpec(
                    "GET",
                    f"/v1/posts/{_q(post_id)}/comments",
                    params={"cursor": cursor},
                    options=_opts(request_options),
                )
            )
            out.extend(_models(PostComment, r))
            cursor = r.json().get("next_cursor")
            if not cursor:
                return out

    async def add_comment(
        self,
        post_id: str,
        *,
        body: str,
        parent_id: str | None = None,
        quote: str | None = None,
        channel_id: str | None = None,
        part: int | None = None,
        anchor_start: int | None = None,
        anchor_end: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> PostComment:
        """Comments as the team: a reply to a thread (``parent_id``) or a new thread (on a passage)."""
        payload: dict[str, Any] = {"body": body}
        for key, value in (
            ("parent_id", parent_id),
            ("quote", quote),
            ("channel_id", channel_id),
            ("part", part),
            ("anchor_start", anchor_start),
            ("anchor_end", anchor_end),
        ):
            if value is not None:
                payload[key] = value
        r = await self._http.request(
            RequestSpec(
                "POST", f"/v1/posts/{_q(post_id)}/comments", json=payload, options=_opts(request_options)
            )
        )
        return _model(PostComment, r)

    async def resolve_comment(
        self, post_id: str, comment_id: str, *, request_options: RequestOptions | None = None
    ) -> PostComment:
        """Resolves a feedback thread by its first comment."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                f"/v1/posts/{_q(post_id)}/comments/{_q(comment_id)}/resolve",
                options=_opts(request_options),
            )
        )
        return _model(PostComment, r)

    async def reopen_comment(
        self, post_id: str, comment_id: str, *, request_options: RequestOptions | None = None
    ) -> PostComment:
        """Reopens a resolved feedback thread."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                f"/v1/posts/{_q(post_id)}/comments/{_q(comment_id)}/reopen",
                options=_opts(request_options),
            )
        )
        return _model(PostComment, r)

    async def delete_comment(
        self, post_id: str, comment_id: str, *, request_options: RequestOptions | None = None
    ) -> None:
        """Removes a comment; removing a thread's first comment removes its replies."""
        await self._http.request(
            RequestSpec(
                "DELETE",
                f"/v1/posts/{_q(post_id)}/comments/{_q(comment_id)}",
                options=_opts(request_options),
            )
        )

    async def history(
        self,
        post_id: str,
        *,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> EventPage:
        """The post's history, newest first."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                f"/v1/posts/{_q(post_id)}/events",
                params={"limit": limit, "cursor": cursor},
                options=_opts(request_options),
            )
        )
        return _model(EventPage, r)

    async def tags(self, *, request_options: RequestOptions | None = None) -> builtins.list[Tag]:
        """The workspace's tags, most used first."""
        r = await self._http.request(RequestSpec("GET", "/v1/tags", options=_opts(request_options)))
        return _models(Tag, r)

    async def wait_for_published(
        self,
        post_id: str,
        *,
        timeout: float = DEFAULT_WAIT_TIMEOUT,
        poll_interval: float = 2.0,
        on_poll: Callable[[Post], None] | None = None,
        request_options: RequestOptions | None = None,
    ) -> Post:
        """Polls the post until it is ``published`` or ``failed`` and returns it (check ``status``
        and the targets' ``error``). Raises :class:`~staqly.WaitTimeoutError` after ``timeout``
        seconds. For push instead of waiting, subscribe a webhook to ``post.published`` /
        ``post.failed``."""
        started = time.monotonic()
        deadline = started + timeout
        while True:
            post = await self.get(post_id, request_options=request_options)
            if on_poll is not None:
                on_poll(post)
            if post.status in ("published", "failed"):
                return post
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise WaitTimeoutError(post_id, time.monotonic() - started, post)
            await self._http._sleep(min(max(poll_interval, 0.1), remaining))


# ---------------------------------------------------------------------- media


class AsyncMediaResource(_Resource):
    """Uploaded files (images, videos). Posts reference them by id in ``media``."""

    async def upload(
        self,
        file: FileInput,
        *,
        filename: str | None = None,
        content_type: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Media:
        """Uploads a file; the type is detected from its content (SVG/HTML are rejected with 415)."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/media",
                files={"file": read_file(file, filename, content_type)},
                options=_opts(request_options),
            )
        )
        return _model(Media, r)

    async def download(self, media_id: str, *, request_options: RequestOptions | None = None) -> bytes:
        """Downloads a file's bytes."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/media/{_q(media_id)}", options=_opts(request_options))
        )
        return r.content

    async def replace(
        self,
        media_id: str,
        file: FileInput,
        *,
        filename: str | None = None,
        content_type: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Media:
        """Replaces a file with its edit: the new file takes the original's name, alt text, folder and
        tags, drafts that used the original use it instead, and the original moves to Deleted. A file
        a scheduled or sent post uses answers 409. Needs ``media:write``, ``posts:write`` and
        ``posts:delete``."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                f"/v1/media/{_q(media_id)}/replace",
                files={"file": read_file(file, filename, content_type)},
                options=_opts(request_options),
            )
        )
        return _model(Media, r)

    def url(self, media_id: str) -> str:
        """Absolute URL of a file (requires the same authentication as the API)."""
        return self._http.url(f"/v1/media/{_q(media_id)}")

    async def list(
        self,
        *,
        q: str | None = None,
        kind: str | None = None,
        used: str | None = None,
        folder: str | None = None,
        tag: str | None = None,
        deleted: bool | None = None,
        duplicates: bool | None = None,
        sort: str | None = None,
        offset: int | None = None,
        limit: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> MediaPage:
        """The media library, newest first: filter by name (``q``), ``kind`` (image, video, audio) and
        ``used`` (used, unused), ``folder`` (a folder id or ``"root"``) and ``tag``;
        ``deleted=True`` lists the files in Deleted, ``duplicates=True`` only files with an identical twin;
        ``sort`` is
        created_desc, created_asc, size_desc or name_asc. Page with
        ``offset``/``limit``; :attr:`MediaPage.next_offset` is where the next page starts."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                "/v1/media",
                params={
                    "q": q,
                    "kind": kind,
                    "used": used,
                    "folder": folder,
                    "tag": tag,
                    "deleted": None if deleted is None else str(deleted).lower(),
                    "duplicates": None if duplicates is None else str(duplicates).lower(),
                    "sort": sort,
                    "offset": offset,
                    "limit": limit,
                },
                options=_opts(request_options),
            )
        )
        return _model(MediaPage, r)

    async def info(self, media_id: str, *, request_options: RequestOptions | None = None) -> Media:
        """A file's details (name, type, size, alt text, pixels, length)."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/media/{_q(media_id)}/info", options=_opts(request_options))
        )
        return _model(Media, r)

    async def update(
        self,
        media_id: str,
        *,
        name: str | None = None,
        alt_text: str | None = None,
        width: int | None = None,
        height: int | None = None,
        duration_ms: int | None = None,
        folder_id: str | None = None,
        tags: Sequence[str] | None = None,
        request_options: RequestOptions | None = None,
    ) -> Media:
        """Renames a file, sets its alt text, moves it to a folder (``folder_id``; ``""`` = the top
        level), replaces its ``tags``, or records a video's size and length. Needs ``media:write``."""
        body = clean(
            {
                "name": name,
                "alt_text": alt_text,
                "width": width,
                "height": height,
                "duration_ms": duration_ms,
                "folder_id": folder_id,
                "tags": None if tags is None else list(tags),
            }
        )
        r = await self._http.request(
            RequestSpec("PATCH", f"/v1/media/{_q(media_id)}", json=body, options=_opts(request_options))
        )
        return _model(Media, r)

    async def usage(
        self, media_id: str, *, request_options: RequestOptions | None = None
    ) -> builtins.list[MediaUse]:
        """The posts that use a file, newest first (at most 50)."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/media/{_q(media_id)}/usage", options=_opts(request_options))
        )
        return _models(MediaUse, r)

    async def delete_many(
        self,
        media_ids: Sequence[str],
        *,
        permanent: bool = False,
        request_options: RequestOptions | None = None,
    ) -> dict[str, builtins.list[str]]:
        """Moves several files to Deleted (``permanent=True``: deletes them for good); returns
        ``{"deleted": [...], "missing": [...]}``. A file a scheduled post uses refuses the whole call (409).
        Needs ``posts:delete``."""
        body: dict[str, object] = {"ids": list(media_ids)}
        if permanent:
            body["permanent"] = True
        r = await self._http.request(
            RequestSpec("POST", "/v1/media/delete", json=body, options=_opts(request_options))
        )
        out: dict[str, builtins.list[str]] = r.json()
        return out

    async def restore(self, media_id: str, *, request_options: RequestOptions | None = None) -> Media:
        """Takes a file out of Deleted, back where it was. Needs ``posts:delete``."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/media/{_q(media_id)}/restore", options=_opts(request_options))
        )
        return _model(Media, r)

    async def keep(
        self, media_id: str, *, request_options: RequestOptions | None = None
    ) -> dict[str, builtins.list[str]]:
        """Keeps this file of a set of identical ones: posts using a copy use it instead and the copies
        move to Deleted. Returns ``{"deleted": [...]}``. Needs ``posts:delete`` and ``posts:write``."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/media/{_q(media_id)}/keep", options=_opts(request_options))
        )
        out: dict[str, builtins.list[str]] = r.json()
        return out

    async def organize(
        self,
        media_ids: Sequence[str],
        *,
        folder_id: str | None = None,
        add_tags: Sequence[str] | None = None,
        remove_tags: Sequence[str] | None = None,
        request_options: RequestOptions | None = None,
    ) -> dict[str, builtins.list[str]]:
        """Moves up to 200 files to a folder (``folder_id``; ``""`` = the top level) and/or adds and
        removes tags. Returns ``{"updated": [...], "missing": [...]}``. Needs ``media:write``."""
        body = clean(
            {
                "ids": list(media_ids),
                "folder_id": folder_id,
                "add_tags": None if add_tags is None else list(add_tags),
                "remove_tags": None if remove_tags is None else list(remove_tags),
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/media/bulk", json=body, options=_opts(request_options))
        )
        out: dict[str, builtins.list[str]] = r.json()
        return out

    async def folders(self, *, request_options: RequestOptions | None = None) -> MediaFolders:
        """The library's folders (flat, with ``parent_id``), file counts and tags with counts."""
        r = await self._http.request(RequestSpec("GET", "/v1/media-folders", options=_opts(request_options)))
        return _model(MediaFolders, r)

    async def create_folder(
        self, name: str, *, parent_id: str | None = None, request_options: RequestOptions | None = None
    ) -> MediaFolder:
        """Creates a folder at the top level or inside ``parent_id``. Needs ``media:write``."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/media-folders",
                json=clean({"name": name, "parent_id": parent_id}),
                options=_opts(request_options),
            )
        )
        return _model(MediaFolder, r)

    async def update_folder(
        self,
        folder_id: str,
        *,
        name: str | None = None,
        parent_id: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> MediaFolder:
        """Renames a folder or moves it under another one (``parent_id=""`` = the top level)."""
        r = await self._http.request(
            RequestSpec(
                "PATCH",
                f"/v1/media-folders/{_q(folder_id)}",
                json=clean({"name": name, "parent_id": parent_id}),
                options=_opts(request_options),
            )
        )
        return _model(MediaFolder, r)

    async def delete_folder(self, folder_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Deletes a folder; its files and subfolders move up to its parent (no file is deleted)."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/media-folders/{_q(folder_id)}", options=_opts(request_options))
        )

    async def delete(
        self, media_id: str, *, permanent: bool = False, request_options: RequestOptions | None = None
    ) -> None:
        """Moves a file to Deleted, restorable for 30 days (409 while a scheduled post uses it); with
        ``permanent=True`` deletes it for good now and posts referencing it lose it.
        Needs ``posts:delete``."""
        await self._http.request(
            RequestSpec(
                "DELETE",
                f"/v1/media/{_q(media_id)}",
                params={"permanent": "true"} if permanent else None,
                options=_opts(request_options),
            )
        )


# --------------------------------------------------------------------- events


class AsyncEvents(_Resource):
    """The workspace activity log, newest first."""

    async def list(
        self,
        *,
        post_id: str | None = None,
        collection_id: str | None = None,
        channel_id: str | None = None,
        schedule_id: str | None = None,
        type: str | None = None,
        agent: str | None = None,
        actor_type: str | None = None,
        actor_id: str | None = None,
        type_prefix: str | None = None,
        from_: str | None = None,
        to: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> EventPage:
        params = {
            "post_id": post_id,
            "collection_id": collection_id,
            "channel_id": channel_id,
            "schedule_id": schedule_id,
            "type": type,
            "agent": agent,
            "actor_type": actor_type,
            "actor_id": actor_id,
            "type_prefix": type_prefix,
            "from": from_,
            "to": to,
            "limit": limit,
            "cursor": cursor,
        }
        r = await self._http.request(
            RequestSpec("GET", "/v1/events", params=params, options=_opts(request_options))
        )
        return _model(EventPage, r)

    async def iterate(
        self,
        *,
        post_id: str | None = None,
        collection_id: str | None = None,
        channel_id: str | None = None,
        schedule_id: str | None = None,
        type: str | None = None,
        agent: str | None = None,
        actor_type: str | None = None,
        actor_id: str | None = None,
        type_prefix: str | None = None,
        from_: str | None = None,
        to: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> AsyncIterator[Event]:
        """Yields every matching event across pages."""
        while True:
            page = await self.list(
                post_id=post_id,
                collection_id=collection_id,
                channel_id=channel_id,
                schedule_id=schedule_id,
                type=type,
                agent=agent,
                actor_type=actor_type,
                actor_id=actor_id,
                type_prefix=type_prefix,
                from_=from_,
                to=to,
                limit=limit,
                cursor=cursor,
                request_options=request_options,
            )
            for event in page.data:
                yield event
            if not page.next_cursor:
                return
            cursor = page.next_cursor


# ------------------------------------------------------------------ analytics


class AsyncAnalyticsResource(_Resource):
    """How publishing went: totals, channels, sources, posting times, top posts and failures."""

    async def get(
        self,
        *,
        from_: str | date | None = None,
        to: str | date | None = None,
        days: int | None = None,
        channel: str | None = None,
        collection: str | None = None,
        source: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Analytics:
        """The report for a range of days in the workspace's time zone (``from_`` and ``to``
        both included, YYYY-MM-DD; default the last 30 days, at most 366). ``days`` instead of
        ``from_``: the last n days up to ``to`` (sending both is a 422). Needs ``posts:read``."""
        params = {
            "from": _day(from_),
            "to": _day(to),
            "days": days,
            "channel": channel,
            "collection": collection,
            "source": source,
        }
        r = await self._http.request(
            RequestSpec("GET", "/v1/analytics", params=params, options=_opts(request_options))
        )
        return _model(Analytics, r)


# ------------------------------------------------------------------- webhooks


class AsyncDeliveries(_Resource):
    """Delivery history of outgoing webhooks."""

    async def list(
        self,
        *,
        webhook_id: str | None = None,
        post_id: str | None = None,
        status: str | None = None,
        limit: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> builtins.list[Delivery]:
        """The most recent deliveries, newest first (``limit``: 50 by default, at most 200)."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                "/v1/webhook-deliveries",
                params={"webhook_id": webhook_id, "post_id": post_id, "status": status, "limit": limit},
                options=_opts(request_options),
            )
        )
        return _models(Delivery, r)

    async def retry(self, delivery_id: str, *, request_options: RequestOptions | None = None) -> Delivery:
        """Re-queues a finished (succeeded or failed) delivery."""
        r = await self._http.request(
            RequestSpec(
                "POST", f"/v1/webhook-deliveries/{_q(delivery_id)}/retry", options=_opts(request_options)
            )
        )
        return _model(Delivery, r)


class AsyncWebhooks(_Resource):
    """Signed HTTP callbacks for events (at-least-once). Verify them with :func:`staqly.verify_webhook`."""

    def __init__(self, client: AsyncStaqly) -> None:
        super().__init__(client)
        self.deliveries = AsyncDeliveries(client)

    async def list(self, *, request_options: RequestOptions | None = None) -> builtins.list[Webhook]:
        r = await self._http.request(RequestSpec("GET", "/v1/webhooks", options=_opts(request_options)))
        return _models(Webhook, r)

    async def get(self, webhook_id: str, *, request_options: RequestOptions | None = None) -> Webhook:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/webhooks/{_q(webhook_id)}", options=_opts(request_options))
        )
        return _model(Webhook, r)

    async def create(
        self,
        url: str,
        *,
        events: Sequence[str] | None = None,
        description: str | None = None,
        collection_ids: Sequence[str] | None = None,
        active: bool | None = None,
        timeout_ms: int | None = None,
        max_attempts: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> Webhook:
        """Creates a webhook. ``events`` are names or globs (``"post.*"``, default ``["*"]``);
        the response carries the generated signing ``secret``."""
        body = clean(
            {
                "url": url,
                "events": list(events) if events is not None else None,
                "description": description,
                "collection_ids": list(collection_ids) if collection_ids is not None else None,
                "active": active,
                "timeout_ms": timeout_ms,
                "max_attempts": max_attempts,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/webhooks", json=body, options=_opts(request_options))
        )
        return _model(Webhook, r)

    async def update(
        self,
        webhook_id: str,
        *,
        url: str | None = None,
        events: Sequence[str] | None = None,
        description: str | None = None,
        collection_ids: Sequence[str] | None = None,
        active: bool | None = None,
        timeout_ms: int | None = None,
        max_attempts: int | None = None,
        rotate_secret: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> Webhook:
        body = clean(
            {
                "url": url,
                "events": list(events) if events is not None else None,
                "description": description,
                "collection_ids": list(collection_ids) if collection_ids is not None else None,
                "active": active,
                "timeout_ms": timeout_ms,
                "max_attempts": max_attempts,
                "rotate_secret": rotate_secret,
            }
        )
        r = await self._http.request(
            RequestSpec("PATCH", f"/v1/webhooks/{_q(webhook_id)}", json=body, options=_opts(request_options))
        )
        return _model(Webhook, r)

    async def rotate_secret(
        self, webhook_id: str, *, request_options: RequestOptions | None = None
    ) -> Webhook:
        """Issues a new signing secret (the old one stops working immediately)."""
        return await self.update(webhook_id, rotate_secret=True, request_options=request_options)

    async def delete(self, webhook_id: str, *, request_options: RequestOptions | None = None) -> None:
        await self._http.request(
            RequestSpec("DELETE", f"/v1/webhooks/{_q(webhook_id)}", options=_opts(request_options))
        )

    async def test(self, webhook_id: str, *, request_options: RequestOptions | None = None) -> Delivery:
        """Queues a ``webhook.test`` delivery."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/webhooks/{_q(webhook_id)}/test", options=_opts(request_options))
        )
        return _model(Delivery, r)


# ------------------------------------------------------------------- channels


class AsyncChannels(_Resource):
    """Connected accounts posts go to. Credentials are stored sealed and never returned."""

    async def list(
        self, *, group: str | None = None, request_options: RequestOptions | None = None
    ) -> builtins.list[Channel]:
        """Lists the channels with their ``group_id`` and ``activity`` (last and next post);
        ``group`` (a channel group id) keeps one group."""
        r = await self._http.request(
            RequestSpec("GET", "/v1/channels", params={"group": group}, options=_opts(request_options))
        )
        return _models(Channel, r)

    async def list_groups(
        self, *, request_options: RequestOptions | None = None
    ) -> builtins.list[ChannelGroup]:
        """Lists the channel groups (Default first) with how many channels each holds."""
        r = await self._http.request(RequestSpec("GET", "/v1/channel-groups", options=_opts(request_options)))
        return _models(ChannelGroup, r)

    async def create_group(
        self,
        name: str,
        *,
        color: str | None = None,
        position: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> ChannelGroup:
        """Creates a channel group (``name`` unique in the workspace; ``color`` like ``#22c55e``)."""
        body = clean({"name": name, "color": color, "position": position})
        r = await self._http.request(
            RequestSpec("POST", "/v1/channel-groups", json=body, options=_opts(request_options))
        )
        return _model(ChannelGroup, r)

    async def update_group(
        self,
        group_id: str,
        *,
        name: str | None = None,
        color: str | None = None,
        position: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> ChannelGroup:
        """Renames a channel group or changes its colour or position."""
        body = clean({"name": name, "color": color, "position": position})
        r = await self._http.request(
            RequestSpec(
                "PATCH", f"/v1/channel-groups/{_q(group_id)}", json=body, options=_opts(request_options)
            )
        )
        return _model(ChannelGroup, r)

    async def delete_group(self, group_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Deletes a channel group; its channels move to Default, which can't be deleted."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/channel-groups/{_q(group_id)}", options=_opts(request_options))
        )

    async def create_link(
        self,
        *,
        note: str = "",
        providers: builtins.list[str] | None = None,
        group_id: str = "",
        expires_in_hours: int = 0,
        max_uses: int = 0,
        request_options: RequestOptions | None = None,
    ) -> ChannelLink:
        """A connect link someone without access opens to connect a channel; ``url`` is returned only now."""
        body: dict[str, Any] = {
            "note": note,
            "group_id": group_id,
            "expires_in_hours": expires_in_hours,
            "max_uses": max_uses,
        }
        if providers is not None:
            body["providers"] = providers
        r = await self._http.request(
            RequestSpec("POST", "/v1/channel-links", json=body, options=_opts(request_options))
        )
        return _model(ChannelLink, r)

    async def links(self, *, request_options: RequestOptions | None = None) -> builtins.list[ChannelLink]:
        """The workspace's connect links, without their tokens, newest first."""
        r = await self._http.request(RequestSpec("GET", "/v1/channel-links", options=_opts(request_options)))
        return _models(ChannelLink, r)

    async def revoke_link(self, link_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Stops a connect link from working; channels already connected through it stay."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/channel-links/{_q(link_id)}", options=_opts(request_options))
        )

    async def get(self, channel_id: str, *, request_options: RequestOptions | None = None) -> Channel:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/channels/{_q(channel_id)}", options=_opts(request_options))
        )
        return _model(Channel, r)

    async def connect(
        self,
        provider: str,
        *,
        name: str | None = None,
        fields: Mapping[str, str] | None = None,
        settings: JSONMapping | None = None,
        group_id: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Channel:
        """Connects a channel on a token platform (``fields`` carries the provider's
        ``credential_fields``) or your own automation (provider ``custom``, no fields). OAuth
        platforms connect through :meth:`oauth_start` instead (422 here). Needs ``channels:manage``."""
        body = clean(
            {
                "provider": provider,
                "name": name,
                "fields": dict(fields) if fields is not None else None,
                "settings": dict(settings) if settings is not None else None,
                "group_id": group_id,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/channels", json=body, options=_opts(request_options))
        )
        return _model(Channel, r)

    async def update(
        self,
        channel_id: str,
        *,
        name: str | None = None,
        settings: JSONMapping | None = None,
        status: str | None = None,
        fields: Mapping[str, str] | None = None,
        group_id: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Channel:
        """Renames, changes defaults (``settings`` replaces them; ``settings={"links": "remove"}``
        sets what the channel does with links: keep, remove or defang, within the provider's
        ``limits.link_modes``), turns off or on (``status``: connected or disabled), or
        reconnects a token platform with fresh ``fields``. ``group_id`` moves the channel to
        another channel group."""
        body = clean(
            {
                "name": name,
                "settings": dict(settings) if settings is not None else None,
                "status": status,
                "fields": dict(fields) if fields is not None else None,
                "group_id": group_id,
            }
        )
        r = await self._http.request(
            RequestSpec("PATCH", f"/v1/channels/{_q(channel_id)}", json=body, options=_opts(request_options))
        )
        return _model(Channel, r)

    async def disable(self, channel_id: str, *, request_options: RequestOptions | None = None) -> Channel:
        """Turns the channel off: targets on it fail without trying."""
        return await self.update(channel_id, status="disabled", request_options=request_options)

    async def enable(self, channel_id: str, *, request_options: RequestOptions | None = None) -> Channel:
        return await self.update(channel_id, status="connected", request_options=request_options)

    async def remove(self, channel_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Removes the channel; targets on it go with it."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/channels/{_q(channel_id)}", options=_opts(request_options))
        )

    async def test(
        self, channel_id: str, *, request_options: RequestOptions | None = None
    ) -> ChannelTestResult:
        """Checks the stored credentials against the platform; problems are reported in
        ``ok``/``error``, not raised."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/channels/{_q(channel_id)}/test", options=_opts(request_options))
        )
        return _model(ChannelTestResult, r)

    async def oauth_start(
        self, provider: str, *, instance: str | None = None, request_options: RequestOptions | None = None
    ) -> str:
        """Begins connecting an OAuth platform: returns the URL to open in a browser signed in to
        the platform. ``instance`` is the server for OAuth platforms with many servers."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                f"/v1/channels/oauth/{_q(provider)}/start",
                params={"instance": instance},
                options=_opts(request_options),
            )
        )
        url: str = r.json()["url"]
        return url

    async def options(
        self, channel_id: str, key: str, *, request_options: RequestOptions | None = None
    ) -> builtins.list[ChannelOption]:
        """The choices of a channel's remote setting (a settings field of kind ``remote``), read from
        its platform: for Slack, ``channel`` lists the channels a post can go to."""
        r = await self._http.request(
            RequestSpec(
                "GET", f"/v1/channels/{_q(channel_id)}/options/{_q(key)}", options=_opts(request_options)
            )
        )
        return _models(ChannelOption, r)

    async def telegram_connect(
        self, *, group_id: str | None = None, request_options: RequestOptions | None = None
    ) -> TelegramConnect:
        """A code to connect a Telegram chat through the server's bot: add ``bot`` to the chat and send
        ``command`` there; the code lasts 10 minutes. 409 when the server has no bot of its own."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/channels/telegram/connect",
                json=clean({"group_id": group_id}),
                options=_opts(request_options),
            )
        )
        return _model(TelegramConnect, r)

    async def telegram_connect_status(
        self, code: str, *, request_options: RequestOptions | None = None
    ) -> TelegramConnect:
        """Whether the chat for a Telegram connect code is connected yet (``status``, ``channel_id``)."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/channels/telegram/connect/{_q(code)}", options=_opts(request_options))
        )
        return _model(TelegramConnect, r)


class AsyncProviders(_Resource):
    """The platform catalog: what staqly can publish to and what each platform needs."""

    async def list(self, *, request_options: RequestOptions | None = None) -> builtins.list[Provider]:
        r = await self._http.request(RequestSpec("GET", "/v1/providers", options=_opts(request_options)))
        return _models(Provider, r)

    async def set_app(
        self,
        provider: str,
        *,
        client_id: str,
        client_secret: str,
        request_options: RequestOptions | None = None,
    ) -> None:
        """Registers the workspace's own developer app (OAuth client) for a platform."""
        await self._http.request(
            RequestSpec(
                "PUT",
                f"/v1/providers/{_q(provider)}/app",
                json={"client_id": client_id, "client_secret": client_secret},
                options=_opts(request_options),
            )
        )

    async def delete_app(self, provider: str, *, request_options: RequestOptions | None = None) -> None:
        """Removes the workspace's developer app; the server's ``PROVIDER_<ID>_CLIENT_ID`` applies
        again, if set."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/providers/{_q(provider)}/app", options=_opts(request_options))
        )

    async def check(self, provider: str, *, request_options: RequestOptions | None = None) -> ProviderCheck:
        """Checks this server's setup for an OAuth platform without any connected account: app keys,
        the return address to register, and whether the platform can reach media links. Needs
        ``channels:manage``."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/providers/{_q(provider)}/check", options=_opts(request_options))
        )
        return _model(ProviderCheck, r)


# ------------------------------------------------------------------ design editor


def _project_form(
    name: str | None, source_ids: Sequence[str] | None, output_id: str | None, page_ids: Sequence[str] | None
) -> dict[str, str]:
    return clean(
        {
            "name": name,
            "source_ids": None if source_ids is None else ",".join(source_ids),
            "output_id": output_id,
            "page_ids": None if page_ids is None else ",".join(page_ids),
        }
    )


class AsyncMediaProjects(_Resource):
    """The design editor's editable projects (a design as JSON, version 1)."""

    async def list(
        self, *, media: str | None = None, request_options: RequestOptions | None = None
    ) -> builtins.list[MediaProject]:
        """Projects, newest first; ``media`` keeps the ones made from that file or saved as it."""
        r = await self._http.request(
            RequestSpec("GET", "/v1/media-projects", params={"media": media}, options=_opts(request_options))
        )
        return _models(MediaProject, r)

    async def get(self, project_id: str, *, request_options: RequestOptions | None = None) -> MediaProject:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/media-projects/{_q(project_id)}", options=_opts(request_options))
        )
        return _model(MediaProject, r)

    async def create(
        self,
        file: FileInput,
        *,
        name: str | None = None,
        source_ids: Sequence[str] | None = None,
        output_id: str | None = None,
        page_ids: Sequence[str] | None = None,
        filename: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> MediaProject:
        """Saves an editable project: ``file`` is the editor's design; ``source_ids`` the library files it
        was made from, ``output_id`` and ``page_ids`` the files it saved as. Counts toward storage."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/media-projects",
                data=_project_form(name, source_ids, output_id, page_ids),
                files={"file": read_file(file, filename, "application/json")},
                options=_opts(request_options),
            )
        )
        return _model(MediaProject, r)

    async def update(
        self,
        project_id: str,
        file: FileInput,
        *,
        name: str | None = None,
        source_ids: Sequence[str] | None = None,
        output_id: str | None = None,
        page_ids: Sequence[str] | None = None,
        filename: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> MediaProject:
        """Saves a new version of a project."""
        r = await self._http.request(
            RequestSpec(
                "PUT",
                f"/v1/media-projects/{_q(project_id)}",
                data=_project_form(name, source_ids, output_id, page_ids),
                files={"file": read_file(file, filename, "application/json")},
                options=_opts(request_options),
            )
        )
        return _model(MediaProject, r)

    async def delete(self, project_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Deletes a project and its file; the files it was made from stay. Needs ``posts:delete``."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/media-projects/{_q(project_id)}", options=_opts(request_options))
        )

    async def file(self, project_id: str, *, request_options: RequestOptions | None = None) -> bytes:
        """Downloads a project's design (JSON)."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/media-projects/{_q(project_id)}/file", options=_opts(request_options))
        )
        return r.content


class AsyncAIJobs(_Resource):
    """AI requests (pictures and videos) on the server's own AI provider accounts."""

    async def create(
        self,
        operation: str,
        *,
        prompt: str | None = None,
        image: FileInput | None = None,
        mask: FileInput | None = None,
        width: int | None = None,
        height: int | None = None,
        duration: int | None = None,
        aspect_ratio: str | None = None,
        canvas_width: int | None = None,
        canvas_height: int | None = None,
        image_x: int | None = None,
        image_y: int | None = None,
        place_width: int | None = None,
        place_height: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> AIJob:
        """Queues one AI operation (:meth:`AI.status` lists them: generate, edit, inpaint, erase, expand,
        remove_background, replace_background, upscale, enhance, text_to_video, image_to_video); poll
        :meth:`get` until it has succeeded, failed or was canceled. ``image`` and ``mask`` are PNG, JPEG or
        WebP. A repeated ``request_options={"idempotency_key": ...}`` answers the earlier job."""
        fields = clean(
            {
                "operation": operation,
                "prompt": prompt,
                "width": width,
                "height": height,
                "duration": duration,
                "aspect_ratio": aspect_ratio,
                "canvas_width": canvas_width,
                "canvas_height": canvas_height,
                "image_x": image_x,
                "image_y": image_y,
                "place_width": place_width,
                "place_height": place_height,
            }
        )
        # Every part goes in ``files`` so the body is multipart even without a picture, settings first.
        parts: dict[str, tuple[str | None, bytes, str | None]] = {
            k: (None, str(v).encode(), None) for k, v in fields.items()
        }
        if image is not None:
            parts["image"] = read_file(image, None, None)
        if mask is not None:
            parts["mask"] = read_file(mask, None, "image/png")
        opts = _opts(request_options)
        if "idempotency_key" in opts:
            opts = _merged(RequestOptions(headers={"Idempotency-Key": opts["idempotency_key"]}), opts)
        r = await self._http.request(RequestSpec("POST", "/v1/ai/jobs", files=parts, options=opts))
        return _model(AIJob, r)

    async def get(self, job_id: str, *, request_options: RequestOptions | None = None) -> AIJob:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/ai/jobs/{_q(job_id)}", options=_opts(request_options))
        )
        return _model(AIJob, r)

    async def discard(self, job_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Cancels a running request or drops its result."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/ai/jobs/{_q(job_id)}", options=_opts(request_options))
        )

    async def result(self, job_id: str, *, request_options: RequestOptions | None = None) -> bytes:
        """The made picture or video (kept for a day, until it is accepted or discarded)."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/ai/jobs/{_q(job_id)}/result", options=_opts(request_options))
        )
        return r.content

    async def accept(self, job_id: str, *, request_options: RequestOptions | None = None) -> Media:
        """Keeps the result in the media library (accepting again answers the same file)."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/ai/jobs/{_q(job_id)}/accept", options=_opts(request_options))
        )
        return _model(Media, r)


class AsyncAI(_Resource):
    """The AI tools of the design editor: what this server offers, text rewrites and AI requests."""

    def __init__(self, client: AsyncStaqly) -> None:
        super().__init__(client)
        self.jobs = AsyncAIJobs(client)

    async def status(self, *, request_options: RequestOptions | None = None) -> AIStatus:
        """Which AI operations work on this server, and the workspace's AI usage this month."""
        r = await self._http.request(RequestSpec("GET", "/v1/ai", options=_opts(request_options)))
        return _model(AIStatus, r)

    async def text(
        self, text: str, *, instruction: str, request_options: RequestOptions | None = None
    ) -> str:
        """Rewrites a short text (at most 4000 characters) as ``instruction`` says ("Make it shorter",
        "Translate to German"). Counts toward AI usage."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/ai/text",
                json={"text": text, "instruction": instruction},
                options=_opts(request_options),
            )
        )
        out: str = r.json()["text"]
        return out


class AsyncDesign(_Resource):
    """The workspace's brand kit for the design editor."""

    async def brand(self, *, request_options: RequestOptions | None = None) -> DesignBrand:
        r = await self._http.request(RequestSpec("GET", "/v1/design/brand", options=_opts(request_options)))
        return _model(DesignBrand, r)

    async def set_brand(
        self,
        *,
        colors: Sequence[str],
        fonts: Sequence[str],
        logos: Sequence[str],
        request_options: RequestOptions | None = None,
    ) -> DesignBrand:
        """Replaces the brand kit: ``colors`` like ``#22c55e``, ``fonts`` from the editor's list,
        ``logos`` media ids."""
        body = {"colors": list(colors), "fonts": list(fonts), "logos": list(logos)}
        r = await self._http.request(
            RequestSpec("PUT", "/v1/design/brand", json=body, options=_opts(request_options))
        )
        return _model(DesignBrand, r)


class AsyncStock(_Resource):
    """Stock photos from Pixabay (when the server has a key)."""

    async def photos(
        self,
        *,
        query: str | None = None,
        page: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> StockPage:
        """30 photos a page (``page`` 1 to 16); ``configured`` is false without a Pixabay key."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                "/v1/stock/photos",
                params={"query": query, "page": page},
                options=_opts(request_options),
            )
        )
        return _model(StockPage, r)

    async def use(self, photo_id: str, *, request_options: RequestOptions | None = None) -> Media:
        """Copies a photo into the media library (Pixabay allows no hotlinking)."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/stock/photos/{_q(photo_id)}/use", options=_opts(request_options))
        )
        return _model(Media, r)


# ------------------------------------------------------------------ schedules


def _rule(rule: ScheduleRule | JSONMapping | None) -> dict[str, Any] | None:
    if rule is None:
        return None
    if isinstance(rule, ScheduleRule):
        return rule.model_dump(exclude_none=True)
    return dict(rule)


class AsyncSchedules(_Resource):
    """Publishing schedules: publish a collection's waiting drafts at a time (``kind="once"``,
    ``run_at``) or on a cadence (``kind="recurring"``, ``rule``). When nothing waits,
    ``source="agent"`` asks an agent (``schedule.posts_requested``, :meth:`AsyncWork.list`) and
    ``source="endpoint"`` fetches posts from ``endpoint_url``. Changing needs ``schedules:manage``."""

    async def list(
        self, *, collection_id: str | None = None, request_options: RequestOptions | None = None
    ) -> builtins.list[Schedule]:
        """Lists schedules, optionally of one collection (id or exact name)."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                "/v1/schedules",
                params={"collection_id": collection_id},
                options=_opts(request_options),
            )
        )
        return _models(Schedule, r)

    async def get(self, schedule_id: str, *, request_options: RequestOptions | None = None) -> Schedule:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/schedules/{_q(schedule_id)}", options=_opts(request_options))
        )
        return _model(Schedule, r)

    async def create(
        self,
        collection: str,
        *,
        name: str,
        kind: str | None = None,
        run_at: TimeFilter | None = None,
        rule: ScheduleRule | JSONMapping | None = None,
        channels: Sequence[str] | None = None,
        pick: str | None = None,
        post_ids: Sequence[str] | None = None,
        source: str | None = None,
        endpoint_url: str | None = None,
        endpoint_secret: str | None = None,
        enabled: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> Schedule:
        """Creates a schedule for a collection (id or exact name). ``kind`` is ``once`` (with
        ``run_at``) or ``recurring`` (with ``rule``: ``times`` on ``weekdays`` in ``tz``, or
        ``every_minutes``); ``pick`` is ``next`` (the oldest waiting draft), ``all`` or ``selected``
        (``post_ids``); ``channels`` are added to posts without their own targets."""
        body = clean(
            {
                "collection": collection,
                "name": name,
                "kind": kind,
                "run_at": _when(run_at),
                "rule": _rule(rule),
                "channels": list(channels) if channels is not None else None,
                "pick": pick,
                "post_ids": list(post_ids) if post_ids is not None else None,
                "source": source,
                "endpoint_url": endpoint_url,
                "endpoint_secret": endpoint_secret,
                "enabled": enabled,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/schedules", json=body, options=_opts(request_options))
        )
        return _model(Schedule, r)

    async def update(
        self,
        schedule_id: str,
        *,
        name: str | None = None,
        kind: str | None = None,
        run_at: TimeFilter | None = None,
        rule: ScheduleRule | JSONMapping | None = None,
        channels: Sequence[str] | None = None,
        pick: str | None = None,
        post_ids: Sequence[str] | None = None,
        source: str | None = None,
        endpoint_url: str | None = None,
        endpoint_secret: str | None = None,
        enabled: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> Schedule:
        """Changes a schedule; the next run is recomputed."""
        body = clean(
            {
                "name": name,
                "kind": kind,
                "run_at": _when(run_at),
                "rule": _rule(rule),
                "channels": list(channels) if channels is not None else None,
                "pick": pick,
                "post_ids": list(post_ids) if post_ids is not None else None,
                "source": source,
                "endpoint_url": endpoint_url,
                "endpoint_secret": endpoint_secret,
                "enabled": enabled,
            }
        )
        r = await self._http.request(
            RequestSpec(
                "PATCH", f"/v1/schedules/{_q(schedule_id)}", json=body, options=_opts(request_options)
            )
        )
        return _model(Schedule, r)

    async def enable(self, schedule_id: str, *, request_options: RequestOptions | None = None) -> Schedule:
        return await self.update(schedule_id, enabled=True, request_options=request_options)

    async def disable(self, schedule_id: str, *, request_options: RequestOptions | None = None) -> Schedule:
        return await self.update(schedule_id, enabled=False, request_options=request_options)

    async def delete(self, schedule_id: str, *, request_options: RequestOptions | None = None) -> None:
        await self._http.request(
            RequestSpec("DELETE", f"/v1/schedules/{_q(schedule_id)}", options=_opts(request_options))
        )

    async def run(self, schedule_id: str, *, request_options: RequestOptions | None = None) -> Schedule:
        """Runs the schedule now (a ``run_schedule`` job); its regular next run is unchanged."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/schedules/{_q(schedule_id)}/run", options=_opts(request_options))
        )
        return _model(Schedule, r)


# ----------------------------------------------------------------------- jobs


class AsyncJobs(_Resource):
    """The background queue: publishing, schedule runs and sign-in refreshes, with retries."""

    async def list(
        self,
        *,
        status: str | None = None,
        kind: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> JobPage:
        """One page of jobs, newest first. ``status``: queued, running, done, failed, cancelled;
        ``kind``: publish_post, publish_target, target_timeout, run_schedule, refresh_channel."""
        params = {"status": status, "kind": kind, "limit": limit, "cursor": cursor}
        r = await self._http.request(
            RequestSpec("GET", "/v1/jobs", params=params, options=_opts(request_options))
        )
        return _model(JobPage, r)

    async def iterate(
        self,
        *,
        status: str | None = None,
        kind: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> AsyncIterator[Job]:
        """Yields every matching job across pages."""
        while True:
            page = await self.list(
                status=status, kind=kind, limit=limit, cursor=cursor, request_options=request_options
            )
            for job in page.data:
                yield job
            if not page.next_cursor:
                return
            cursor = page.next_cursor

    async def stats(self, *, request_options: RequestOptions | None = None) -> JobStats:
        r = await self._http.request(RequestSpec("GET", "/v1/jobs/stats", options=_opts(request_options)))
        return _model(JobStats, r)

    async def get(self, job_id: str, *, request_options: RequestOptions | None = None) -> Job:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/jobs/{_q(job_id)}", options=_opts(request_options))
        )
        return _model(Job, r)

    async def retry(self, job_id: str, *, request_options: RequestOptions | None = None) -> Job:
        """Queues a failed job again, now (needs ``posts:publish``)."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/jobs/{_q(job_id)}/retry", options=_opts(request_options))
        )
        return _model(Job, r)

    async def cancel(self, job_id: str, *, request_options: RequestOptions | None = None) -> Job:
        """Cancels a queued job."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/jobs/{_q(job_id)}/cancel", options=_opts(request_options))
        )
        return _model(Job, r)


class AsyncWork(_Resource):
    """What the workspace asks your automation for: targets on your own channels (provider
    ``custom``) that wait for :meth:`AsyncPosts.report_result`, and schedules of ``source="agent"``
    whose last run found nothing to publish and want new posts."""

    async def list(self, *, request_options: RequestOptions | None = None) -> builtins.list[WorkItem]:
        r = await self._http.request(RequestSpec("GET", "/v1/work", options=_opts(request_options)))
        return _models(WorkItem, r)


# ---------------------------------------------------------- workspace & keys


class AsyncWorkspaceResource(_Resource):
    """The workspace the client is authenticated to."""

    async def get(self, *, request_options: RequestOptions | None = None) -> Workspace:
        r = await self._http.request(RequestSpec("GET", "/v1/workspace", options=_opts(request_options)))
        return _model(Workspace, r)

    async def agents(self, *, request_options: RequestOptions | None = None) -> builtins.list[Agent]:
        """Agents (MCP clients) and API connections seen in this workspace, latest first (admin)."""
        r = await self._http.request(RequestSpec("GET", "/v1/agents", options=_opts(request_options)))
        return _models(Agent, r)

    async def forget_agent(self, agent_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Removes an agent from the list; it comes back if it talks to the workspace again (admin)."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/agents/{_q(agent_id)}", options=_opts(request_options))
        )

    async def rename(self, name: str, *, request_options: RequestOptions | None = None) -> Workspace:
        """Renames the workspace (needs the ``admin`` scope)."""
        r = await self._http.request(
            RequestSpec("PATCH", "/v1/workspace", json={"name": name}, options=_opts(request_options))
        )
        return _model(Workspace, r)

    async def update(
        self,
        *,
        name: str | None = None,
        timezone: str | None = None,
        logo: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Workspace:
        """Renames the workspace, sets its time zone (an IANA name) and/or its logo.

        The logo is a PNG or JPEG ``data:`` URL of at most 64 KB (``""`` for staqly's).
        Needs the ``admin`` scope.
        """
        fields = {"name": name, "timezone": timezone, "logo": logo}
        body = {k: v for k, v in fields.items() if v is not None}
        r = await self._http.request(
            RequestSpec("PATCH", "/v1/workspace", json=body, options=_opts(request_options))
        )
        return _model(Workspace, r)

    async def usage(self, *, request_options: RequestOptions | None = None) -> WorkspaceUsage:
        """Current plan, period and usage (local installs report the ``unlocked`` plan)."""
        r = await self._http.request(
            RequestSpec("GET", "/v1/workspace/usage", options=_opts(request_options))
        )
        return _model(WorkspaceUsage, r)

    async def members(self, *, request_options: RequestOptions | None = None) -> builtins.list[Member]:
        """People in the workspace and their roles (cloud accounts; needs ``admin``). Inviting and
        changing roles is done by a person in the dashboard, never with an API key."""
        r = await self._http.request(RequestSpec("GET", "/v1/members", options=_opts(request_options)))
        return _models(Member, r)

    async def delete(self, *, confirm: str, request_options: RequestOptions | None = None) -> None:
        """Permanently deletes the workspace and all its data (cloud, dashboard session only;
        API keys get 403, local mode 409). ``confirm`` must equal the workspace name."""
        await self._http.request(
            RequestSpec("DELETE", "/v1/workspace", json={"confirm": confirm}, options=_opts(request_options))
        )

    async def access_token(self, *, request_options: RequestOptions | None = None) -> AccessToken:
        """Local mode: the access token agents, SDKs and the CLI send as ``Authorization: Bearer``
        (cloud mode: :class:`~staqly.NotFoundError`)."""
        r = await self._http.request(RequestSpec("GET", "/v1/access-token", options=_opts(request_options)))
        return _model(AccessToken, r)

    async def rotate_access_token(
        self, *, request_options: RequestOptions | None = None
    ) -> AccessTokenRotated:
        """Local mode: makes a new access token. The old one stops working at once (this client
        included, unless it switches to the new token) and every other browser is signed out."""
        r = await self._http.request(
            RequestSpec("POST", "/v1/access-token/rotate", options=_opts(request_options))
        )
        return _model(AccessTokenRotated, r)

    async def rotate_recovery_key(self, *, request_options: RequestOptions | None = None) -> RecoveryKey:
        """Issues a new recovery key, shown once (dashboard sessions only). Every other
        session of the workspace is signed out."""
        r = await self._http.request(
            RequestSpec("POST", "/v1/recovery-key/rotate", options=_opts(request_options))
        )
        return _model(RecoveryKey, r)


class AsyncSessions(_Resource):
    """Browser (dashboard) sessions. Listing and revoking need ``admin``."""

    async def current(self, *, request_options: RequestOptions | None = None) -> Whoami:
        """Server mode, workspace, the calling principal and the CSRF token (same as ``whoami()``)."""
        r = await self._http.request(RequestSpec("GET", "/v1/session", options=_opts(request_options)))
        return _model(Whoami, r)

    async def list(self, *, request_options: RequestOptions | None = None) -> builtins.list[Session]:
        r = await self._http.request(RequestSpec("GET", "/v1/sessions", options=_opts(request_options)))
        return _models(Session, r)

    async def revoke(self, session_id: str, *, request_options: RequestOptions | None = None) -> None:
        await self._http.request(
            RequestSpec("DELETE", f"/v1/sessions/{_q(session_id)}", options=_opts(request_options))
        )

    async def logout(self, *, request_options: RequestOptions | None = None) -> None:
        """Revokes the current session (``DELETE /v1/session``)."""
        await self._http.request(RequestSpec("DELETE", "/v1/session", options=_opts(request_options)))

    async def sign_in_with_api_key(
        self, api_key: str, *, request_options: RequestOptions | None = None
    ) -> SessionCreated:
        """Exchanges an ``admin`` API key (cloud: the key sign-up or ``recover()`` returns; local
        mode: the access token) for a session cookie (kept in the client's cookie jar). Sent
        without credentials (the key goes in the body); other sessions stay signed in. Keys
        without ``admin`` raise :class:`~staqly.PermissionDeniedError` (403), unknown, revoked or
        expired keys :class:`~staqly.AuthenticationError` (401)."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/sessions/api-key",
                json={"api_key": api_key},
                no_auth=True,
                options=_opts(request_options),
            )
        )
        return _model(SessionCreated, r)

    async def recover(
        self, recovery_key: str, *, request_options: RequestOptions | None = None
    ) -> SessionRecovered:
        """Exchanges the recovery key (single use) for a new session cookie (kept in the client's
        cookie jar), a new admin API key and the next recovery key. The old recovery key stops
        working and every other session of the workspace is signed out; existing API keys keep
        working, except on a plan with an API key limit (Starter, Plus), where the new admin key
        replaces the old admin keys. Sent without
        credentials. Store ``api_key.key`` and ``recovery_key`` now: they
        are shown only once."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/sessions/recover",
                json={"recovery_key": recovery_key},
                no_auth=True,
                options=_opts(request_options),
            )
        )
        return _model(SessionRecovered, r)


class AsyncDeviceLinks(_Resource):
    """One-time "connect a device" links (QR codes) for signing in another browser."""

    async def create(self, *, request_options: RequestOptions | None = None) -> DeviceLink:
        """Creates a link (dashboard session or local principal only; API keys get 403).
        The token is in the URL fragment, valid for 5 minutes, single use."""
        r = await self._http.request(RequestSpec("POST", "/v1/device-links", options=_opts(request_options)))
        return _model(DeviceLink, r)

    async def redeem(self, token: str, *, request_options: RequestOptions | None = None) -> SessionCreated:
        """Exchanges a device-link token for a session cookie. Sent without credentials;
        a used or expired token raises :class:`~staqly.GoneError` (410)."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/device-links/redeem",
                json={"token": token},
                no_auth=True,
                options=_opts(request_options),
            )
        )
        return _model(SessionCreated, r)


class AsyncBillingResource(_Resource):
    """Pro subscription through Stripe (cloud mode; needs ``admin``)."""

    async def get(self, *, request_options: RequestOptions | None = None) -> Billing:
        """Whether billing is enabled, the plan, Stripe's subscription status and the period end."""
        r = await self._http.request(RequestSpec("GET", "/v1/billing", options=_opts(request_options)))
        return _model(Billing, r)

    async def details(
        self, *, starting_after: str | None = None, request_options: RequestOptions | None = None
    ) -> BillingDetails:
        """The subscription period, cards on file (brand, last four digits) and the newest invoices, 24 per
        page, read from Stripe. ``starting_after``: the last invoice id of the previous page."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                "/v1/billing/details",
                params={"starting_after": starting_after},
                options=_opts(request_options),
            )
        )
        return _model(BillingDetails, r)

    async def checkout(
        self,
        *,
        plan: str | None = None,
        interval: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> BillingURL:
        """Pays for a plan (``starter``, ``plus``, ``pro``, ``business``; default ``pro``) billed
        monthly or yearly (``interval`` ``"month"`` or ``"year"``, 20 % off or more; default
        ``"month"``): open ``url`` in a browser to pay. A workspace that already pays is switched to
        that plan instead (prorated) and gets the billing page URL. The plan changes when Stripe
        confirms. Raises :class:`~staqly.ConflictError` when billing is off, the price is not set
        up, or it is the plan and interval it already has."""
        body = {k: v for k, v in {"plan": plan, "interval": interval}.items() if v is not None}
        r = await self._http.request(
            RequestSpec("POST", "/v1/billing/checkout", json=body, options=_opts(request_options))
        )
        return _model(BillingURL, r)

    async def x_posts(self, *, request_options: RequestOptions | None = None) -> XPosts:
        """Posts to X this period: included, bought and what's left (``posts:read``)."""
        r = await self._http.request(
            RequestSpec("GET", "/v1/billing/x-posts", options=_opts(request_options))
        )
        return _model(XPosts, r)

    async def credits_checkout(
        self, posts: int, *, provider: str = "x", request_options: RequestOptions | None = None
    ) -> BillingURL:
        """Buys a bundle of extra posts to X (``200``, ``1000`` or ``4000``): open ``url`` in a browser to
        pay. The posts are added when Stripe confirms and expire at the end of the billing period."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/billing/credits/checkout",
                json={"provider": provider, "posts": posts},
                options=_opts(request_options),
            )
        )
        return _model(BillingURL, r)

    async def portal(self, *, request_options: RequestOptions | None = None) -> BillingURL:
        """Stripe customer portal URL (payment method, invoices, cancel). 409 before the first checkout."""
        r = await self._http.request(
            RequestSpec("POST", "/v1/billing/portal", options=_opts(request_options))
        )
        return _model(BillingURL, r)


class AsyncWorkspaces(_Resource):
    """Cloud signup."""

    async def create(
        self,
        *,
        name: str | None = None,
        setup_token: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> WorkspaceCreated:
        """Creates a cloud workspace (on a 7-day Pro trial). Returns its first API key
        (``api_key.key``) and recovery key — shown only here. Sent without credentials;
        rate limited to 5 per hour per IP."""
        r = await self._http.request(
            RequestSpec(
                "POST",
                "/v1/workspaces",
                json=clean({"name": name, "setup_token": setup_token}),
                no_auth=True,
                options=_opts(request_options),
            )
        )
        return _model(WorkspaceCreated, r)


class AsyncPlans(_Resource):
    async def list(self, *, request_options: RequestOptions | None = None) -> builtins.list[Plan]:
        """Cloud plans and their limits (``-1`` = unlimited)."""
        r = await self._http.request(RequestSpec("GET", "/v1/plans", options=_opts(request_options)))
        return _models(Plan, r)


class AsyncAPIKeys(_Resource):
    """Scoped API keys (cloud; needs the ``admin`` scope)."""

    async def list(self, *, request_options: RequestOptions | None = None) -> builtins.list[APIKey]:
        r = await self._http.request(RequestSpec("GET", "/v1/api-keys", options=_opts(request_options)))
        return _models(APIKey, r)

    async def create(
        self,
        name: str,
        *,
        scopes: Sequence[str],
        expires_at: str | datetime | date | None = None,
        request_options: RequestOptions | None = None,
    ) -> APIKey:
        """Creates a key; the raw secret is in ``.key`` and is returned only once."""
        body = clean(
            {
                "name": name,
                "scopes": list(scopes),
                "expires_at": expires_at.isoformat()
                if isinstance(expires_at, (datetime, date))
                else expires_at,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/api-keys", json=body, options=_opts(request_options))
        )
        return _model(APIKey, r)

    async def revoke(self, key_id: str, *, request_options: RequestOptions | None = None) -> None:
        await self._http.request(
            RequestSpec("DELETE", f"/v1/api-keys/{_q(key_id)}", options=_opts(request_options))
        )

    async def rotate(self, key_id: str, *, request_options: RequestOptions | None = None) -> APIKey:
        """Revokes a key and issues a replacement with the same name and scopes."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/api-keys/{_q(key_id)}/rotate", options=_opts(request_options))
        )
        return _model(APIKey, r)


# --------------------------------------------------------------------- alerts


class AsyncAlerts(_Resource):
    """Alerts push a message to Telegram, Slack, Discord, ntfy or email when a post is published or
    fails, or a channel needs reconnecting (needs ``alerts:manage``).

    Secrets in ``config`` are write-only and come back masked.
    """

    async def list(self, *, request_options: RequestOptions | None = None) -> builtins.list[Alert]:
        r = await self._http.request(RequestSpec("GET", "/v1/alerts", options=_opts(request_options)))
        return _models(Alert, r)

    async def get(self, alert_id: str, *, request_options: RequestOptions | None = None) -> Alert:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/alerts/{_q(alert_id)}", options=_opts(request_options))
        )
        return _model(Alert, r)

    async def create(
        self,
        name: str,
        *,
        type: str,
        config: JSONMapping | None = None,
        events: Sequence[str] | None = None,
        collection_ids: Sequence[str] | None = None,
        digest_window_seconds: int | None = None,
        enabled: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> Alert:
        """Creates an alert channel. ``type``: telegram, slack, discord, ntfy or email; ``config`` is
        type-specific (e.g. ntfy: ``{"topic": ...}``). Plan limit ``alerts`` (402)."""
        body = clean(
            {
                "name": name,
                "type": type,
                "config": dict(config) if config is not None else None,
                "events": list(events) if events is not None else None,
                "collection_ids": list(collection_ids) if collection_ids is not None else None,
                "digest_window_seconds": digest_window_seconds,
                "enabled": enabled,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/alerts", json=body, options=_opts(request_options))
        )
        return _model(Alert, r)

    async def update(
        self,
        alert_id: str,
        *,
        name: str | None = None,
        config: JSONMapping | None = None,
        events: Sequence[str] | None = None,
        collection_ids: Sequence[str] | None = None,
        digest_window_seconds: int | None = None,
        enabled: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> Alert:
        """Updates an alert. ``config`` keys are merged (``None`` values remove a key, a masked
        value keeps the stored secret); ``enabled=True`` resets the failure counter."""
        body = clean(
            {
                "name": name,
                "config": dict(config) if config is not None else None,
                "events": list(events) if events is not None else None,
                "collection_ids": list(collection_ids) if collection_ids is not None else None,
                "digest_window_seconds": digest_window_seconds,
                "enabled": enabled,
            }
        )
        r = await self._http.request(
            RequestSpec(
                "PATCH",
                f"/v1/alerts/{_q(alert_id)}",
                json=body,
                options=_opts(request_options),
            )
        )
        return _model(Alert, r)

    async def enable(self, alert_id: str, *, request_options: RequestOptions | None = None) -> Alert:
        return await self.update(alert_id, enabled=True, request_options=request_options)

    async def disable(self, alert_id: str, *, request_options: RequestOptions | None = None) -> Alert:
        return await self.update(alert_id, enabled=False, request_options=request_options)

    async def delete(self, alert_id: str, *, request_options: RequestOptions | None = None) -> None:
        await self._http.request(
            RequestSpec("DELETE", f"/v1/alerts/{_q(alert_id)}", options=_opts(request_options))
        )

    async def test(self, alert_id: str, *, request_options: RequestOptions | None = None) -> AlertTestResult:
        """Sends a test message now; delivery problems are reported in ``ok``/``error``, not raised."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/alerts/{_q(alert_id)}/test", options=_opts(request_options))
        )
        return _model(AlertTestResult, r)

    async def deliveries(
        self, alert_id: str, *, limit: int | None = None, request_options: RequestOptions | None = None
    ) -> builtins.list[AlertDelivery]:
        """Recent outbox rows of an alert channel."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                f"/v1/alerts/{_q(alert_id)}/deliveries",
                params={"limit": limit},
                options=_opts(request_options),
            )
        )
        return _models(AlertDelivery, r)


# -------------------------------------------------------------------- exports


def _export_params(
    *,
    from_: TimeFilter | None,
    to: TimeFilter | None,
    type: str | None,
    type_prefix: str | None,
    actor_type: str | None,
    actor_id: str | None,
    collection_id: str | None,
    post_id: str | None,
    format: str,
) -> dict[str, Any]:
    return {
        "format": format,
        "from": from_,
        "to": to,
        "type": type,
        "type_prefix": type_prefix,
        "actor_type": actor_type,
        "actor_id": actor_id,
        "collection_id": collection_id,
        "post_id": post_id,
    }


class AsyncExports(_Resource):
    """Data portability: stream the activity log, or export the whole workspace as a ZIP."""

    async def events(
        self,
        *,
        from_: TimeFilter | None = None,
        to: TimeFilter | None = None,
        type: str | None = None,
        type_prefix: str | None = None,
        actor_type: str | None = None,
        actor_id: str | None = None,
        collection_id: str | None = None,
        post_id: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> AsyncIterator[Event]:
        """Streams every matching event, oldest first (NDJSON; nothing is buffered).

        ``from_`` / ``to`` bound ``created_at`` (RFC3339, date or datetime; ``to`` is exclusive).
        """
        params = _export_params(
            from_=from_,
            to=to,
            type=type,
            type_prefix=type_prefix,
            actor_type=actor_type,
            actor_id=actor_id,
            collection_id=collection_id,
            post_id=post_id,
            format="ndjson",
        )
        spec = RequestSpec("GET", "/v1/events/export", params=params, options=_opts(request_options))
        async with self._http.stream(spec) as r:
            async for line in r.aiter_lines():
                if line.strip():
                    yield Event.model_validate_json(line)

    async def save_events(
        self,
        path: str | PathLike[str],
        *,
        format: str = "ndjson",
        from_: TimeFilter | None = None,
        to: TimeFilter | None = None,
        type: str | None = None,
        type_prefix: str | None = None,
        actor_type: str | None = None,
        actor_id: str | None = None,
        collection_id: str | None = None,
        post_id: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Path:
        """Streams the event export (``format="ndjson"`` or ``"csv"``) to a file and returns its path."""
        params = _export_params(
            from_=from_,
            to=to,
            type=type,
            type_prefix=type_prefix,
            actor_type=actor_type,
            actor_id=actor_id,
            collection_id=collection_id,
            post_id=post_id,
            format=format,
        )
        spec = RequestSpec("GET", "/v1/events/export", params=params, options=_opts(request_options))
        return await self._save(spec, path)

    async def create_workspace_export(
        self, *, request_options: RequestOptions | None = None
    ) -> WorkspaceExport:
        """Starts a workspace export (admin); data only on Starter (``includes_media`` False).
        Only one may be pending/running (409 with ``details["export_id"]``);
        archives are deleted after 24 h."""
        r = await self._http.request(
            RequestSpec("POST", "/v1/workspace/export", options=_opts(request_options))
        )
        return _model(WorkspaceExport, r)

    async def list_workspace_exports(
        self, *, request_options: RequestOptions | None = None
    ) -> builtins.list[WorkspaceExport]:
        """The workspace's latest exports (backups), newest first, at most 20 (admin)."""
        r = await self._http.request(
            RequestSpec("GET", "/v1/workspace/exports", options=_opts(request_options))
        )
        return _models(WorkspaceExport, r)

    async def get_workspace_export(
        self, export_id: str, *, request_options: RequestOptions | None = None
    ) -> WorkspaceExport:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/workspace/exports/{_q(export_id)}", options=_opts(request_options))
        )
        return _model(WorkspaceExport, r)

    async def wait_for_workspace_export(
        self,
        export_id: str,
        *,
        timeout: float = DEFAULT_WAIT_TIMEOUT,
        poll_interval: float = 1.0,
        request_options: RequestOptions | None = None,
    ) -> WorkspaceExport:
        """Polls until the export is ``ready``. Raises :class:`~staqly.ExportFailedError` if it
        fails or expires and :class:`~staqly.WaitTimeoutError` after ``timeout`` seconds."""
        started = time.monotonic()
        while True:
            job = await self.get_workspace_export(export_id, request_options=request_options)
            if job.status == "ready":
                return job
            if job.status in ("failed", "expired"):
                raise ExportFailedError(job)
            remaining = started + timeout - time.monotonic()
            if remaining <= 0:
                raise WaitTimeoutError(export_id, time.monotonic() - started, None)
            await self._http._sleep(min(poll_interval, remaining))

    async def download_workspace_export(
        self, export_id: str, *, request_options: RequestOptions | None = None
    ) -> bytes:
        """Downloads a finished archive into memory (see :meth:`save_workspace_export`)."""
        r = await self._http.request(
            RequestSpec(
                "GET", f"/v1/workspace/exports/{_q(export_id)}/download", options=_opts(request_options)
            )
        )
        return r.content

    async def save_workspace_export(
        self, export_id: str, path: str | PathLike[str], *, request_options: RequestOptions | None = None
    ) -> Path:
        """Streams a finished archive to ``path`` (no buffering) and returns the path."""
        spec = RequestSpec(
            "GET", f"/v1/workspace/exports/{_q(export_id)}/download", options=_opts(request_options)
        )
        return await self._save(spec, path)

    async def export_workspace(
        self,
        path: str | PathLike[str] | None = None,
        *,
        timeout: float = DEFAULT_WAIT_TIMEOUT,
        poll_interval: float = 1.0,
        request_options: RequestOptions | None = None,
    ) -> WorkspaceExport:
        """Starts an export, waits until it is ready and, when ``path`` is given, saves the ZIP there."""
        job = await self.create_workspace_export(request_options=request_options)
        job = await self.wait_for_workspace_export(
            job.id, timeout=timeout, poll_interval=poll_interval, request_options=request_options
        )
        if path is not None:
            await self.save_workspace_export(job.id, path, request_options=request_options)
        return job

    async def _save(self, spec: RequestSpec, path: str | PathLike[str]) -> Path:
        target = Path(path)
        out = open_for_write(target)
        try:
            async with self._http.stream(spec) as r:
                async for chunk in r.aiter_bytes():
                    out.write(chunk)
        finally:
            out.close()
        return target


# --------------------------------------------------------------------- client


class AsyncStaqly:
    """staqly API client.

    ::

        staqly = AsyncStaqly()  # local mode: the access token from STAQLY_API_KEY
        cloud = AsyncStaqly(base_url="https://staqly.example.com", api_key="stk_...")

    Configuration falls back to ``STAQLY_API_URL`` and ``STAQLY_API_KEY``. Pass
    ``api_key=""`` to ignore the environment variable.
    """

    def __init__(
        self,
        *,
        base_url: str | None = None,
        api_key: str | None = None,
        timeout: float | httpx.Timeout | None = None,
        max_retries: int | None = None,
        default_headers: Mapping[str, str] | None = None,
        user_agent: str | None = None,
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """
        :param base_url: API URL (default ``STAQLY_API_URL`` or ``http://localhost:3333``).
        :param api_key: ``stk_...`` API key, or a local server's access token ``stk_local_...``
            (``staqly serve token``); default ``STAQLY_API_KEY``.
        :param timeout: per-request timeout in seconds (default 60).
        :param max_retries: retries for 408/429/5xx/network errors (default 2).
        :param default_headers: headers sent with every request.
        :param user_agent: appended to the ``User-Agent``.
        :param http_client: your own ``httpx`` client (proxies, transports, tests).
        """
        self._http = AsyncHTTPClient(
            base_url=base_url,
            api_key=api_key,
            timeout=timeout,
            max_retries=max_retries,
            default_headers=default_headers,
            user_agent=user_agent,
            http_client=http_client,
        )
        self.collections = AsyncCollections(self)
        self.posts = AsyncPosts(self)
        self.channels = AsyncChannels(self)
        self.providers = AsyncProviders(self)
        self.schedules = AsyncSchedules(self)
        self.jobs = AsyncJobs(self)
        self.work = AsyncWork(self)
        self.media = AsyncMediaResource(self)
        self.media_projects = AsyncMediaProjects(self)
        self.ai = AsyncAI(self)
        self.design = AsyncDesign(self)
        self.stock = AsyncStock(self)
        self.events = AsyncEvents(self)
        self.webhooks = AsyncWebhooks(self)
        self.alerts = AsyncAlerts(self)
        self.analytics = AsyncAnalyticsResource(self)
        self.workspace = AsyncWorkspaceResource(self)
        self.workspaces = AsyncWorkspaces(self)
        self.plans = AsyncPlans(self)
        self.api_keys = AsyncAPIKeys(self)
        self.exports = AsyncExports(self)
        self.billing = AsyncBillingResource(self)
        self.sessions = AsyncSessions(self)
        self.device_links = AsyncDeviceLinks(self)

    @property
    def base_url(self) -> str:
        return self._http.base_url

    @property
    def api_key(self) -> str | None:
        """The configured API key (read-only; never shown by ``repr()``)."""
        return self._http.api_key

    def __repr__(self) -> str:
        key = "'***'" if self._http.api_key else "None"
        return f"{type(self).__name__}(base_url={self.base_url!r}, api_key={key})"

    async def __aenter__(self) -> AsyncStaqly:
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.aclose()

    async def aclose(self) -> None:
        """Closes the underlying HTTP client (unless you passed your own)."""
        await self._http.aclose()

    async def request(
        self,
        method: str,
        path: str,
        *,
        params: Mapping[str, Any] | None = None,
        json: Any = None,
        request_options: RequestOptions | None = None,
    ) -> Any:
        """Low-level escape hatch: calls any endpoint with auth, retries and error
        mapping, returning the decoded JSON (``None`` for 204)."""
        r = await self._http.request(
            RequestSpec(method.upper(), path, params=params, json=json, options=_opts(request_options))
        )
        return r.json() if r.content else None

    async def health(self, *, request_options: RequestOptions | None = None) -> Health:
        """Liveness and database check (no auth)."""
        r = await self._http.request(
            RequestSpec("GET", "/health", no_auth=True, options=_opts(request_options))
        )
        return _model(Health, r)

    async def ready(self, *, request_options: RequestOptions | None = None) -> Readiness:
        """Readiness probe (database, migrations, workers). A not-ready server raises
        :class:`~staqly.InternalServerError` (503) whose ``body`` holds the same report. Not retried."""
        opts = RequestOptions(max_retries=0)
        opts.update(_opts(request_options))
        r = await self._http.request(RequestSpec("GET", "/readyz", no_auth=True, options=opts))
        return _model(Readiness, r)

    async def live(self, *, request_options: RequestOptions | None = None) -> Liveness:
        """Liveness probe: answers while the process serves HTTP (no auth, no database)."""
        r = await self._http.request(
            RequestSpec("GET", "/livez", no_auth=True, options=_opts(request_options))
        )
        return _model(Liveness, r)

    async def setup(self, *, request_options: RequestOptions | None = None) -> SetupStatus:
        """Server mode, whether the workspace exists (local) and whether signup is open (cloud). No auth."""
        r = await self._http.request(
            RequestSpec("GET", "/v1/setup", no_auth=True, options=_opts(request_options))
        )
        return _model(SetupStatus, r)

    async def metrics(
        self, *, token: str | None = None, request_options: RequestOptions | None = None
    ) -> str:
        """Prometheus metrics (text format). Allowed from this machine, or anywhere with
        ``token`` (the server's ``METRICS_TOKEN``); otherwise the server answers 404."""
        headers = {"Accept": "text/plain"}
        if token:
            headers["Authorization"] = f"Bearer {token}"
        opts = _merged(RequestOptions(headers=headers), request_options)
        r = await self._http.request(RequestSpec("GET", "/metrics", no_auth=True, options=opts))
        return r.text

    async def openapi(self, *, request_options: RequestOptions | None = None) -> str:
        """The server's OpenAPI document (YAML text)."""
        opts = _merged(RequestOptions(headers={"Accept": "application/yaml"}), request_options)
        r = await self._http.request(RequestSpec("GET", "/openapi.yaml", no_auth=True, options=opts))
        return r.text

    async def whoami(self, *, request_options: RequestOptions | None = None) -> Whoami:
        """Server mode, workspace and the calling principal with its scopes."""
        r = await self._http.request(RequestSpec("GET", "/v1/session", options=_opts(request_options)))
        return _model(Whoami, r)
