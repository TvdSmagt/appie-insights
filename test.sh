#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

TARGET="${1:-fast}"

# All targets run the Go checks (every make test-* target depends on go-test).
# These can run against a local Go toolchain or inside the backend Docker image.
# Prefer local Go when available (faster, no image build); otherwise fall back to
# Docker. An explicit GO_RUNTIME in the environment is respected as an override.
if [[ -z "${GO_RUNTIME:-}" ]]; then
  if command -v go &>/dev/null; then
    GO_RUNTIME=local
  elif command -v docker &>/dev/null; then
    GO_RUNTIME=docker
    echo "Go not found on PATH; running Go checks in Docker."
  else
    echo "Error: neither Go nor Docker is available. Install Go (https://go.dev/doc/install) or Docker to run the backend tests."
    exit 1
  fi
fi
export GO_RUNTIME

# Python 3 and make are required for the Python test suite and the shared .venv setup.
if ! command -v python3 &>/dev/null; then
  echo "Error: Python 3 is not installed."
  exit 1
fi
if ! command -v make &>/dev/null; then
  echo "Error: make is not installed."
  exit 1
fi

# For integration tests, warn if credentials are missing before make builds the
# local .venv and then fails inside pytest.
if [[ "$TARGET" == "integration" || "$TARGET" == "all" ]]; then
  if [[ ! -f config/appie.json ]] && [[ -z "${AH_ACCESS_TOKEN:-}" ]]; then
    echo "Error: Integration tests require AH credentials. Set AH_ACCESS_TOKEN or log in via the dashboard first (config/appie.json)."
    exit 1
  fi
fi

case "$TARGET" in
  fast)        make test-fast ;;
  unit)        make test-unit ;;
  # Extra args are forwarded — e.g. ./test.sh integration KEEP_DB=/tmp/ah.db
  integration) shift; make test-integration "$@" ;;
  all)         make test-all ;;
  *)
    echo "Usage: $0 [fast|unit|integration|all]"
    echo "  fast         Unit tests, no model download (default)"
    echo "  unit         All unit tests"
    echo "  integration  Integration tests (requires AH credentials)"
    echo "  all          Unit + integration"
    exit 1
    ;;
esac
