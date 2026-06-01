"""Shared pytest configuration and fixtures."""

import sys
from pathlib import Path

REPO_ROOT = Path(__file__).parent.parent
DASHBOARD_DIR = REPO_ROOT / "dashboard"

# Add repo root so `from dashboard.x import ...` works.
# Add dashboard dir so bare `import x` still works (e.g. within dashboard modules).
for p in (str(REPO_ROOT), str(DASHBOARD_DIR)):
    if p not in sys.path:
        sys.path.insert(0, p)
