# Embedded artifacts

The launcher embeds this whole directory via `//go:embed embedded` (see
`../embedded.go`) and unpacks two artifacts from it at run time:

- `backend.zip` — the per-platform Go backend binary (`appie-backend[.exe]`),
  zipped at the archive root.
- `dashboard.zip` — the PyInstaller onedir freeze of the Streamlit dashboard
  (the *contents* of `appie-dashboard/`, so `appie-dashboard[.exe]` sits at the
  archive root).

These zips are **generated at build time and not committed** (`*.zip` is
git-ignored). `packaging/scripts/build.sh` (and `build-windows.ps1`) create
them here before compiling the launcher.

The only committed file is `.gitkeep`, which keeps this directory present on a
fresh clone. Because we embed the *directory* (not the individual zips by name),
the launcher compiles and `go vet`/`go build` pass even with no zips present —
a missing artifact becomes a clear runtime error ("built without its
artifacts; build it via packaging/scripts/build.sh") rather than a compile
error. Every real build populates the zips first, so that path is unaffected.
