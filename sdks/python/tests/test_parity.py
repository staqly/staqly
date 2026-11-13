"""The Python SDK calls the same REST operations as the TypeScript SDK (sdks/typescript):
neither falls behind."""

from __future__ import annotations

import os
import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
TS = ROOT.parent / "typescript" / "src"
# Outside the monorepo (the published sdist) there is nothing to compare with; in CI a missing neighbour is a
# moved directory, and skipping would let the SDKs drift apart unseen.
pytestmark = pytest.mark.skipif(
    not TS.exists() and not os.environ.get("CI"),
    reason="the TypeScript SDK is not next to this one (outside the monorepo)",
)


def _norm(path: str) -> str:
    # Path parameters as {}, without a query string: `${enc(id)}` and `{_q(post_id)}` both read {}.
    return re.sub(r"\$\{[^}]*\}|\{[^}]*\}", "{}", path).split("?")[0]


def _typescript() -> set[tuple[str, str]]:
    calls = set()
    for f in TS.rglob("*.ts"):
        for m in re.finditer(r"method:\s*'([A-Z]+)'\s*,\s*path:\s*(`[^`]*`|'[^']*')", f.read_text()):
            calls.add((m.group(1), _norm(m.group(2)[1:-1])))
    return calls


def _python() -> set[tuple[str, str]]:
    source = (ROOT / "src" / "staqly" / "_client_async.py").read_text()
    return {
        (m.group(1), _norm(m.group(2)))
        for m in re.finditer(r'RequestSpec\(\s*"([A-Z]+)"\s*,\s*f?"([^"]*)"', source)
    }


def test_both_sdks_call_the_same_operations() -> None:
    ts, py = _typescript(), _python()
    assert len(ts) > 100 and len(py) > 100, (
        "the call patterns changed: update this test's regular expressions"
    )
    assert sorted(ts - py) == [], "operations only the TypeScript SDK calls"
    assert sorted(py - ts) == [], "operations only the Python SDK calls"


def test_both_sdks_raise_the_same_error_for_a_status() -> None:
    """The same status is the same typed error in both SDKs (errors.ts makeAPIError, errors.py _BY_STATUS)."""
    from staqly import errors

    source = (TS / "errors.ts").read_text()
    ts = {int(m.group(1)): m.group(2) for m in re.finditer(r"case (\d+):\s*return new (\w+)\(", source)}
    # 402 is checked before the switch, together with the plan_limit_exceeded code.
    m = re.search(r"status === (\d+)\) \{\s*return new (\w+)\(", source)
    assert m, "errors.ts changed: update this test's regular expressions"
    ts[int(m.group(1))] = m.group(2)
    assert len(ts) >= 11, "errors.ts changed: update this test's regular expressions"
    assert ts == {status: cls.__name__ for status, cls in errors._BY_STATUS.items()}
    assert "status >= 500" in source and "InternalServerError" in source
    # Every error class one SDK exports, the other does too (a caller's abort is JavaScript's alone).
    ts_classes = set(
        re.findall(r"export class (\w+Error) extends", "".join(f.read_text() for f in TS.rglob("*.ts")))
    )
    py_classes = {n for n in errors.__all__ if n.endswith("Error")}
    assert ts_classes - py_classes == {"APIUserAbortError"}
    assert py_classes - ts_classes == set()


SPEC = ROOT.parents[1] / "apps" / "api" / "openapi.yaml"

# Operations no API client calls: browser redirects and callbacks, sign-in, the signed-in person's account,
# members and invitations (people are managed by people, never by keys), billing, the public pages behind a
# link's token, the dashboard's assistant and OAuth account picker, and endpoints for other servers.
NOT_IN_SDK = {
    ("POST", "/v1/feedback"),  # signed-in people submit from the dashboard; API keys cannot
    ("GET", "/v1/workspaces"),
    ("PATCH", "/v1/account"),
    ("POST", "/v1/account/workspaces"),
    ("PATCH", "/v1/members/{}"),
    ("DELETE", "/v1/members/{}"),
    ("GET", "/v1/oauth/{}/client-metadata.json"),
    ("GET", "/v1/media-links/{}/{}/{}"),  # platforms download from these links, clients never call them
    ("POST", "/v1/notifications/unsubscribe"),
    ("POST", "/v1/billing/webhook"),
    ("GET", "/v1/meta/webhook"),  # Meta calls these, clients never do
    ("POST", "/v1/meta/webhook"),
    ("POST", "/v1/tiktok/webhook"),  # TikTok calls it, clients never do
    ("POST", "/v1/kick/webhook"),  # Kick calls it, clients never do
    ("POST", "/v1/whop/webhook/{}"),  # Whop calls it, clients never do
    ("POST", "/v1/meta/deauthorize"),
    ("POST", "/v1/meta/data-deletion"),
    ("GET", "/v1/meta/data-deletion/{}"),
    ("GET", "/v1/channels/oauth/{}/callback"),
    ("GET", "/v1/pending-connections/{}"),
    ("POST", "/v1/pending-connections/{}"),
    ("POST", "/v1/ai/post"),  # the dashboard composer's assistant, like /v1/assistant/* below
    ("POST", "/mcp"),
}
NOT_IN_SDK_PREFIXES = (
    "/v1/auth/",
    "/v1/account/",
    "/v1/session/",
    "/v1/invitations",
    "/v1/billing/addons",
    "/v1/billing/pending-payment",
    "/v1/billing/promotion-code",
    "/v1/billing/start-paid-plan",
    "/v1/billing/sync",
    "/v1/previews/",
    "/v1/connect-links/",
    "/v1/assistant/",
)


@pytest.mark.skipif(
    not SPEC.exists() and not os.environ.get("CI"), reason="openapi.yaml not found (outside the monorepo)"
)
def test_every_operation_is_in_the_sdk_or_left_out_on_purpose() -> None:
    import yaml

    spec = yaml.safe_load(SPEC.read_text())
    ops = {
        (method.upper(), _norm(path))
        for path, item in spec["paths"].items()
        for method in item
        if method in ("get", "post", "put", "patch", "delete")
    }
    left_out = {op for op in ops if op in NOT_IN_SDK or op[1].startswith(NOT_IN_SDK_PREFIXES)}
    missing = sorted(ops - _python() - left_out)
    assert missing == [], "add these to both SDKs, or to NOT_IN_SDK with the reason"
    assert sorted(left_out & _python()) == [], "the SDK calls these: take them off NOT_IN_SDK"
    assert sorted(NOT_IN_SDK - ops) == [], "the API no longer has these: take them off NOT_IN_SDK"
    stale = [p for p in NOT_IN_SDK_PREFIXES if not any(op[1].startswith(p) for op in ops)]
    assert stale == [], "no operation starts with these: take them off NOT_IN_SDK_PREFIXES"
