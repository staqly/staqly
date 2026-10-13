"""Async client and resources — the source of truth.

``_client_sync.py`` is GENERATED from this file by ``scripts/gen_sync.py``
(``make gen``): ``async``/``await`` are stripped and ``Async*`` names lose
their prefix, so both clients always expose the same surface. Lines ending in
``# async-only`` are dropped from the sync version.
"""

from __future__ import annotations

import builtins
import collections.abc as cabc  # async-only
import contextlib
import functools
import json
import time
from collections.abc import AsyncIterator, Callable, Mapping, Sequence
from datetime import date, datetime
from os import PathLike
from pathlib import Path
from types import TracebackType
from typing import Any, ParamSpec, TypeVar

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
from .errors import ExportFailedError, RejectedError, WaitTimeoutError
from .models import (
    ActionDef,
    APIKey,
    BulkActionResult,
    Collection,
    Decision,
    Delivery,
    Event,
    EventPage,
    Health,
    Item,
    ItemPage,
    ItemVersion,
    ItemWaitResult,
    Media,
    NotificationChannel,
    NotificationDelivery,
    NotificationTestResult,
    Plan,
    PurgeResult,
    Readiness,
    Rule,
    RuleMatch,
    RuleParams,
    StaqlyModel,
    Watcher,
    WatcherPreset,
    WatcherRunResult,
    Webhook,
    Whoami,
    Workspace,
    WorkspaceCreated,
    WorkspaceExport,
    WorkspaceUsage,
)

__all__ = ["AsyncStaqly"]

P = ParamSpec("P")
R = TypeVar("R")
M = TypeVar("M", bound=StaqlyModel)

#: Default ``wait_for_decision`` timeout in seconds.
DEFAULT_WAIT_TIMEOUT = 600.0
#: Longest single long-poll the server accepts (seconds).
MAX_LONG_POLL = 60

TimeFilter = str | datetime | date
JSONMapping = Mapping[str, Any]


def _q(value: str) -> str:
    from urllib.parse import quote

    return quote(value, safe="")


def _opts(request_options: RequestOptions | None) -> RequestOptions:
    return request_options if request_options is not None else RequestOptions()


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


def _item_fields(
    *,
    title: str | None = None,
    content: Any = None,
    url: str | None = None,
    media_id: str | None = None,
    metadata: JSONMapping | None = None,
    metadata_patch: JSONMapping | None = None,
) -> dict[str, Any]:
    return clean(
        {
            "title": title,
            "content": content,
            "url": url,
            "media_id": media_id,
            "metadata": dict(metadata) if metadata is not None else None,
            "metadata_patch": dict(metadata_patch) if metadata_patch is not None else None,
        }
    )


def _jsonable(value: Any) -> Any:
    try:
        json.dumps(value)
    except (TypeError, ValueError):
        return repr(value)
    return value


class _Resource:
    def __init__(self, client: AsyncStaqly) -> None:
        self._client = client
        self._http = client._http


# ---------------------------------------------------------------- collections


class AsyncCollections(_Resource):
    """Collections group items (e.g. "Instagram posts") and define their custom actions."""

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
        default_view: str | None = None,
        actions: Sequence[ActionDef | JSONMapping] | None = None,
        request_options: RequestOptions | None = None,
    ) -> Collection:
        body = clean(
            {
                "name": name,
                "description": description,
                "icon": icon,
                "color": color,
                "default_view": default_view,
                "actions": _dump(actions) if actions is not None else None,
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
        default_view: str | None = None,
        actions: Sequence[ActionDef | JSONMapping] | None = None,
        archived: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> Collection:
        """Renames, re-describes, archives or replaces the custom actions of a collection."""
        body = clean(
            {
                "name": name,
                "description": description,
                "icon": icon,
                "color": color,
                "default_view": default_view,
                "actions": _dump(actions) if actions is not None else None,
                "archived": archived,
            }
        )
        r = await self._http.request(
            RequestSpec(
                "PATCH", f"/v1/collections/{_q(collection_id)}", json=body, options=_opts(request_options)
            )
        )
        return _model(Collection, r)

    async def set_actions(
        self,
        collection_id: str,
        actions: Sequence[ActionDef | JSONMapping],
        *,
        request_options: RequestOptions | None = None,
    ) -> Collection:
        """Replaces the collection's custom actions (approve/reject are built in)."""
        return await self.update(collection_id, actions=actions, request_options=request_options)

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
        """Permanently deletes a collection and its items; ``confirm`` must equal its name."""
        await self._http.request(
            RequestSpec(
                "DELETE",
                f"/v1/collections/{_q(collection_id)}",
                params={"confirm": confirm},
                options=_opts(request_options),
            )
        )

    async def resolve_id(self, id_or_name: str, *, request_options: RequestOptions | None = None) -> str:
        """Returns the collection ID for an ID or name (no request when given an ID)."""
        if id_or_name.startswith("col_"):
            return id_or_name
        return (await self.get(id_or_name, request_options=request_options)).id


# ---------------------------------------------------------------------- items


class AsyncItems(_Resource):
    """Items are the content agents produce and humans review."""

    async def list(
        self,
        *,
        collection: str | None = None,
        collection_id: str | None = None,
        status: str | None = None,
        type: str | None = None,
        q: str | None = None,
        source: str | None = None,
        external_ref: str | None = None,
        created_after: TimeFilter | None = None,
        created_before: TimeFilter | None = None,
        decided_after: TimeFilter | None = None,
        decided_before: TimeFilter | None = None,
        sort: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> ItemPage:
        """One page of items. ``collection`` accepts an ID or a name (resolved with one request).

        ``sort``: relevance, created_desc (default), created_asc, updated_desc,
        updated_asc, decided_desc, title_asc. ``limit``: 1-200 (default 50).
        """
        if collection:
            collection_id = await self._client.collections.resolve_id(
                collection, request_options=request_options
            )
        params = {
            "collection_id": collection_id,
            "status": status,
            "type": type,
            "q": q,
            "source": source,
            "external_ref": external_ref,
            "created_after": created_after,
            "created_before": created_before,
            "decided_after": decided_after,
            "decided_before": decided_before,
            "sort": sort,
            "limit": limit,
            "cursor": cursor,
        }
        r = await self._http.request(
            RequestSpec("GET", "/v1/items", params=params, options=_opts(request_options))
        )
        return _model(ItemPage, r)

    async def search(
        self,
        q: str,
        *,
        collection: str | None = None,
        collection_id: str | None = None,
        status: str | None = None,
        type: str | None = None,
        sort: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> ItemPage:
        """Full-text search (prefix match over title, content and metadata), best match first."""
        return await self.list(
            q=q,
            collection=collection,
            collection_id=collection_id,
            status=status,
            type=type,
            sort=sort,
            limit=limit,
            cursor=cursor,
            request_options=request_options,
        )

    async def iterate(
        self,
        *,
        collection: str | None = None,
        collection_id: str | None = None,
        status: str | None = None,
        type: str | None = None,
        q: str | None = None,
        source: str | None = None,
        external_ref: str | None = None,
        created_after: TimeFilter | None = None,
        created_before: TimeFilter | None = None,
        decided_after: TimeFilter | None = None,
        decided_before: TimeFilter | None = None,
        sort: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> AsyncIterator[Item]:
        """Yields every matching item, fetching pages lazily (``limit`` is the page size)."""
        if collection:
            collection_id = await self._client.collections.resolve_id(
                collection, request_options=request_options
            )
        while True:
            page = await self.list(
                collection_id=collection_id,
                status=status,
                type=type,
                q=q,
                source=source,
                external_ref=external_ref,
                created_after=created_after,
                created_before=created_before,
                decided_after=decided_after,
                decided_before=decided_before,
                sort=sort,
                limit=limit,
                cursor=cursor,
                request_options=request_options,
            )
            for item in page.data:
                yield item
            if not page.next_cursor:
                return
            cursor = page.next_cursor

    async def get(self, item_id: str, *, request_options: RequestOptions | None = None) -> Item:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/items/{_q(item_id)}", options=_opts(request_options))
        )
        return _model(Item, r)

    async def create(
        self,
        collection: str,
        *,
        title: str | None = None,
        content: Any = None,
        url: str | None = None,
        media_id: str | None = None,
        metadata: JSONMapping | None = None,
        type: str | None = None,
        status: str | None = None,
        source: str | None = None,
        external_ref: str | None = None,
        expires_in: int | str | None = None,
        editable: Sequence[str] | None = None,
        request_options: RequestOptions | None = None,
    ) -> Item:
        """Creates an item in a collection (ID or exact name).

        ``content`` is a string or any JSON value. New items are ``pending``
        unless ``status`` says otherwise; approval rules may decide them at once.
        ``expires_in`` (seconds or e.g. ``"24h"``) expires undecided items;
        ``editable`` limits what reviewers may edit (``"content"``, ``"metadata.caption"`` …).
        """
        body = _item_fields(title=title, content=content, url=url, media_id=media_id, metadata=metadata)
        body.update(
            clean(
                {
                    "type": type,
                    "status": status,
                    "source": source,
                    "external_ref": external_ref,
                    "expires_in": expires_in,
                    "editable": list(editable) if editable is not None else None,
                }
            )
        )
        r = await self._http.request(
            RequestSpec(
                "POST", f"/v1/collections/{_q(collection)}/items", json=body, options=_opts(request_options)
            )
        )
        return _model(Item, r)

    async def upload(
        self,
        collection: str,
        file: FileInput,
        *,
        filename: str | None = None,
        content_type: str | None = None,
        title: str | None = None,
        content: Any = None,
        url: str | None = None,
        metadata: JSONMapping | None = None,
        type: str | None = None,
        status: str | None = None,
        source: str | None = None,
        external_ref: str | None = None,
        expires_in: int | str | None = None,
        editable: Sequence[str] | None = None,
        request_options: RequestOptions | None = None,
    ) -> Item:
        """Uploads a file (path, bytes or binary file object) and creates the item in one
        multipart request. The type (image/video/file) is inferred from the content."""
        fields = _item_fields(title=title, content=content, url=url, metadata=metadata)
        fields.update(
            clean(
                {
                    "type": type,
                    "status": status,
                    "source": source,
                    "external_ref": external_ref,
                    "expires_in": expires_in,
                    "editable": list(editable) if editable is not None else None,
                }
            )
        )
        r = await self._http.request(
            RequestSpec(
                "POST",
                f"/v1/collections/{_q(collection)}/items",
                files={"file": read_file(file, filename, content_type)},
                data={"item": json.dumps(fields)},
                options=_opts(request_options),
            )
        )
        return _model(Item, r)

    async def update(
        self,
        item_id: str,
        *,
        title: str | None = None,
        content: Any = None,
        url: str | None = None,
        media_id: str | None = None,
        metadata: JSONMapping | None = None,
        metadata_patch: JSONMapping | None = None,
        status: str | None = None,
        source: str | None = None,
        external_ref: str | None = None,
        collection_id: str | None = None,
        expected_version: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> Item:
        """Updates fields, moves the item, or changes its status (not to approved/rejected —
        use :meth:`approve`/:meth:`reject`). ``metadata_patch`` merges keys (``None`` removes one)."""
        body = _item_fields(
            title=title,
            content=content,
            url=url,
            media_id=media_id,
            metadata=metadata,
            metadata_patch=metadata_patch,
        )
        body.update(
            clean(
                {
                    "status": status,
                    "source": source,
                    "external_ref": external_ref,
                    "collection_id": collection_id,
                    "expected_version": expected_version,
                }
            )
        )
        r = await self._http.request(
            RequestSpec("PATCH", f"/v1/items/{_q(item_id)}", json=body, options=_opts(request_options))
        )
        return _model(Item, r)

    async def delete(self, item_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Deletes the item (its history is kept)."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/items/{_q(item_id)}", options=_opts(request_options))
        )

    async def purge(self, item_id: str, *, request_options: RequestOptions | None = None) -> PurgeResult:
        """Deletes the item **and its history** (events, webhook deliveries, unused media);
        only a content-free ``item.purged`` event remains. Needs ``admin``."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/items/{_q(item_id)}/purge", options=_opts(request_options))
        )
        return _model(PurgeResult, r)

    async def approve(
        self,
        item_id: str,
        *,
        feedback: str | None = None,
        edits: JSONMapping | None = None,
        payload: Any = None,
        expected_version: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> Item:
        """Approves a pending item (needs ``items:review``), optionally editing it in the same step."""
        return await self.action(
            item_id,
            "approve",
            feedback=feedback,
            edits=edits,
            payload=payload,
            expected_version=expected_version,
            request_options=request_options,
        )

    async def reject(
        self,
        item_id: str,
        *,
        feedback: str | None = None,
        edits: JSONMapping | None = None,
        payload: Any = None,
        expected_version: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> Item:
        """Rejects a pending item, usually with ``feedback`` for the agent."""
        return await self.action(
            item_id,
            "reject",
            feedback=feedback,
            edits=edits,
            payload=payload,
            expected_version=expected_version,
            request_options=request_options,
        )

    async def action(
        self,
        item_id: str,
        name: str,
        *,
        feedback: str | None = None,
        edits: JSONMapping | None = None,
        payload: Any = None,
        expected_version: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> Item:
        """Runs ``approve``, ``reject`` or a collection action (``regenerate``, ``publish`` …).
        ``payload`` (any JSON) is forwarded in the event and webhook."""
        body = clean(
            {
                "feedback": feedback,
                "edits": dict(edits) if edits is not None else None,
                "payload": payload,
                "expected_version": expected_version,
            }
        )
        r = await self._http.request(
            RequestSpec(
                "POST",
                f"/v1/items/{_q(item_id)}/actions/{_q(name)}",
                json=body,
                options=_opts(request_options),
            )
        )
        return _model(Item, r)

    async def list_actions(
        self, item_id: str, *, request_options: RequestOptions | None = None
    ) -> builtins.list[ActionDef]:
        """Actions the caller may run on the item in its current status."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/items/{_q(item_id)}/actions", options=_opts(request_options))
        )
        return _models(ActionDef, r)

    async def bulk_action(
        self,
        ids: Sequence[str],
        action: str,
        *,
        feedback: str | None = None,
        edits: JSONMapping | None = None,
        payload: Any = None,
        request_options: RequestOptions | None = None,
    ) -> builtins.list[BulkActionResult]:
        """Runs one action on up to 100 items; each succeeds or fails independently."""
        body = clean(
            {
                "ids": list(ids),
                "action": action,
                "feedback": feedback,
                "edits": dict(edits) if edits is not None else None,
                "payload": payload,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/items/bulk-actions", json=body, options=_opts(request_options))
        )
        return _models(BulkActionResult, r)

    async def history(
        self,
        item_id: str,
        *,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> EventPage:
        """Activity history of one item, newest first."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                f"/v1/items/{_q(item_id)}/events",
                params={"limit": limit, "cursor": cursor},
                options=_opts(request_options),
            )
        )
        return _model(EventPage, r)

    async def versions(
        self, item_id: str, *, request_options: RequestOptions | None = None
    ) -> builtins.list[ItemVersion]:
        """Snapshots of the item before each change."""
        r = await self._http.request(
            RequestSpec("GET", f"/v1/items/{_q(item_id)}/versions", options=_opts(request_options))
        )
        return _models(ItemVersion, r)

    async def wait(
        self, item_id: str, *, timeout: int | None = None, request_options: RequestOptions | None = None
    ) -> ItemWaitResult:
        """One long-poll round: the server holds the request up to ``timeout`` seconds
        (default 30, max 60) until the item leaves ``pending``."""
        opts = RequestOptions(**_opts(request_options))
        # the HTTP timeout must outlast the server-side wait
        opts["timeout"] = max(opts.get("timeout", 0.0), (timeout or 30) + 15.0)
        r = await self._http.request(
            RequestSpec("GET", f"/v1/items/{_q(item_id)}/wait", params={"timeout": timeout}, options=opts)
        )
        return _model(ItemWaitResult, r)

    async def wait_for_decision(
        self,
        item_id: str,
        *,
        timeout: float = DEFAULT_WAIT_TIMEOUT,
        on_poll: Callable[[Item], None] | None = None,
        request_options: RequestOptions | None = None,
    ) -> Item:
        """Waits until the item leaves ``pending`` (approved, rejected, expired, or moved on
        by an action or rule) and returns it.

        Uses the server's long-poll endpoint, re-issuing it until the item is decided —
        no busy polling. Raises :class:`~staqly.WaitTimeoutError` after ``timeout``
        seconds. For push instead of waiting, subscribe a webhook to
        ``item.approved`` / ``item.rejected`` / ``item.expired``.
        """
        started = time.monotonic()
        deadline = started + timeout
        while True:
            remaining = deadline - time.monotonic()
            secs = int(min(MAX_LONG_POLL, max(1, -(-remaining // 1))))
            res = await self.wait(item_id, timeout=secs, request_options=request_options)
            if on_poll is not None:
                on_poll(res.item)
            if res.decided or res.item.status != "pending":
                return res.item
            if time.monotonic() >= deadline:
                raise WaitTimeoutError(item_id, time.monotonic() - started, res.item)

    async def report_result(
        self,
        item_id: str,
        *,
        status: str,
        result: Any = None,
        error: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Item:
        """Reports what happened after acting on an approved (or processing) item:
        ``status="completed"`` with an optional JSON ``result`` (stored in
        ``metadata.result``) or ``status="failed"`` with an ``error``.
        Raises :class:`~staqly.ConflictError` unless the item is approved/processing."""
        body = clean({"status": status, "result": result, "error": error})
        r = await self._http.request(
            RequestSpec("POST", f"/v1/items/{_q(item_id)}/result", json=body, options=_opts(request_options))
        )
        return _model(Item, r)


# ---------------------------------------------------------------------- media


class AsyncMediaResource(_Resource):
    """Uploaded files. Items reference them via ``media_id``."""

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

    def url(self, media_id: str) -> str:
        """Absolute URL of a file (requires the same authentication as the API)."""
        return self._http.url(f"/v1/media/{_q(media_id)}")

    async def delete(self, media_id: str, *, request_options: RequestOptions | None = None) -> None:
        """Deletes a file; items referencing it lose their media."""
        await self._http.request(
            RequestSpec("DELETE", f"/v1/media/{_q(media_id)}", options=_opts(request_options))
        )


# --------------------------------------------------------------------- events


class AsyncEvents(_Resource):
    """The workspace activity log, newest first."""

    async def list(
        self,
        *,
        item_id: str | None = None,
        collection_id: str | None = None,
        type: str | None = None,
        actor_type: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> EventPage:
        params = {
            "item_id": item_id,
            "collection_id": collection_id,
            "type": type,
            "actor_type": actor_type,
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
        item_id: str | None = None,
        collection_id: str | None = None,
        type: str | None = None,
        actor_type: str | None = None,
        limit: int | None = None,
        cursor: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> AsyncIterator[Event]:
        """Yields every matching event across pages."""
        while True:
            page = await self.list(
                item_id=item_id,
                collection_id=collection_id,
                type=type,
                actor_type=actor_type,
                limit=limit,
                cursor=cursor,
                request_options=request_options,
            )
            for event in page.data:
                yield event
            if not page.next_cursor:
                return
            cursor = page.next_cursor


# ------------------------------------------------------------------- webhooks


class AsyncDeliveries(_Resource):
    """Delivery history of outgoing webhooks."""

    async def list(
        self,
        *,
        webhook_id: str | None = None,
        status: str | None = None,
        limit: int | None = None,
        request_options: RequestOptions | None = None,
    ) -> builtins.list[Delivery]:
        r = await self._http.request(
            RequestSpec(
                "GET",
                "/v1/webhook-deliveries",
                params={"webhook_id": webhook_id, "status": status, "limit": limit},
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
        """Creates a webhook. ``events`` are names or globs (``"item.*"``, default ``["*"]``);
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


# ---------------------------------------------------------------------- rules


class AsyncRules(_Resource):
    """Approval rules run on new pending items (lowest priority first, first match wins)."""

    async def list(self, *, request_options: RequestOptions | None = None) -> builtins.list[Rule]:
        r = await self._http.request(RequestSpec("GET", "/v1/rules", options=_opts(request_options)))
        return _models(Rule, r)

    async def get(self, rule_id: str, *, request_options: RequestOptions | None = None) -> Rule:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/rules/{_q(rule_id)}", options=_opts(request_options))
        )
        return _model(Rule, r)

    async def create(
        self,
        name: str,
        action: str,
        *,
        match: RuleMatch | JSONMapping | None = None,
        params: RuleParams | JSONMapping | None = None,
        priority: int | None = None,
        enabled: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> Rule:
        """Creates a rule. ``action``: auto_approve, auto_reject, set_status, expire, escalate.

        ``match`` example: ``{"collection_ids": [...], "conditions": [{"path": "score",
        "op": "gte", "value": 0.9}]}``.
        """
        body = clean(
            {
                "name": name,
                "action": action,
                "match": _dump(match) if isinstance(match, StaqlyModel) else match,
                "params": _dump(params) if isinstance(params, StaqlyModel) else params,
                "priority": priority,
                "enabled": enabled,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/rules", json=body, options=_opts(request_options))
        )
        return _model(Rule, r)

    async def update(
        self,
        rule_id: str,
        *,
        name: str | None = None,
        action: str | None = None,
        match: RuleMatch | JSONMapping | None = None,
        params: RuleParams | JSONMapping | None = None,
        priority: int | None = None,
        enabled: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> Rule:
        body = clean(
            {
                "name": name,
                "action": action,
                "match": _dump(match) if isinstance(match, StaqlyModel) else match,
                "params": _dump(params) if isinstance(params, StaqlyModel) else params,
                "priority": priority,
                "enabled": enabled,
            }
        )
        r = await self._http.request(
            RequestSpec("PATCH", f"/v1/rules/{_q(rule_id)}", json=body, options=_opts(request_options))
        )
        return _model(Rule, r)

    async def enable(self, rule_id: str, *, request_options: RequestOptions | None = None) -> Rule:
        return await self.update(rule_id, enabled=True, request_options=request_options)

    async def disable(self, rule_id: str, *, request_options: RequestOptions | None = None) -> Rule:
        return await self.update(rule_id, enabled=False, request_options=request_options)

    async def delete(self, rule_id: str, *, request_options: RequestOptions | None = None) -> None:
        await self._http.request(
            RequestSpec("DELETE", f"/v1/rules/{_q(rule_id)}", options=_opts(request_options))
        )


# ------------------------------------------------------------------- watchers


class AsyncWatchers(_Resource):
    """Watchers turn RSS feeds and web pages into items on a schedule."""

    async def list(self, *, request_options: RequestOptions | None = None) -> builtins.list[Watcher]:
        r = await self._http.request(RequestSpec("GET", "/v1/watchers", options=_opts(request_options)))
        return _models(Watcher, r)

    async def get(self, watcher_id: str, *, request_options: RequestOptions | None = None) -> Watcher:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/watchers/{_q(watcher_id)}", options=_opts(request_options))
        )
        return _model(Watcher, r)

    async def create(
        self,
        name: str,
        *,
        kind: str,
        url: str,
        collection: str,
        keywords: Sequence[str] | None = None,
        exclude_keywords: Sequence[str] | None = None,
        every: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Watcher:
        """Creates a watcher. ``kind``: ``rss`` or ``url_diff``; ``collection``: ID or name;
        ``every``: e.g. ``"30m"``, ``"6h"`` (plan minimums apply)."""
        body = clean(
            {
                "name": name,
                "kind": kind,
                "url": url,
                "collection": collection,
                "keywords": list(keywords) if keywords is not None else None,
                "exclude_keywords": list(exclude_keywords) if exclude_keywords is not None else None,
                "every": every,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/watchers", json=body, options=_opts(request_options))
        )
        return _model(Watcher, r)

    async def update(
        self,
        watcher_id: str,
        *,
        name: str | None = None,
        kind: str | None = None,
        url: str | None = None,
        collection: str | None = None,
        keywords: Sequence[str] | None = None,
        exclude_keywords: Sequence[str] | None = None,
        every: str | None = None,
        status: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Watcher:
        body = clean(
            {
                "name": name,
                "kind": kind,
                "url": url,
                "collection": collection,
                "keywords": list(keywords) if keywords is not None else None,
                "exclude_keywords": list(exclude_keywords) if exclude_keywords is not None else None,
                "every": every,
                "status": status,
            }
        )
        r = await self._http.request(
            RequestSpec("PATCH", f"/v1/watchers/{_q(watcher_id)}", json=body, options=_opts(request_options))
        )
        return _model(Watcher, r)

    async def pause(self, watcher_id: str, *, request_options: RequestOptions | None = None) -> Watcher:
        return await self.update(watcher_id, status="paused", request_options=request_options)

    async def resume(self, watcher_id: str, *, request_options: RequestOptions | None = None) -> Watcher:
        return await self.update(watcher_id, status="active", request_options=request_options)

    async def delete(self, watcher_id: str, *, request_options: RequestOptions | None = None) -> None:
        await self._http.request(
            RequestSpec("DELETE", f"/v1/watchers/{_q(watcher_id)}", options=_opts(request_options))
        )

    async def run(
        self, watcher_id: str, *, request_options: RequestOptions | None = None
    ) -> WatcherRunResult:
        """Checks the source now instead of waiting for the schedule."""
        r = await self._http.request(
            RequestSpec("POST", f"/v1/watchers/{_q(watcher_id)}/run", options=_opts(request_options))
        )
        return _model(WatcherRunResult, r)

    async def list_presets(
        self, *, request_options: RequestOptions | None = None
    ) -> builtins.list[WatcherPreset]:
        """Catalog of ready-made sources."""
        r = await self._http.request(
            RequestSpec("GET", "/v1/watcher-presets", options=_opts(request_options))
        )
        return _models(WatcherPreset, r)

    async def create_from_preset(
        self,
        preset_id: str,
        *,
        collection: str,
        params: Mapping[str, str] | None = None,
        name: str | None = None,
        every: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> Watcher:
        body = clean(
            {
                "preset_id": preset_id,
                "collection": collection,
                "params": dict(params) if params is not None else None,
                "name": name,
                "every": every,
            }
        )
        r = await self._http.request(
            RequestSpec("POST", "/v1/watchers/from-preset", json=body, options=_opts(request_options))
        )
        return _model(Watcher, r)


# ---------------------------------------------------------- workspace & keys


class AsyncWorkspaceResource(_Resource):
    """The workspace the client is authenticated to."""

    async def get(self, *, request_options: RequestOptions | None = None) -> Workspace:
        r = await self._http.request(RequestSpec("GET", "/v1/workspace", options=_opts(request_options)))
        return _model(Workspace, r)

    async def rename(self, name: str, *, request_options: RequestOptions | None = None) -> Workspace:
        """Renames the workspace (needs the ``admin`` scope)."""
        r = await self._http.request(
            RequestSpec("PATCH", "/v1/workspace", json={"name": name}, options=_opts(request_options))
        )
        return _model(Workspace, r)

    async def usage(self, *, request_options: RequestOptions | None = None) -> WorkspaceUsage:
        """Current plan, period and usage (local installs report the ``unlocked`` plan)."""
        r = await self._http.request(
            RequestSpec("GET", "/v1/workspace/usage", options=_opts(request_options))
        )
        return _model(WorkspaceUsage, r)


class AsyncWorkspaces(_Resource):
    """Cloud signup."""

    async def create(
        self,
        *,
        name: str | None = None,
        setup_token: str | None = None,
        request_options: RequestOptions | None = None,
    ) -> WorkspaceCreated:
        """Creates a cloud workspace on the Free plan. Returns its first API key
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


# ------------------------------------------------------- notification channels


class AsyncNotificationChannels(_Resource):
    """Push review requests to Telegram, Slack, Discord, ntfy or email (needs ``channels:manage``).

    Secrets in ``config`` are write-only and come back masked.
    """

    async def list(
        self, *, request_options: RequestOptions | None = None
    ) -> builtins.list[NotificationChannel]:
        r = await self._http.request(
            RequestSpec("GET", "/v1/notification-channels", options=_opts(request_options))
        )
        return _models(NotificationChannel, r)

    async def get(
        self, channel_id: str, *, request_options: RequestOptions | None = None
    ) -> NotificationChannel:
        r = await self._http.request(
            RequestSpec("GET", f"/v1/notification-channels/{_q(channel_id)}", options=_opts(request_options))
        )
        return _model(NotificationChannel, r)

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
    ) -> NotificationChannel:
        """Creates a channel. ``type``: telegram, slack, discord, ntfy or email; ``config`` is
        type-specific (e.g. ntfy: ``{"topic": …}``). Plan limit ``notification_channels`` (402)."""
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
            RequestSpec("POST", "/v1/notification-channels", json=body, options=_opts(request_options))
        )
        return _model(NotificationChannel, r)

    async def update(
        self,
        channel_id: str,
        *,
        name: str | None = None,
        config: JSONMapping | None = None,
        events: Sequence[str] | None = None,
        collection_ids: Sequence[str] | None = None,
        digest_window_seconds: int | None = None,
        enabled: bool | None = None,
        request_options: RequestOptions | None = None,
    ) -> NotificationChannel:
        """Updates a channel. ``config`` keys are merged (``None`` values remove a key, a masked
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
                f"/v1/notification-channels/{_q(channel_id)}",
                json=body,
                options=_opts(request_options),
            )
        )
        return _model(NotificationChannel, r)

    async def enable(
        self, channel_id: str, *, request_options: RequestOptions | None = None
    ) -> NotificationChannel:
        return await self.update(channel_id, enabled=True, request_options=request_options)

    async def disable(
        self, channel_id: str, *, request_options: RequestOptions | None = None
    ) -> NotificationChannel:
        return await self.update(channel_id, enabled=False, request_options=request_options)

    async def delete(self, channel_id: str, *, request_options: RequestOptions | None = None) -> None:
        await self._http.request(
            RequestSpec(
                "DELETE", f"/v1/notification-channels/{_q(channel_id)}", options=_opts(request_options)
            )
        )

    async def test(
        self, channel_id: str, *, request_options: RequestOptions | None = None
    ) -> NotificationTestResult:
        """Sends a test message now; delivery problems are reported in ``ok``/``error``, not raised."""
        r = await self._http.request(
            RequestSpec(
                "POST", f"/v1/notification-channels/{_q(channel_id)}/test", options=_opts(request_options)
            )
        )
        return _model(NotificationTestResult, r)

    async def deliveries(
        self, channel_id: str, *, limit: int | None = None, request_options: RequestOptions | None = None
    ) -> builtins.list[NotificationDelivery]:
        """Recent notification outbox rows for a channel."""
        r = await self._http.request(
            RequestSpec(
                "GET",
                f"/v1/notification-channels/{_q(channel_id)}/deliveries",
                params={"limit": limit},
                options=_opts(request_options),
            )
        )
        return _models(NotificationDelivery, r)


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
    item_id: str | None,
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
        "item_id": item_id,
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
        item_id: str | None = None,
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
            item_id=item_id,
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
        item_id: str | None = None,
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
            item_id=item_id,
            format=format,
        )
        spec = RequestSpec("GET", "/v1/events/export", params=params, options=_opts(request_options))
        return await self._save(spec, path)

    async def create_workspace_export(
        self, *, request_options: RequestOptions | None = None
    ) -> WorkspaceExport:
        """Starts a full workspace export (admin). Only one may be pending/running
        (409 with ``details["export_id"]``); archives are deleted after 24 h."""
        r = await self._http.request(
            RequestSpec("POST", "/v1/workspace/export", options=_opts(request_options))
        )
        return _model(WorkspaceExport, r)

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
    """staq.ly API client.

    ::

        staqly = AsyncStaqly()                                   # local mode, http://localhost:3333
        cloud = AsyncStaqly(base_url="https://staq.example.com", api_key="stk_…")

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
        :param api_key: ``stk_…`` key for cloud mode (default ``STAQLY_API_KEY``); omit for local mode.
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
        self.items = AsyncItems(self)
        self.media = AsyncMediaResource(self)
        self.events = AsyncEvents(self)
        self.webhooks = AsyncWebhooks(self)
        self.rules = AsyncRules(self)
        self.watchers = AsyncWatchers(self)
        self.workspace = AsyncWorkspaceResource(self)
        self.workspaces = AsyncWorkspaces(self)
        self.plans = AsyncPlans(self)
        self.api_keys = AsyncAPIKeys(self)
        self.notification_channels = AsyncNotificationChannels(self)
        self.exports = AsyncExports(self)

    @property
    def base_url(self) -> str:
        return self._http.base_url

    @property
    def api_key(self) -> str | None:
        return self._http.api_key

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

    async def whoami(self, *, request_options: RequestOptions | None = None) -> Whoami:
        """Server mode, workspace and the calling principal with its scopes."""
        r = await self._http.request(RequestSpec("GET", "/v1/session", options=_opts(request_options)))
        return _model(Whoami, r)

    async def request_approval(
        self,
        collection: str,
        *,
        title: str,
        content: Any = None,
        metadata: JSONMapping | None = None,
        url: str | None = None,
        media_id: str | None = None,
        type: str | None = None,
        source: str | None = None,
        external_ref: str | None = None,
        expires_in: int | str | None = None,
        editable: Sequence[str] | None = None,
        timeout: float = DEFAULT_WAIT_TIMEOUT,
        request_options: RequestOptions | None = None,
    ) -> Decision:
        """Human approval gate: creates an item and waits until it is decided.

        Returns a :class:`~staqly.models.Decision` (never raises on rejection or
        expiry — check ``decision.approved``). Raises
        :class:`~staqly.WaitTimeoutError` if nobody decides within ``timeout`` seconds.
        """
        created = await self.items.create(
            collection,
            title=title,
            content=content,
            metadata=metadata,
            url=url,
            media_id=media_id,
            type=type,
            source=source,
            external_ref=external_ref,
            expires_in=expires_in,
            editable=editable,
            request_options=request_options,
        )
        item = created
        if created.status == "pending":
            item = await self.items.wait_for_decision(
                created.id, timeout=timeout, request_options=request_options
            )
        return Decision(
            item=item,
            status=item.status,
            approved=item.status == "approved",
            expired=item.status == "expired",
            feedback=item.feedback,
        )

    def requires_approval(
        self,
        collection: str,
        *,
        title: str | Callable[..., str],
        content: Any = None,
        metadata: JSONMapping | Callable[..., JSONMapping] | None = None,
        expires_in: int | str | None = None,
        editable: Sequence[str] | None = None,
        timeout: float = DEFAULT_WAIT_TIMEOUT,
        report: bool = True,
    ) -> Callable[[Callable[P, cabc.Awaitable[R]]], Callable[P, cabc.Awaitable[R]]]:
        """Decorator: every call first asks a human.

        The decorated function runs only after approval; afterwards the item is
        reported ``completed`` (with the return value as ``result``) or ``failed``
        (the exception is re-raised). A rejection or expiry raises
        :class:`~staqly.RejectedError`. ``title``, ``content`` and ``metadata``
        may be callables receiving the function's arguments; ``content``
        defaults to the arguments as JSON.

        ::

            @staqly.requires_approval("Outbox", title=lambda to, **_: f"Email {to}?")
            def send_email(to: str, body: str) -> str: ...
        """

        def build(value: Any, args: tuple[Any, ...], kwargs: dict[str, Any]) -> Any:
            return value(*args, **kwargs) if callable(value) else value

        def decorator(fn: Callable[P, cabc.Awaitable[R]]) -> Callable[P, cabc.Awaitable[R]]:
            @functools.wraps(fn)
            async def wrapper(*args: P.args, **kwargs: P.kwargs) -> R:
                body = build(content, args, kwargs)
                if body is None:
                    body = _jsonable({"args": list(args), "kwargs": kwargs})
                decision = await self.request_approval(
                    collection,
                    title=build(title, args, kwargs),
                    content=body,
                    metadata=build(metadata, args, kwargs),
                    expires_in=expires_in,
                    editable=editable,
                    timeout=timeout,
                )
                if not decision.approved:
                    raise RejectedError(decision.item)
                try:
                    result = await fn(*args, **kwargs)
                except Exception as exc:
                    if report:
                        # the original error matters more than a failed report
                        with contextlib.suppress(Exception):
                            await self.items.report_result(decision.item.id, status="failed", error=str(exc))
                    raise
                if report:
                    await self.items.report_result(
                        decision.item.id, status="completed", result=_jsonable(result)
                    )
                return result

            return wrapper

        return decorator
