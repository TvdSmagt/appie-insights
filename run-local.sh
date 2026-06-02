#!/usr/bin/env bash
# Run the app locally with just Go and Python — no Docker.
#
# Starts the Go backend and the Streamlit dashboard as native processes.
# Data lands in ./data/groceries.db and the AH token in ~/.config/appie/appie.json
# (the same defaults the backend uses when run directly). Both processes are
# stopped on exit (Ctrl-C).
set -euo pipefail

cd "$(dirname "$0")"

# --- Prerequisites -----------------------------------------------------------
if ! command -v go &>/dev/null; then
  echo "Error: Go is not installed. See https://go.dev/dl/ (need 1.23+)."
  exit 1
fi
if ! command -v python3 &>/dev/null; then
  echo "Error: python3 is not installed (need 3.12+)."
  exit 1
fi

# --- Ports -------------------------------------------------------------------
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

# --- Version stamp (matches run.sh) ------------------------------------------
if VERSION=$(git describe --tags --dirty 2>/dev/null); then
  :
elif COMMIT=$(git rev-parse --short HEAD 2>/dev/null); then
  VERSION="prerelease+$COMMIT"
else
  VERSION="development"
fi

echo "Backend API: port $BACKEND_PORT"
echo "Dashboard:   http://localhost:$DASHBOARD_PORT"
echo "Version:     $VERSION"

# --- Python virtual environment for the dashboard ----------------------------
DASH_VENV=".venv-dashboard"
if [ ! -x "$DASH_VENV/bin/streamlit" ]; then
  echo "Setting up dashboard virtualenv in $DASH_VENV ..."
  python3 -m venv "$DASH_VENV"
  "$DASH_VENV/bin/pip" install --quiet --upgrade pip
  "$DASH_VENV/bin/pip" install --quiet -r dashboard/requirements.txt
fi

# Skip Streamlit's interactive "enter your email" onboarding prompt, which would
# otherwise block the first run.
export STREAMLIT_BROWSER_GATHER_USAGE_STATS=false
mkdir -p "$HOME/.streamlit"
[ -f "$HOME/.streamlit/credentials.toml" ] || printf '[general]\nemail = ""\n' > "$HOME/.streamlit/credentials.toml"

# --- Build backend -----------------------------------------------------------
# Build to a real binary (rather than `go run`) so the started process is the
# server itself — SIGINT from the cleanup trap then reaches it directly instead
# of an intermediate `go run` wrapper that may orphan its child.
echo "Building backend ..."
(cd backend && go build -ldflags "-X main.Version=$VERSION" -o backend .)

# --- Start backend -----------------------------------------------------------
# Defaults (DB_PATH, ENRICHMENT_DATA_DIR, CONFIG_PATH) are already local-friendly;
# we only override the port.
echo "Starting backend ..."
(
  cd backend
  BACKEND_PORT="$BACKEND_PORT" exec ./backend
) &
BACKEND_PID=$!

# --- Start dashboard ---------------------------------------------------------
echo "Starting dashboard ..."
(
  cd dashboard
  PYTHONPATH="$PWD" "../$DASH_VENV/bin/python" -c "from theme import init_theme; init_theme()"
  BACKEND_URL="http://localhost:$BACKEND_PORT" \
    "../$DASH_VENV/bin/streamlit" run app.py \
    --server.address=localhost \
    --server.port="$DASHBOARD_PORT"
) &
DASHBOARD_PID=$!

# Stop both processes (and their children) on exit.
cleanup() {
  trap - EXIT INT TERM
  echo
  echo "Stopping ..."
  kill "$BACKEND_PID" "$DASHBOARD_PID" 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Open the dashboard in the default browser once it's likely up.
sleep 2
xdg-open "http://localhost:$DASHBOARD_PORT" 2>/dev/null || open "http://localhost:$DASHBOARD_PORT" 2>/dev/null || true

# Wait for either process; if one dies, exit (cleanup stops the other).
wait -n "$BACKEND_PID" "$DASHBOARD_PID"
