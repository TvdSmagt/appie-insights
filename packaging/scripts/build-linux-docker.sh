#!/usr/bin/env bash
# Build the self-contained Linux executable inside a clean Docker container,
# so you don't need Go, Python, or PyInstaller installed on the host.
#
# The container runs packaging/scripts/build.sh against a bind-mount of the
# repo and writes the result to ./dist/appie-insights on the host.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
IMAGE="appie-insights-builder:linux"

cd "$REPO_ROOT"

echo "==> Building builder image ($IMAGE) ..."
docker build -f packaging/Dockerfile.build-linux -t "$IMAGE" .

echo "==> Running build inside container ..."
# Bind-mount the repo so build artifacts land back on the host. Run as the host
# user so dist/ files aren't owned by root. HOME=/tmp gives that user a writable
# home for the Go build cache (the pip deps are already baked into the image's
# venv at /opt/pkgvenv).
docker run --rm \
  --user "$(id -u):$(id -g)" \
  -v "$REPO_ROOT":/src \
  -e HOME=/tmp \
  -e GOFLAGS=-buildvcs=false \
  "$IMAGE"

echo
echo "==> Done. Executable: $REPO_ROOT/dist/appie-insights"
