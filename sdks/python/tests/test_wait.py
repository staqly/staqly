from __future__ import annotations

import pytest

from staqly import AsyncStaqly, Staqly, WaitTimeoutError

from .conftest import Mock, jsonr, post, target


class TestWaitForPublished:
    def test_polls_until_published(self, client: Staqly, mock: Mock) -> None:
        mock.reply(
            jsonr(post(status="publishing")),
            jsonr(post(status="publishing")),
            jsonr(post(status="published", targets=[target(status="published", external_url="https://x/1")])),
        )
        seen: list[str] = []
        out = client.posts.wait_for_published(
            "pst_1", poll_interval=0.01, on_poll=lambda p: seen.append(p.status)
        )
        assert out.status == "published"
        assert out.targets[0].external_url == "https://x/1"
        assert seen == ["publishing", "publishing", "published"]
        assert [r.url.path for r in mock.requests] == ["/v1/posts/pst_1"] * 3

    def test_failed_is_returned_not_raised(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(post(status="failed", targets=[target(status="failed", error="token expired")])))
        out = client.posts.wait_for_published("pst_1")
        assert out.is_failed and out.targets[0].error == "token expired"

    def test_timeout(self, client: Staqly, mock: Mock) -> None:
        mock.reply(jsonr(post(status="publishing")))
        with pytest.raises(WaitTimeoutError) as exc:
            client.posts.wait_for_published("pst_1", timeout=0.01, poll_interval=0.01)
        assert exc.value.id == "pst_1"
        assert exc.value.post is not None and exc.value.post.status == "publishing"
        assert isinstance(exc.value, TimeoutError)

    async def test_async(self, aclient: AsyncStaqly, mock: Mock) -> None:
        mock.reply(jsonr(post(status="publishing")), jsonr(post(status="published")))
        assert (await aclient.posts.wait_for_published("pst_1", poll_interval=0.01)).status == "published"
        mock.reply(jsonr(post(status="publishing")))
        with pytest.raises(WaitTimeoutError):
            await aclient.posts.wait_for_published("pst_1", timeout=0.01, poll_interval=0.01)
