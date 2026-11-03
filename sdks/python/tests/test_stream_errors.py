from __future__ import annotations

import httpx
import pytest

from staqly import APIConnectionError, AsyncStaqly, Staqly

from .conftest import Mock
from .test_exports import NDJSON, InterruptedStream


def test_interrupted_error_response_is_closed_and_typed(client: Staqly, mock: Mock) -> None:
    stream = InterruptedStream()
    mock.route.side_effect = lambda _: httpx.Response(503, stream=stream)
    with pytest.raises(APIConnectionError):
        list(client.exports.events(request_options={"max_retries": 0}))
    assert stream.closed


def test_interrupted_error_response_can_be_retried(client: Staqly, mock: Mock) -> None:
    stream = InterruptedStream()
    mock.route.side_effect = [
        httpx.Response(503, stream=stream),
        httpx.Response(200, content=NDJSON.encode()),
    ]
    assert len(list(client.exports.events())) == 3
    assert stream.closed
    assert len(mock.requests) == 2


async def test_interrupted_error_response_is_closed_and_typed_async(aclient: AsyncStaqly, mock: Mock) -> None:
    stream = InterruptedStream()
    mock.route.side_effect = lambda _: httpx.Response(503, stream=stream)
    with pytest.raises(APIConnectionError):
        _ = [event async for event in aclient.exports.events(request_options={"max_retries": 0})]
    assert stream.closed


async def test_interrupted_error_response_can_be_retried_async(aclient: AsyncStaqly, mock: Mock) -> None:
    stream = InterruptedStream()
    mock.route.side_effect = [
        httpx.Response(503, stream=stream),
        httpx.Response(200, content=NDJSON.encode()),
    ]
    assert len([event async for event in aclient.exports.events()]) == 3
    assert stream.closed
    assert len(mock.requests) == 2
