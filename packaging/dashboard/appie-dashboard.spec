# PyInstaller spec for the Streamlit dashboard.
#
# Builds a onedir bundle named "appie-dashboard". Streamlit is notoriously
# tricky to freeze because it loads its own package metadata, static assets,
# and many submodules dynamically; we collect all of those explicitly.
#
# Build (run from the repo root, with the dashboard deps + pyinstaller in the
# active venv):
#
#   pyinstaller --noconfirm --distpath build/dashboard-dist \
#       --workpath build/dashboard-work \
#       packaging/dashboard/appie-dashboard.spec
#
# The result is build/dashboard-dist/appie-dashboard/ — a folder containing the
# executable plus its runtime. The build script zips that folder for the
# launcher to embed.

import os
from PyInstaller.utils.hooks import (
    collect_all,
    collect_data_files,
    copy_metadata,
)

# The dashboard source lives at <repo>/dashboard. SPECPATH is the dir holding
# this spec file (packaging/dashboard), so the repo root is two levels up.
REPO_ROOT = os.path.abspath(os.path.join(SPECPATH, "..", ".."))
DASHBOARD_SRC = os.path.join(REPO_ROOT, "dashboard")

datas = []
binaries = []
hiddenimports = []

# Streamlit: pull in everything (metadata, static assets, submodules).
for pkg in ("streamlit",):
    d, b, h = collect_all(pkg)
    datas += d
    binaries += b
    hiddenimports += h

# Package metadata several libs read at runtime via importlib.metadata.
for pkg in (
    "streamlit",
    "pandas",
    "plotly",
    "pyarrow",
    "altair",
    "numpy",
    "requests",
):
    try:
        datas += copy_metadata(pkg)
    except Exception:
        pass

# Plotly ships JSON/JS assets it loads lazily.
datas += collect_data_files("plotly")

# The dashboard's own source: app.py, modules, and the pages/ directory.
for fname in os.listdir(DASHBOARD_SRC):
    src = os.path.join(DASHBOARD_SRC, fname)
    if fname in ("__pycache__", "Dockerfile", "entrypoint.sh", "requirements.txt"):
        continue
    if os.path.isfile(src) and fname.endswith(".py"):
        datas.append((src, "."))
    elif os.path.isdir(src) and fname == "pages":
        # Place page modules under ./pages next to the executable.
        for page in os.listdir(src):
            if page.endswith(".py"):
                datas.append((os.path.join(src, page), "pages"))

hiddenimports += [
    "streamlit.web.cli",
    "streamlit.runtime.scriptrunner.magic_funcs",
]

block_cipher = None

a = Analysis(
    [os.path.join(SPECPATH, "run_streamlit.py")],
    pathex=[DASHBOARD_SRC],
    binaries=binaries,
    datas=datas,
    hiddenimports=hiddenimports,
    hookspath=[],
    runtime_hooks=[],
    excludes=["tkinter", "matplotlib"],
    cipher=block_cipher,
    noarchive=False,
)

# Drop bundled test suites and C headers that ride along in some wheels (most
# notably pyarrow's tests/ and include/, several MB each). These are never
# imported at runtime. We can't trim pyarrow's shared libraries themselves —
# libarrow.so is linked against libparquet/libarrow_flight/etc., so removing any
# breaks `import pyarrow` — but the non-code extras are safe to drop.
#
# This must run AFTER Analysis: pyarrow is pulled in by PyInstaller's automatic
# dependency discovery (via pandas/streamlit), so its tests/ show up in
# a.datas / a.binaries, not in the lists we assembled by hand above.
def _prune(table):
    kept = []
    for entry in table:
        dest = entry[0].replace(os.sep, "/")
        parts = dest.split("/")
        if "tests" in parts or "test" in parts:
            continue
        if parts[:2] == ["pyarrow", "include"]:
            continue
        kept.append(entry)
    return type(table)(kept)

a.datas = _prune(a.datas)
a.binaries = _prune(a.binaries)

pyz = PYZ(a.pure, a.zipped_data, cipher=block_cipher)

exe = EXE(
    pyz,
    a.scripts,
    [],
    exclude_binaries=True,
    name="appie-dashboard",
    debug=False,
    bootloader_ignore_signals=False,
    # Strip debug symbols from bundled binaries — a sizeable, low-risk win
    # across the many .so files (libarrow alone drops ~11 MB). Requires `strip`
    # (binutils) on PATH, which the build environments provide. UPX is left off:
    # it trips macOS Gatekeeper and some Windows antivirus, which is the wrong
    # trade-off for "hand it to a friend".
    strip=True,
    upx=False,
    console=True,
)

coll = COLLECT(
    exe,
    a.binaries,
    a.zipfiles,
    a.datas,
    strip=True,
    upx=False,
    name="appie-dashboard",
)
