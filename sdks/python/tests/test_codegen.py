from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def test_sync_client_is_up_to_date() -> None:
    """`_client_sync.py` must be regenerated (`make gen`) after editing `_client_async.py`."""
    res = subprocess.run(
        [sys.executable, str(ROOT / "scripts" / "gen_sync.py"), "--check"], capture_output=True, text=True
    )
    assert res.returncode == 0, res.stderr
