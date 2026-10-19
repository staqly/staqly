from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import httpx
import pytest

from staqly import AsyncStaqly, ExportFailedError, InternalServerError, Staqly, WaitTimeoutError

from .conftest import Mock, jsonr

TS = "2026-01-01T00:00:00Z"


def ev(i: str) -> dict[str, Any]:
    return {
        "id": i,
        "seq": 1,
        "type": "post.created",
        "actor": {"type": "local", "id": "l", "name": "L"},
        "data": {},
        "created_at": TS,
    }


NDJSON = "\n".join(json.dumps(ev(i)) for i in ("e1", "e2", "e3")) + "\n"


def job(status: str, **kw: Any) -> httpx.Response:
    return jsonr({"id": "exp_1", "status": status, "size_bytes": 0, "created_at": TS, "expires_at": TS, **kw})


def test_streams_events(client: Staqly, mock: Mock) -> None:
    mock.reply(httpx.Response(200, content=NDJSON.encode(), headers={"Content-Type": "application/x-ndjson"}))
    ids = [e.id for e in client.exports.events(from_="2026-10-01", type_prefix="post.")]
    assert ids == ["e1", "e2", "e3"]
    assert mock.last.url.path == "/v1/events/export"
    assert dict(mock.last.url.params) == {"format": "ndjson", "from": "2026-10-01", "type_prefix": "post."}


async def test_streams_events_async(aclient: AsyncStaqly, mock: Mock) -> None:
    mock.reply(httpx.Response(200, content=NDJSON.encode()))
    assert [e.id async for e in aclient.exports.events()] == ["e1", "e2", "e3"]


def test_stream_errors_are_typed_and_retried(client: Staqly, mock: Mock) -> None:
    mock.reply(
        httpx.Response(503, json={"error": {"code": "x", "message": "m"}}, headers={"Retry-After": "0"}),
        httpx.Response(200, content=NDJSON.encode()),
    )
    assert len(list(client.exports.events())) == 3
    assert len(mock.requests) == 2
    mock.reply(httpx.Response(500, json={"error": {"code": "internal", "message": "m"}}))
    with pytest.raises(InternalServerError):
        list(client.exports.events(request_options={"max_retries": 0}))


def test_save_events_csv(client: Staqly, mock: Mock, tmp_path: Path) -> None:
    mock.reply(httpx.Response(200, content=b"created_at,id\n1,2\n", headers={"Content-Type": "text/csv"}))
    out = client.exports.save_events(tmp_path / "events.csv", format="csv", post_id="pst_1")
    assert out.read_bytes() == b"created_at,id\n1,2\n"
    assert mock.last.url.params["format"] == "csv"


def test_export_workspace_waits_and_saves(client: Staqly, mock: Mock, tmp_path: Path) -> None:
    mock.reply(
        job("pending"),
        job("running"),
        job("ready", download_url="/v1/workspace/exports/exp_1/download"),
        httpx.Response(200, content=b"PK\x03\x04zip"),
    )
    out = client.exports.export_workspace(tmp_path / "ws.zip", poll_interval=0.01)
    assert out.status == "ready"
    assert (tmp_path / "ws.zip").read_bytes() == b"PK\x03\x04zip"
    assert [f"{r.method} {r.url.path}" for r in mock.requests] == [
        "POST /v1/workspace/export",
        "GET /v1/workspace/exports/exp_1",
        "GET /v1/workspace/exports/exp_1",
        "GET /v1/workspace/exports/exp_1/download",
    ]


async def test_export_workspace_async(aclient: AsyncStaqly, mock: Mock, tmp_path: Path) -> None:
    mock.reply(job("pending"), job("ready"), httpx.Response(200, content=b"PK"))
    await aclient.exports.export_workspace(tmp_path / "a.zip", poll_interval=0.01)
    assert (tmp_path / "a.zip").read_bytes() == b"PK"


def test_failed_export(client: Staqly, mock: Mock) -> None:
    mock.reply(job("failed", error="disk full"))
    with pytest.raises(ExportFailedError) as exc:
        client.exports.wait_for_workspace_export("exp_1")
    assert exc.value.job.error == "disk full"


def test_export_timeout(client: Staqly, mock: Mock) -> None:
    mock.reply(job("running"))
    with pytest.raises(WaitTimeoutError):
        client.exports.wait_for_workspace_export("exp_1", timeout=0.05, poll_interval=0.01)


def test_ready_is_not_retried(client: Staqly, mock: Mock) -> None:
    mock.reply(httpx.Response(503, json={"status": "unavailable", "checks": {"database": "unavailable"}}))
    with pytest.raises(InternalServerError) as exc:
        client.ready()
    assert exc.value.body["status"] == "unavailable"
    assert len(mock.requests) == 1
