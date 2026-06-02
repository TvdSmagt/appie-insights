# Packaging — single-file executables

This directory builds **one double-clickable executable per OS** that bundles
both halves of the app (the Go backend and the Streamlit dashboard) so people
who don't clone repos can just run it.

```
appie-insights        (Linux)
appie-insights         (macOS)
appie-insights.exe    (Windows)
```

A friend downloads the file for their OS and double-clicks it. No Python, no Go,
no Docker, no install. On first launch it unpacks itself to a per-user app
directory, starts both services on free local ports, and opens the dashboard in
the browser.

## How it works

The shipped executable is a small **Go launcher** (`launcher/`) that embeds two
artifacts via `go:embed`:

1. **`backend.zip`** — the Go backend, compiled for the target OS. The
   enrichment CSVs are baked into the backend binary itself (`backend/embed.go`),
   so no data files travel alongside it.
2. **`dashboard.zip`** — the Streamlit dashboard, frozen with **PyInstaller**
   (`dashboard/appie-dashboard.spec`) into a self-contained runtime that needs
   no system Python.

At run time the launcher (`launcher/main.go`):

1. extracts both artifacts to `~/<config>/appie-insights/runtime/<version>/`
   (once per version),
2. picks two free `127.0.0.1` ports,
3. starts the backend and waits for `/version` to answer,
4. starts the dashboard pointed at the backend (`BACKEND_URL`),
5. opens the browser, and
6. stops both children on Ctrl-C / window close.

User data (the SQLite DB, the AH token, settings) lives in
`~/<config>/appie-insights/data/` so it survives version upgrades. On Linux/mac
that's `~/.config/appie-insights/`; on Windows it's `%AppData%\appie-insights\`.

## Building

> **PyInstaller cannot cross-compile.** Each OS's executable must be built **on
> that OS** (or in CI on that OS's runner). Go cross-compiles fine, but the
> frozen Python dashboard does not. So you get a Windows `.exe` only by building
> on Windows, a macOS binary only on macOS, etc.

### Linux — via Docker (no local toolchain needed)

```bash
./packaging/scripts/build-linux-docker.sh
```

Builds a clean builder image and runs the build inside it, writing
`dist/appie-insights` back to the host (owned by you, not root). This is the
recommended Linux path — you don't need Go, Python, or PyInstaller installed.

### Any OS — directly (Linux / macOS / Windows-Git-Bash)

Needs Go 1.23+, Python 3.12+, and PyInstaller on `PATH`:

```bash
python3 -m venv .venv-package
. .venv-package/bin/activate          # Windows: .venv-package\Scripts\activate
pip install -r dashboard/requirements.txt pyinstaller
./packaging/scripts/build.sh          # Windows: bash packaging/scripts/build.sh (Git Bash)
```

Produces `dist/appie-insights` (or `dist/appie-insights.exe` on Windows).

### Windows — without Git Bash

Use `packaging/scripts/build-windows.ps1` from PowerShell; it does the same
steps natively and produces `dist\appie-insights.exe`.

### All platforms at once — GitHub Actions

`.github/workflows/package.yml` builds Linux, macOS (Apple Silicon), and Windows
executables on native runners and uploads them as workflow artifacts. Tagging a
release (`v*`) additionally attaches them to the GitHub Release, so friends can
download from the Releases page. Trigger it manually from the Actions tab
("Run workflow") or by pushing a tag.

## Notes & limitations

- **Size**: the executables are large (~150–180 MB) because they embed a full
  Python runtime plus pandas/plotly/pyarrow. That's the cost of "no install".
- **macOS Gatekeeper**: unsigned binaries trigger a security warning. A friend
  must right-click → Open the first time (or you sign/notarize it). Apple
  Silicon vs Intel are separate builds; the CI matrix builds Apple Silicon.
- **Windows SmartScreen**: an unsigned `.exe` shows a "Windows protected your
  PC" prompt; click *More info → Run anyway*.
- **First launch is slower** (unpacking); subsequent launches reuse the
  extracted runtime.
- **AH login** still happens in the dashboard exactly as before; the OAuth
  callback works because everything runs on localhost.
