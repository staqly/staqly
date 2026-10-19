from __future__ import annotations

from staqly import AsyncStaqly, Staqly

from .conftest import Mock, jsonr, post

EVENT = {
    "seq": 1,
    "type": "post.created",
    "actor": {"type": "local", "id": "l", "name": "L"},
    "created_at": "2026-01-01T00:00:00Z",
}


def test_iterates_posts_across_pages(client: Staqly, mock: Mock) -> None:
    mock.reply(
        jsonr({"data": [post(id="a"), post(id="b")], "next_cursor": "c1"}),
        jsonr({"data": [post(id="c")], "next_cursor": "c2"}),
        jsonr({"data": []}),
    )
    ids = [it.id for it in client.posts.iterate(status="draft", limit=2)]
    assert ids == ["a", "b", "c"]
    assert [r.url.params.get("cursor") for r in mock.requests] == [None, "c1", "c2"]
    assert all(r.url.params["limit"] == "2" for r in mock.requests)


def test_is_lazy(client: Staqly, mock: Mock) -> None:
    mock.reply(jsonr({"data": [post(id="a"), post(id="b")], "next_cursor": "c1"}))
    it = client.posts.iterate()
    assert mock.requests == []
    assert next(it).id == "a"
    assert len(mock.requests) == 1


def test_iterate_passes_filters(client: Staqly, mock: Mock) -> None:
    mock.reply(jsonr({"data": [post()], "next_cursor": "x"}), jsonr({"data": [post(id="2")]}))
    assert len(list(client.posts.iterate(collection_id="News", tag="a"))) == 2
    assert [r.url.path for r in mock.requests] == ["/v1/posts", "/v1/posts"]
    assert mock.requests[1].url.params["cursor"] == "x"
    assert mock.requests[1].url.params["collection_id"] == "News"


def test_iterates_jobs(client: Staqly, mock: Mock) -> None:
    job = {
        "id": "job_1",
        "kind": "publish_post",
        "status": "queued",
        "run_at": "2026-01-01T00:00:00Z",
        "created_at": "2026-01-01T00:00:00Z",
        "updated_at": "2026-01-01T00:00:00Z",
    }
    mock.reply(jsonr({"data": [job], "next_cursor": "x"}), jsonr({"data": [job | {"id": "job_2"}]}))
    assert [j.id for j in client.jobs.iterate(status="queued")] == ["job_1", "job_2"]


def test_iterates_events(client: Staqly, mock: Mock) -> None:
    mock.reply(
        jsonr({"data": [EVENT | {"id": "e1"}], "next_cursor": "x"}), jsonr({"data": [EVENT | {"id": "e2"}]})
    )
    assert [e.id for e in client.events.iterate(type="post.created")] == ["e1", "e2"]
    assert mock.requests[1].url.params["cursor"] == "x"


async def test_async_iteration(aclient: AsyncStaqly, mock: Mock) -> None:
    mock.reply(jsonr({"data": [post(id="a")], "next_cursor": "c1"}), jsonr({"data": [post(id="b")]}))
    ids = [it.id async for it in aclient.posts.iterate()]
    assert ids == ["a", "b"]
    mock.reply(jsonr({"data": [EVENT | {"id": "e1"}]}))
    assert [e.id async for e in aclient.events.iterate()] == ["e1"]
