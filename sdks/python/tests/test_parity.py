"""The Python SDK calls the same REST operations as the TypeScript SDK (sdks/typescript):
neither falls behind."""

from __future__ import annotations

import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
TS = ROOT.parent / "typescript" / "src"
pytestmark = pytest.mark.skipif(
    not TS.exists(), reason="the TypeScript SDK is not next to this one (outside the monorepo)"
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


SPEC = ROOT.parents[1] / "apps" / "api" / "openapi.yaml"

# Operations no API client calls: browser redirects and callbacks, sign-in, the signed-in person's account,
# members and invitations (people are managed by people, never by keys), billing, the public pages behind a
# link's token, the dashboard's assistant and OAuth account picker, and endpoints for other servers.
NOT_IN_SDK = {
    ("GET", "/v1/workspaces"),
    ("PATCH", "/v1/account"),
    ("POST", "/v1/account/workspaces"),
    ("PATCH", "/v1/members/{}"),
    ("DELETE", "/v1/members/{}"),
    ("GET", "/v1/oauth/{}/client-metadata.json"),
    ("GET", "/v1/media-links/{}/{}/{}"),  # platforms download from these links, clients never call them
    ("POST", "/v1/notifications/unsubscribe"),
    ("POST", "/v1/billing/webhook"),
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


@pytest.mark.skipif(not SPEC.exists(), reason="openapi.yaml not found (outside the monorepo)")
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
