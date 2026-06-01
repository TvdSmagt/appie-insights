#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

# Require Docker to be installed and the daemon to be running.
if ! command -v docker &>/dev/null; then
  echo "Error: Docker is not installed. See https://docs.docker.com/get-docker/"
  exit 1
fi
if ! docker info &>/dev/null; then
  echo "Error: Docker daemon is not running."
  exit 1
fi

# Find the first free port at or above the given starting port.
port_in_use() {
  (echo >/dev/tcp/localhost/"$1") 2>/dev/null
}
find_free_port() {
  local port=$1
  while port_in_use "$port"; do
    port=$((port + 1))
  done
  echo "$port"
}

BACKEND_PORT=$(find_free_port 8001)
DASHBOARD_PORT=$(find_free_port 8501)
export BACKEND_PORT DASHBOARD_PORT

echo "Backend API: port $BACKEND_PORT"
echo "Dashboard:   http://localhost:$DASHBOARD_PORT"

# Stamp the build with a version. Prefer the git tag (e.g. v1.0.0); before the
# first tag exists, fall back to a traceable prerelease label; if git is
# unavailable, use "development".
if VERSION=$(git describe --tags --dirty 2>/dev/null); then
  :
elif COMMIT=$(git rev-parse --short HEAD 2>/dev/null); then
  VERSION="prerelease+$COMMIT"
else
  VERSION="development"
fi
export VERSION

echo "Version:     $VERSION"

# Build images (if changed), then start all services in the foreground.
# On exit (Ctrl-C or termination), stop all services.
trap 'docker compose down' EXIT

docker compose build

docker compose up &
COMPOSE_PID=$!

# Open the dashboard in the default browser. xdg-open is Linux, open is macOS.
sleep 2
xdg-open "http://localhost:$DASHBOARD_PORT" 2>/dev/null || open "http://localhost:$DASHBOARD_PORT" 2>/dev/null || true

wait "$COMPOSE_PID"
