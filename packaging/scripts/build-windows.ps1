# Build the self-contained Windows executable (dist\appie-insights.exe).
#
# Prerequisites on the host: Go 1.23+, Python 3.12+, and PyInstaller.
# Recommended setup:
#   py -3.12 -m venv .venv-package
#   .venv-package\Scripts\Activate.ps1
#   pip install -r dashboard\requirements.txt pyinstaller
#
# Then from the repo root:
#   .\packaging\scripts\build-windows.ps1
#
# PyInstaller cannot cross-compile, so this only produces a Windows binary when
# run on Windows.
$ErrorActionPreference = "Stop"

$ScriptDir   = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot    = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$LauncherDir = Join-Path $RepoRoot "packaging\launcher"
$DashSpec    = Join-Path $RepoRoot "packaging\dashboard\appie-dashboard.spec"
$BuildDir    = Join-Path $RepoRoot "build\packaging"
$DistDir     = Join-Path $RepoRoot "dist"
$EmbeddedDir = Join-Path $LauncherDir "embedded"

Set-Location $RepoRoot

# --- Version stamp ----------------------------------------------------------
# Prefer the exact tag when HEAD is tagged (release builds): this avoids a
# spurious "-dirty" suffix from CI checkouts where line-ending normalization
# marks tracked files as modified. Fall back to the dirty-aware describe for
# untagged/local builds.
$Version = (git describe --tags --exact-match 2>$null)
if (-not $Version) { $Version = (git describe --tags --dirty 2>$null) }
if (-not $Version) {
    $Commit = (git rev-parse --short HEAD 2>$null)
    if ($Commit) { $Version = "prerelease+$Commit" } else { $Version = "development" }
}

Write-Host "==> appie-insights packaging build (windows)"
Write-Host "    version : $Version"

Remove-Item -Recurse -Force $BuildDir -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $BuildDir, $DistDir, $EmbeddedDir | Out-Null

# Note: the launcher embeds the whole embedded\ directory (//go:embed embedded),
# which always compiles thanks to the committed .gitkeep -- no placeholder zips
# needed. Steps 1-2 below write the real backend.zip / dashboard.zip into that
# directory before the launcher is compiled in step 3.

# --- 1. Go backend ----------------------------------------------------------
Write-Host "==> [1/4] Building Go backend ..."
$env:CGO_ENABLED = "0"
Push-Location (Join-Path $RepoRoot "backend")
go build -trimpath -buildvcs=false `
    -ldflags "-s -w -X main.Version=$Version" `
    -o (Join-Path $BuildDir "backend\appie-backend.exe") .
Pop-Location

# Zip the backend exe at archive root.
$BackendZip = Join-Path $EmbeddedDir "backend.zip"
Remove-Item -Force $BackendZip -ErrorAction SilentlyContinue
Compress-Archive -Path (Join-Path $BuildDir "backend\appie-backend.exe") `
    -DestinationPath $BackendZip
Write-Host "    backend.zip ready"

# --- 2. Freeze dashboard ----------------------------------------------------
Write-Host "==> [2/4] Freezing dashboard (PyInstaller) ..."
pyinstaller --noconfirm `
    --distpath (Join-Path $BuildDir "dashboard-dist") `
    --workpath (Join-Path $BuildDir "dashboard-work") `
    $DashSpec

# Zip the *contents* of the onedir output so appie-dashboard.exe is at root.
$DashRoot = Join-Path $BuildDir "dashboard-dist\appie-dashboard"
$DashZip  = Join-Path $EmbeddedDir "dashboard.zip"
Remove-Item -Force $DashZip -ErrorAction SilentlyContinue
Compress-Archive -Path (Join-Path $DashRoot "*") -DestinationPath $DashZip
Write-Host "    dashboard.zip ready"

# --- 3. Build launcher ------------------------------------------------------
Write-Host "==> [3/4] Building launcher ..."
Push-Location $LauncherDir
go build -trimpath -buildvcs=false `
    -ldflags "-s -w -X main.Version=$Version" `
    -o (Join-Path $DistDir "appie-insights.exe") .
Pop-Location

Write-Host "==> [4/4] Done."
Write-Host ""
Write-Host "Single-file executable: $(Join-Path $DistDir 'appie-insights.exe')"
