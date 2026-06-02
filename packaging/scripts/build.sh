#!/usr/bin/env bash
# Build a single, self-contained appie-insights executable for the HOST
# platform.
#
# PyInstaller cannot cross-compile, so this produces a binary only for the OS
# and architecture it runs on:
#   - run it on Linux  -> Linux executable
#   - run it on macOS  -> macOS executable (matching the host's arch)
#   - run it on Windows (Git Bash) -> appie-insights.exe
#
# For Linux you can instead use packaging/scripts/build-linux-docker.sh, which
# runs this inside a clean container. For Windows/macOS, run this on the
# respective OS (or use the GitHub Actions workflow).
#
# Output: dist/appie-insights[.exe]
set -euo pipefail

# --- Locate paths ------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LAUNCHER_DIR="$REPO_ROOT/packaging/launcher"
DASH_SPEC="$REPO_ROOT/packaging/dashboard/appie-dashboard.spec"
BUILD_DIR="$REPO_ROOT/build/packaging"
DIST_DIR="$REPO_ROOT/dist"

cd "$REPO_ROOT"

# --- Version stamp (matches run.sh / run-local.sh) ---------------------------
if VERSION=$(git describe --tags --dirty 2>/dev/null); then :;
elif COMMIT=$(git rev-parse --short HEAD 2>/dev/null); then VERSION="prerelease+$COMMIT";
else VERSION="development"; fi

# --- Platform-specific names -------------------------------------------------
GOOS="$(go env GOOS)"
EXE_EXT=""; BIN_EXT=""
if [ "$GOOS" = "windows" ]; then EXE_EXT=".exe"; BIN_EXT=".exe"; fi

echo "==> appie-insights packaging build"
echo "    version : $VERSION"
echo "    platform: $GOOS/$(go env GOARCH)"
echo

rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR" "$DIST_DIR" "$LAUNCHER_DIR/embedded"

# Note: the launcher embeds the whole embedded/ directory (//go:embed embedded),
# which always compiles thanks to the committed .gitkeep — no placeholder zips
# needed. Steps 1–2 below write the real backend.zip / dashboard.zip into that
# directory before the launcher is compiled in step 3.

# --- 1. Build the Go backend -------------------------------------------------
echo "==> [1/4] Building Go backend ..."
( cd "$REPO_ROOT/backend" && \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false \
      -ldflags "-s -w -X main.Version=$VERSION" \
      -o "$BUILD_DIR/backend/appie-backend$BIN_EXT" . )

# Zip the backend binary for embedding (entry name matches the launcher's
# expectation: appie-backend[.exe] at the zip root).
( cd "$BUILD_DIR/backend" && \
    rm -f "$LAUNCHER_DIR/embedded/backend.zip" && \
    python3 -c "import zipfile,sys; \
zipfile.ZipFile(sys.argv[1],'w',zipfile.ZIP_DEFLATED).write(sys.argv[2], arcname=sys.argv[2])" \
      "$LAUNCHER_DIR/embedded/backend.zip" "appie-backend$BIN_EXT" )
echo "    backend.zip ready"

# --- 2. Freeze the Streamlit dashboard with PyInstaller ----------------------
echo "==> [2/4] Freezing dashboard (PyInstaller) ..."
command -v pyinstaller >/dev/null 2>&1 || {
  echo "ERROR: pyinstaller not found on PATH." >&2
  echo "Install build deps first, e.g.:" >&2
  echo "  python3 -m venv .venv-package && . .venv-package/bin/activate" >&2
  echo "  pip install -r dashboard/requirements.txt pyinstaller" >&2
  exit 1
}

pyinstaller --noconfirm \
  --distpath "$BUILD_DIR/dashboard-dist" \
  --workpath "$BUILD_DIR/dashboard-work" \
  "$DASH_SPEC"

# PyInstaller onedir output is build/.../dashboard-dist/appie-dashboard/.
# Zip the *contents* so they extract to dashboard/ at the launcher's runtime
# root (launcher expects dashboard/appie-dashboard[.exe]).
echo "    zipping frozen dashboard ..."
( cd "$BUILD_DIR/dashboard-dist/appie-dashboard" && \
    rm -f "$LAUNCHER_DIR/embedded/dashboard.zip" && \
    python3 -c "import zipfile,os,sys; \
z=zipfile.ZipFile(sys.argv[1],'w',zipfile.ZIP_DEFLATED); \
[z.write(os.path.join(r,f), os.path.relpath(os.path.join(r,f),'.')) \
 for r,_,fs in os.walk('.') for f in fs]; z.close()" \
      "$LAUNCHER_DIR/embedded/dashboard.zip" )
echo "    dashboard.zip ready"

# --- 3. Build the launcher with both artifacts embedded ----------------------
echo "==> [3/4] Building launcher (embeds backend + dashboard) ..."
( cd "$LAUNCHER_DIR" && \
    go build -trimpath -buildvcs=false \
      -ldflags "-s -w -X main.Version=$VERSION" \
      -o "$DIST_DIR/appie-insights$EXE_EXT" . )

# --- 4. Done -----------------------------------------------------------------
# The embedded zips are now baked into the launcher binary. They are left in
# place under packaging/launcher/embedded/ but are git-ignored (*.zip), so
# there is nothing to clean up or accidentally commit.
echo "==> [4/4] Done."
echo
ls -lh "$DIST_DIR/appie-insights$EXE_EXT"
echo
echo "Single-file executable: $DIST_DIR/appie-insights$EXE_EXT"
echo "Hand this one file to a friend on the same OS — no install required."
