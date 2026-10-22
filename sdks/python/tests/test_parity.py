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
