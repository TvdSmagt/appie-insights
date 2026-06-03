#!/usr/bin/env bash
# Run the app, picking the best available execution mode automatically.
#
# Mode selection (default, "auto"):
#   1. If a compatible Go (1.23+) and Python (3.12+) toolchain is present, run
#      locally as native processes — no Docker needed.
#   2. Otherwise, if Docker is installed and its daemon is running, run via
#      Docker Compose.
#   3. Otherwise, tell the user to install one of the two.
#
# Override the auto-detection with --local or --docker.
set -euo pipefail

cd "$(dirname "$0")"

# --- Argument parsing --------------------------------------------------------
MODE="auto"
usage() {
  cat <<'EOF'
Usage: ./run.sh [--local | --docker]

  --local    Force native Go + Python execution (no Docker).
  --docker   Force Docker Compose execution.
  (default)  Auto-detect: use local if Go 1.23+ and Python 3.12+ are present,
             otherwise fall back to Docker.
EOF
}
while [ $# -gt 0 ]; do
  case "$1" in
    --local)  MODE="local" ;;
    --docker) MODE="docker" ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Error: unknown argument '$1'" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

# --- Capability detection ----------------------------------------------------
# Return success if a compatible Go and Python toolchain are available.
local_available() {
  command -v go &>/dev/null || return 1
  command -v python3 &>/dev/null || return 1
  # Go 1.23+
  local gover
  gover=$(go env GOVERSION 2>/dev/null | sed 's/^go//')
  version_ge "$gover" "1.23" || return 1
  # Python 3.12+
  local pyver
  pyver=$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")' 2>/dev/null)
  version_ge "$pyver" "3.12" || return 1
  return 0
}

# Return success if Docker is installed and its daemon is running.
docker_available() {
  command -v docker &>/dev/null || return 1
  docker info &>/dev/null || return 1
  return 0
}

# version_ge A B  ->  success if version A >= version B (dotted numeric compare).
version_ge() {
  [ -n "$1" ] || return 1
  [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

# --- Resolve mode ------------------------------------------------------------
if [ "$MODE" = "auto" ]; then
  if local_available; then
    MODE="local"
  elif docker_available; then
    MODE="docker"
  else
    cat >&2 <<'EOF'
Error: no way to run the app was found.

Install one of:
  - Go 1.23+ and Python 3.12+  (to run natively), or
  - Docker                     (https://docs.docker.com/get-docker/)
EOF
    exit 1
  fi
elif [ "$MODE" = "local" ]; then
  if ! local_available; then
    echo "Error: --local requested but a compatible toolchain is missing." >&2
    command -v go &>/dev/null || echo "  - Go is not installed (need 1.23+). See https://go.dev/dl/" >&2
    command -v python3 &>/dev/null || echo "  - python3 is not installed (need 3.12+)." >&2
    if command -v go &>/dev/null; then
      version_ge "$(go env GOVERSION 2>/dev/null | sed 's/^go//')" "1.23" || \
        echo "  - Go is too old (need 1.23+); found $(go env GOVERSION 2>/dev/null)." >&2
    fi
    if command -v python3 &>/dev/null; then
      version_ge "$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")' 2>/dev/null)" "3.12" || \
        echo "  - python3 is too old (need 3.12+); found $(python3 --version 2>/dev/null)." >&2
    fi
    exit 1
  fi
elif [ "$MODE" = "docker" ]; then
  if ! docker_available; then
    if ! command -v docker &>/dev/null; then
      echo "Error: Docker is not installed. See https://docs.docker.com/get-docker/" >&2
    else
      echo "Error: Docker daemon is not running." >&2
    fi
    exit 1
  fi
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

# --- Version stamp -----------------------------------------------------------
# Prefer the git tag (e.g. v1.0.0); before the first tag exists, fall back to a
# traceable prerelease label; if git is unavailable, use "development".
if VERSION=$(git describe --tags --dirty 2>/dev/null); then
  :
elif COMMIT=$(git rev-parse --short HEAD 2>/dev/null); then
  VERSION="prerelease+$COMMIT"
else
  VERSION="development"
fi

echo "Mode:        $MODE"
echo "Backend API: port $BACKEND_PORT"
echo "Dashboard:   http://localhost:$DASHBOARD_PORT"
echo "Version:     $VERSION"

# --- Docker mode -------------------------------------------------------------
run_docker() {
  export BACKEND_PORT DASHBOARD_PORT VERSION

  # Build images (if changed), then start all services in the foreground.
  # On exit (Ctrl-C or termination), stop all services.
  trap 'docker compose down' EXIT

  docker compose build
  docker compose up &
  local compose_pid=$!

  # Open the dashboard in the default browser. xdg-open is Linux, open is macOS.
  sleep 2
  xdg-open "http://localhost:$DASHBOARD_PORT" 2>/dev/null || open "http://localhost:$DASHBOARD_PORT" 2>/dev/null || true

  wait "$compose_pid"
}

# --- Local mode --------------------------------------------------------------
# Starts the Go backend and the Streamlit dashboard as native processes. Data
# lands in ./data/groceries.db and the AH token in ~/.config/appie/appie.json
# (the same defaults the backend uses when run directly). Both processes are
# stopped on exit (Ctrl-C).
run_local() {
  # Python virtual environment for the dashboard.
  local dash_venv=".venv-dashboard"
  if [ ! -x "$dash_venv/bin/streamlit" ]; then
    echo "Setting up dashboard virtualenv in $dash_venv ..."
    python3 -m venv "$dash_venv"
    "$dash_venv/bin/pip" install --quiet --upgrade pip
    "$dash_venv/bin/pip" install --quiet -r dashboard/requirements.txt
  fi

  # Skip Streamlit's interactive "enter your email" onboarding prompt, which
  # would otherwise block the first run.
  export STREAMLIT_BROWSER_GATHER_USAGE_STATS=false
  mkdir -p "$HOME/.streamlit"
  [ -f "$HOME/.streamlit/credentials.toml" ] || printf '[general]\nemail = ""\n' > "$HOME/.streamlit/credentials.toml"

  # Build to a real binary (rather than `go run`) so the started process is the
  # server itself — SIGINT from the cleanup trap then reaches it directly
  # instead of an intermediate `go run` wrapper that may orphan its child.
  echo "Building backend ..."
  (cd backend && go build -ldflags "-X main.Version=$VERSION" -o backend .)

  # The backend runs from inside backend/ (so SIGINT from the cleanup trap
  # reaches the binary directly). That changes the working directory, so the
  # relative DB_PATH / ENRICHMENT_DATA_DIR defaults would resolve wrongly (e.g.
  # the enrichment data dir would become backend/backend/data and enrichment
  # would fail, leaving CO₂eq empty). Pin them to absolute repo paths instead.
  local repo_root="$PWD"
  echo "Starting backend ..."
  (
    cd backend
    BACKEND_PORT="$BACKEND_PORT" \
    DB_PATH="$repo_root/data/groceries.db" \
    ENRICHMENT_DATA_DIR="$repo_root/backend/data" \
      exec ./backend
  ) &
  local backend_pid=$!

  echo "Starting dashboard ..."
  (
    cd dashboard
    PYTHONPATH="$PWD" "../$dash_venv/bin/python" -c "from theme import init_theme; init_theme()"
    BACKEND_URL="http://localhost:$BACKEND_PORT" \
      "../$dash_venv/bin/streamlit" run app.py \
      --server.address=localhost \
      --server.port="$DASHBOARD_PORT"
  ) &
  local dashboard_pid=$!

  # Stop both processes (and their children) on exit.
  cleanup() {
    trap - EXIT INT TERM
    echo
    echo "Stopping ..."
    kill "$backend_pid" "$dashboard_pid" 2>/dev/null || true
    wait 2>/dev/null || true
  }
  trap cleanup EXIT INT TERM

  # Streamlit opens the browser itself on startup, so we don't open one here
  # (doing both spawned two tabs).

  # Wait for either process; if one dies, exit (cleanup stops the other).
  wait -n "$backend_pid" "$dashboard_pid"
}

if [ "$MODE" = "docker" ]; then
  run_docker
else
  run_local
fi
