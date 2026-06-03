#!/usr/bin/env bash
# Smoke-test a packaged appie-insights executable: actually launch it headless,
# wait until the bundled backend + dashboard come up and the dashboard answers
# HTTP, then shut it down. This proves the single-file binary really runs on
# the target OS — not just that it was produced.
#
# Usage: smoke-test.sh /path/to/appie-insights[.exe]
#
# Exit status: 0 if the dashboard became reachable, non-zero otherwise.
set -euo pipefail

BIN="${1:?usage: smoke-test.sh <path-to-binary>}"
TIMEOUT="${SMOKE_TIMEOUT:-120}" # seconds to wait for "ready"

if [ ! -x "$BIN" ]; then
  # On Windows the bit isn't meaningful; only hard-fail on a truly missing file.
  if [ ! -f "$BIN" ]; then
    echo "ERROR: binary not found: $BIN" >&2
    exit 1
  fi
fi

LOG="$(mktemp)"
cleanup() {
  # Tear down the launcher and its backend/dashboard children. On Unix the
  # launcher's children share its process group, and signalling the launcher
  # triggers its own clean shutdown. On Windows (Git Bash) a bash `kill` to a
  # native PID is unreliable, so prefer taskkill /T to take down the tree.
  if [ -n "${PID:-}" ]; then
    if command -v taskkill >/dev/null 2>&1; then
      # taskkill reaps the native process tree out-of-band; a bash `wait` on
      # the PID afterwards can block forever in Git Bash, so don't wait here.
      taskkill //T //F //PID "$PID" >/dev/null 2>&1 || true
    elif kill -0 "$PID" 2>/dev/null; then
      kill "$PID" 2>/dev/null || true
      wait "$PID" 2>/dev/null || true
    fi
  fi
  rm -f "$LOG"
}
trap cleanup EXIT

echo "==> launching $BIN (headless, no browser) ..."
APPIE_NO_BROWSER=1 "$BIN" >"$LOG" 2>&1 &
PID=$!

# The launcher logs `ready — your dashboard is at http://127.0.0.1:PORT` once
# both children are up. Poll the log for that line (or an early exit).
URL=""
deadline=$(( $(date +%s) + TIMEOUT ))
while [ "$(date +%s)" -lt "$deadline" ]; do
  if ! kill -0 "$PID" 2>/dev/null; then
    echo "ERROR: launcher exited before becoming ready. Output:" >&2
    cat "$LOG" >&2
    exit 1
  fi
  URL="$(grep -oE 'dashboard is at (http://[0-9.]+:[0-9]+)' "$LOG" | grep -oE 'http://[0-9.]+:[0-9]+' | head -n1 || true)"
  [ -n "$URL" ] && break
  sleep 1
done

if [ -z "$URL" ]; then
  echo "ERROR: launcher did not report a ready dashboard within ${TIMEOUT}s. Output:" >&2
  cat "$LOG" >&2
  exit 1
fi

echo "==> dashboard reported at $URL — probing ..."
# Give the HTTP server a moment, then confirm it answers with a success status.
for _ in $(seq 1 30); do
  code="$(curl -s -o /dev/null -w '%{http_code}' "$URL" || true)"
  if [ "$code" = "200" ]; then
    echo "==> OK: dashboard answered HTTP 200"
    echo "----- launcher log -----"
    cat "$LOG"
    exit 0
  fi
  sleep 1
done

echo "ERROR: dashboard at $URL did not answer HTTP 200 (last: ${code:-none}). Output:" >&2
cat "$LOG" >&2
exit 1
