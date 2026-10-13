from __future__ import annotations

from staqly import AsyncStaqly, Staqly

from .conftest import Mock, item, jsonr

EVENT = {
    "seq": 1,
    "type": "item.created",
    "actor": {"type": "local", "id": "l", "name": "L"},
    "created_at": "2026-01-01T00:00:00Z",
}


def test_iterates_items_across_pages(client: Staqly, mock: Mock) -> None:
    mock.reply(
        jsonr({"data": [item(id="a"), item(id="b")], "next_cursor": "c1"}),
        jsonr({"data": [item(id="c")], "next_cursor": "c2"}),
        jsonr({"data": []}),
    )
    ids = [it.id for it in client.items.iterate(status="pending", limit=2)]
    assert ids == ["a", "b", "c"]
    assert [r.url.params.get("cursor") for r in mock.requests] == [None, "c1", "c2"]
    assert all(r.url.params["limit"] == "2" for r in mock.requests)


def test_is_lazy(client: Staqly, mock: Mock) -> None:
    mock.reply(jsonr({"data": [item(id="a"), item(id="b")], "next_cursor": "c1"}))
    it = client.items.iterate()
    assert mock.requests == []
    assert next(it).id == "a"
    assert len(mock.requests) == 1


def test_iterate_resolves_collection_name_once(client: Staqly, mock: Mock) -> None:
    col = {
        "id": "col_3",
        "name": "News",
        "created_at": "2026-01-01T00:00:00Z",
        "updated_at": "2026-01-01T00:00:00Z",
    }
    mock.reply(jsonr(col), jsonr({"data": [item()], "next_cursor": "x"}), jsonr({"data": [item(id="2")]}))
    assert len(list(client.items.iterate(collection="News"))) == 2
    assert [r.url.path for r in mock.requests] == ["/v1/collections/News", "/v1/items", "/v1/items"]
    assert mock.requests[2].url.params["collection_id"] == "col_3"


def test_iterates_events(client: Staqly, mock: Mock) -> None:
    mock.reply(
        jsonr({"data": [EVENT | {"id": "e1"}], "next_cursor": "x"}), jsonr({"data": [EVENT | {"id": "e2"}]})
    )
    assert [e.id for e in client.events.iterate(type="item.created")] == ["e1", "e2"]
    assert mock.requests[1].url.params["cursor"] == "x"


async def test_async_iteration(aclient: AsyncStaqly, mock: Mock) -> None:
    mock.reply(jsonr({"data": [item(id="a")], "next_cursor": "c1"}), jsonr({"data": [item(id="b")]}))
    ids = [it.id async for it in aclient.items.iterate()]
    assert ids == ["a", "b"]
    mock.reply(jsonr({"data": [EVENT | {"id": "e1"}]}))
    assert [e.id async for e in aclient.events.iterate()] == ["e1"]
