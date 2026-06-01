"""Fixtures for integration tests.

These tests require real AH credentials and will make live API calls.
Credentials are loaded from AH_ACCESS_TOKEN / AH_REFRESH_TOKEN env vars,
or from the config file found at one of these paths (in order):
  - $CONFIG_PATH (env var)
  - $XDG_CONFIG_HOME/appie/appie.json  (~/.config/appie/appie.json)
  - <repo-root>/config/appie.json  (Docker volume mount path)

Run with:
    make test-integration
    # or directly:
    pytest -m integration tests/integration/
    # keep the DB for inspection after the run:
    pytest -m integration tests/integration/ --keep-integration-db=/tmp/ah_test.db
"""

import json
import os
import shutil
import subprocess
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).parent.parent.parent
BACKEND_DIR = REPO_ROOT / "backend"
GO_IMAGE = "golang:1.23-alpine"
_xdg_config = Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config"))
_CONFIG_CANDIDATES = [
    Path(os.environ["CONFIG_PATH"]) if "CONFIG_PATH" in os.environ else None,
    _xdg_config / "appie" / "appie.json",
    REPO_ROOT / "config" / "appie.json",
]
CONFIG_PATH = next((p for p in _CONFIG_CANDIDATES if p is not None and p.exists()), _CONFIG_CANDIDATES[1])


def pytest_addoption(parser):
    parser.addoption(
        "--keep-integration-db",
        metavar="PATH",
        default=None,
        help="Copy the integration test DB to PATH after the run for manual inspection.",
    )


def _load_credentials():
    """Return (access_token, refresh_token) or None if no credentials found."""
    access = os.environ.get("AH_ACCESS_TOKEN", "")
    refresh = os.environ.get("AH_REFRESH_TOKEN", "")
    if access:
        return access, refresh

    if CONFIG_PATH.exists():
        try:
            cfg = json.loads(CONFIG_PATH.read_text())
            access = cfg.get("access_token", "")
            refresh = cfg.get("refresh_token", "")
            if access:
                return access, refresh
        except (json.JSONDecodeError, OSError):
            pass

    return None


@pytest.fixture(scope="session")
def ah_credentials():
    creds = _load_credentials()
    if not creds:
        pytest.skip(
            "No AH credentials found. "
            f"Set AH_ACCESS_TOKEN env var or provide credentials at {CONFIG_PATH}."
        )
    return creds


def _resolve_go_runtime():
    """Decide how to build the backend binary: 'local' Go toolchain or 'docker'.

    Mirrors test.sh: respect an explicit GO_RUNTIME, otherwise prefer a local Go
    install (fast, no container) and fall back to Docker.
    """
    runtime = os.environ.get("GO_RUNTIME")
    if runtime in ("local", "docker"):
        return runtime
    if shutil.which("go"):
        return "local"
    if shutil.which("docker"):
        return "docker"
    pytest.fail(
        "Cannot build backend binary: neither Go nor Docker is available. "
        "Install Go (https://go.dev/doc/install) or Docker, or set GO_RUNTIME."
    )


@pytest.fixture(scope="session")
def backend_binary(tmp_path_factory):
    """Build the backend binary once per test session (local Go, or Docker)."""
    bin_path = tmp_path_factory.mktemp("backend_build") / "backend"
    runtime = _resolve_go_runtime()

    if runtime == "local":
        cmd = ["go", "build", "-o", str(bin_path), "."]
    else:
        cmd = [
            "docker",
            "run",
            "--rm",
            "-v",
            f"{BACKEND_DIR}:/workspace/backend:ro",
            "-v",
            f"{bin_path.parent}:/out",
            "-w",
            "/workspace/backend",
            GO_IMAGE,
            "go",
            "build",
            "-o",
            f"/out/{bin_path.name}",
            ".",
        ]

    result = subprocess.run(
        cmd,
        cwd=str(BACKEND_DIR),
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        pytest.fail(f"Failed to build backend binary ({runtime}):\n{result.stderr}")
    return bin_path


@pytest.fixture(scope="session")
def sync_binary(backend_binary):
    """Alias: the backend binary handles sync via --sync-once."""
    return backend_binary


@pytest.fixture(scope="session")
def enrich_binary(backend_binary):
    """Alias: the backend binary handles enrichment via --once."""
    return backend_binary


@pytest.fixture(scope="session")
def populated_db(ah_credentials, sync_binary, tmp_path_factory, request):
    """Run sync against a fresh temp DB and yield the DB path."""
    db_path = tmp_path_factory.mktemp("integration_db") / "test_groceries.db"
    access_token, refresh_token = ah_credentials

    env = os.environ.copy()
    env["DB_PATH"] = str(db_path)
    env["AH_ACCESS_TOKEN"] = access_token
    env["AH_REFRESH_TOKEN"] = refresh_token
    if CONFIG_PATH.exists():
        env["CONFIG_PATH"] = str(CONFIG_PATH)
    env.setdefault("SYNC_MAX_RECEIPTS", "3")
    env.setdefault("SYNC_MAX_ORDERS", "3")
    print("\nRunning sync...", flush=True)
    result = subprocess.run([str(sync_binary), "--sync-once"], env=env, timeout=300)
    if result.returncode != 0:
        pytest.fail(f"Sync binary exited with code {result.returncode}")

    keep_path = request.config.getoption("--keep-integration-db")
    if keep_path:
        import shutil
        shutil.copy2(db_path, keep_path)
        print(f"\nIntegration DB saved to: {keep_path}")

    yield str(db_path)


@pytest.fixture(scope="session")
def enriched_db(populated_db, enrich_binary):
    """Run Go enrichment worker (--once) against the populated DB."""
    env = os.environ.copy()
    env["DB_PATH"] = populated_db
    env["ENRICHMENT_DATA_DIR"] = str(BACKEND_DIR / "data")

    print("\nRunning enrichment (Go worker)...", flush=True)
    result = subprocess.run(
        [str(enrich_binary), "--once"],
        env=env,
        timeout=300,
    )
    if result.returncode != 0:
        pytest.fail(f"Enrichment binary exited with code {result.returncode}")

    return populated_db


@pytest.fixture(scope="session")
def running_backend(enriched_db, backend_binary):
    """Start the backend HTTP server against the enriched DB; yield the base URL."""
    import socket
    import time

    import requests

    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()

    env = os.environ.copy()
    env["DB_PATH"] = enriched_db
    env["ENRICHMENT_DATA_DIR"] = str(BACKEND_DIR / "data")
    env["BACKEND_PORT"] = str(port)

    proc = subprocess.Popen([str(backend_binary)], env=env)
    base_url = f"http://127.0.0.1:{port}"

    for _ in range(30):
        try:
            requests.get(f"{base_url}/receipts", timeout=1)
            break
        except Exception:
            time.sleep(0.5)
    else:
        proc.terminate()
        pytest.fail("Backend HTTP server did not start in time")

    import sys

    os.environ["BACKEND_URL"] = base_url

    # Patch module-level BACKEND_URL in backend_client (read at import time).
    if "backend_client" in sys.modules:
        sys.modules["backend_client"].BACKEND_URL = base_url

    yield base_url

    proc.terminate()
    proc.wait(timeout=10)
    os.environ.pop("BACKEND_URL", None)


@pytest.fixture(scope="session")
def db_conn(populated_db):
    """Read-only SQLite connection to the populated integration DB."""
    import sqlite3
    conn = sqlite3.connect(f"file:{populated_db}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row
    yield conn
    conn.close()


@pytest.fixture(scope="session")
def enriched_db_conn(enriched_db):
    """Read-only SQLite connection to the enriched integration DB."""
    import sqlite3
    conn = sqlite3.connect(f"file:{enriched_db}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row
    yield conn
    conn.close()
