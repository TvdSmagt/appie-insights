// Command appie-insights is a self-contained launcher that bundles the Go
// backend and the (PyInstaller-frozen) Streamlit dashboard into a single
// double-clickable executable.
//
// At build time, the per-platform backend binary and a zipped, frozen copy of
// the dashboard are placed under ./embedded and baked in with go:embed (see
// embedded.go). At run time the launcher:
//
//  1. extracts both artifacts to a per-user cache dir (once per version),
//  2. picks free localhost ports,
//  3. starts the backend, waits for it to answer,
//  4. starts the dashboard pointed at the backend,
//  5. opens the browser, and
//  6. tears both children down on Ctrl-C / window close.
//
// It is intentionally dependency-free (stdlib only) so it builds anywhere the
// Go toolchain runs, including cross-compiles for Windows and macOS.
package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Version is stamped at build time via -ldflags "-X main.Version=...". It keys
// the extraction cache so a new build re-extracts rather than reusing stale
// artifacts.
var Version = "development"

func main() {
	log.SetFlags(0)
	log.SetPrefix("[appie] ")

	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		// Keep the window open on Windows so double-click users can read the
		// error before the console closes.
		if runtime.GOOS == "windows" {
			fmt.Fprintln(os.Stderr, "\nPress Enter to close...")
			fmt.Scanln()
		}
		os.Exit(1)
	}
}

func run() error {
	appDir, err := appDataDir()
	if err != nil {
		return fmt.Errorf("locate app data dir: %w", err)
	}
	root := filepath.Join(appDir, "runtime", Version)
	log.Printf("version %s", Version)
	log.Printf("runtime dir: %s", root)

	backendExe, dashExe, err := extractArtifacts(root)
	if err != nil {
		return fmt.Errorf("extract bundled artifacts: %w", err)
	}

	backendPort, err := freePort()
	if err != nil {
		return err
	}
	dashPort, err := freePort()
	if err != nil {
		return err
	}

	// Per-user data lives outside the (version-keyed, disposable) runtime dir
	// so it survives upgrades. Mirrors the backend's own defaults.
	dataDir := filepath.Join(appDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	backendURL := fmt.Sprintf("http://127.0.0.1:%d", backendPort)

	// --- Backend ------------------------------------------------------------
	backend := exec.CommandContext(ctx, backendExe)
	backend.Env = append(os.Environ(),
		"BACKEND_PORT="+itoa(backendPort),
		"DB_PATH="+filepath.Join(dataDir, "groceries.db"),
		// ENRICHMENT_DATA_DIR is intentionally left pointing at a path that
		// won't exist next to the extracted binary; the backend falls back to
		// its embedded CSVs. Users who want to override can drop CSVs here.
		"ENRICHMENT_DATA_DIR="+filepath.Join(dataDir, "enrichment"),
		"CONFIG_PATH="+filepath.Join(dataDir, "appie.json"),
	)
	backend.Stdout = prefixWriter("backend", os.Stdout)
	backend.Stderr = prefixWriter("backend", os.Stderr)
	configureProcAttr(backend) // own process group, so killTree gets descendants
	if err := backend.Start(); err != nil {
		return fmt.Errorf("start backend: %w", err)
	}
	log.Printf("backend started on %s (pid %d)", backendURL, backend.Process.Pid)

	if err := waitForHTTP(ctx, backendURL+"/version", 30*time.Second); err != nil {
		killTree(backend)
		return fmt.Errorf("backend did not become ready: %w", err)
	}

	// --- Dashboard ----------------------------------------------------------
	dash := exec.CommandContext(ctx, dashExe,
		"run", "app.py",
		// developmentMode defaults to true when Streamlit thinks it's running
		// from source; it must be off for --server.port to take effect.
		"--global.developmentMode=false",
		"--server.address=127.0.0.1",
		"--server.port="+itoa(dashPort),
		"--server.headless=true",
		"--browser.gatherUsageStats=false",
	)
	dash.Dir = filepath.Dir(dashExe)
	dash.Env = append(os.Environ(),
		"BACKEND_URL="+backendURL,
		"SETTINGS_PATH="+filepath.Join(dataDir, "settings.json"),
		"STREAMLIT_BROWSER_GATHER_USAGE_STATS=false",
	)
	dash.Stdout = prefixWriter("dashboard", os.Stdout)
	dash.Stderr = prefixWriter("dashboard", os.Stderr)
	configureProcAttr(dash) // Streamlit forks a child server; group-kill catches it
	if err := dash.Start(); err != nil {
		killTree(backend)
		return fmt.Errorf("start dashboard: %w", err)
	}
	dashURL := fmt.Sprintf("http://127.0.0.1:%d", dashPort)
	log.Printf("dashboard started on %s (pid %d)", dashURL, dash.Process.Pid)

	if err := waitForHTTP(ctx, dashURL, 60*time.Second); err != nil {
		log.Printf("warning: dashboard slow to start (%v); opening anyway", err)
	}
	openBrowser(dashURL)
	log.Printf("ready — your dashboard is at %s", dashURL)
	log.Printf("close this window (or press Ctrl-C) to stop.")

	// Exit when either child exits or we're signalled, then clean up the other.
	backendDone := make(chan error, 1)
	dashDone := make(chan error, 1)
	go func() { backendDone <- backend.Wait() }()
	go func() { dashDone <- dash.Wait() }()

	select {
	case <-ctx.Done():
		log.Printf("shutting down...")
	case err := <-backendDone:
		log.Printf("backend exited (%v); shutting down", err)
	case err := <-dashDone:
		log.Printf("dashboard exited (%v); shutting down", err)
	}
	stop() // ensure CommandContext cancels both children
	killTree(dash)
	killTree(backend)
	<-backendDone
	<-dashDone
	return nil
}

// extractArtifacts unzips the embedded backend + dashboard into root if not
// already present, and returns the paths to the backend and dashboard
// executables. Extraction is keyed by Version (via root), so a fresh build
// re-extracts.
func extractArtifacts(root string) (backendExe, dashExe string, err error) {
	marker := filepath.Join(root, ".extracted")
	backendExe = filepath.Join(root, "backend", backendBinName())
	dashExe = filepath.Join(root, "dashboard", dashBinName())

	if _, statErr := os.Stat(marker); statErr == nil {
		return backendExe, dashExe, nil
	}

	if err = os.MkdirAll(root, 0o755); err != nil {
		return "", "", err
	}
	log.Printf("first run for this version — unpacking (this happens once)...")
	br, bsz, err := backendZip()
	if err != nil {
		return "", "", err
	}
	if err = unzipReader(br, bsz, filepath.Join(root, "backend")); err != nil {
		return "", "", fmt.Errorf("unpack backend: %w", err)
	}
	dr, dsz, err := dashboardZip()
	if err != nil {
		return "", "", err
	}
	if err = unzipReader(dr, dsz, filepath.Join(root, "dashboard")); err != nil {
		return "", "", fmt.Errorf("unpack dashboard: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(backendExe, 0o755)
		_ = os.Chmod(dashExe, 0o755)
	}
	if err = os.WriteFile(marker, []byte(Version), 0o644); err != nil {
		return "", "", err
	}
	return backendExe, dashExe, nil
}

// --- helpers ---------------------------------------------------------------

func appDataDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "appie-insights"), nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitForHTTP(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s: %w", timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func openBrowser(url string) {
	// Allow headless runs (CI smoke tests, servers) to skip launching a
	// browser, which would otherwise fail or hang on a runner with no GUI.
	if os.Getenv("APPIE_NO_BROWSER") != "" {
		log.Printf("APPIE_NO_BROWSER set; not opening a browser. Visit %s", url)
		return
	}
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd, args = "open", []string{url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	if err := exec.Command(cmd, args...).Start(); err != nil {
		log.Printf("could not open browser automatically; visit %s", url)
	}
}

func unzipReader(r io.ReaderAt, size int64, dest string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if err := extractOne(f, dest); err != nil {
			return err
		}
	}
	return nil
}

func extractOne(f *zip.File, dest string) error {
	// Normalise separators: the zip spec uses forward slashes, but Windows'
	// Compress-Archive (used by the Windows build script) can emit backslashes.
	// Convert to the OS separator so nested paths become real directory trees.
	name := strings.ReplaceAll(f.Name, "\\", "/")
	target := filepath.Join(dest, filepath.FromSlash(name)) //nolint:gosec // names come from our own build
	if !withinDir(dest, target) {
		return fmt.Errorf("zip entry escapes destination: %s", name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc) //nolint:gosec // trusted archive from our own build
	return err
}

// withinDir reports whether target stays inside dir (guards against zip-slip).
func withinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func backendBinName() string {
	if runtime.GOOS == "windows" {
		return "appie-backend.exe"
	}
	return "appie-backend"
}

func dashBinName() string {
	if runtime.GOOS == "windows" {
		return "appie-dashboard.exe"
	}
	return "appie-dashboard"
}

func prefixWriter(prefix string, w io.Writer) io.Writer {
	return &linePrefixer{prefix: "[" + prefix + "] ", w: w}
}

type linePrefixer struct {
	prefix string
	w      io.Writer
	atBOL  bool
	inited bool
}

func (lp *linePrefixer) Write(p []byte) (int, error) {
	if !lp.inited {
		lp.atBOL = true
		lp.inited = true
	}
	for _, b := range p {
		if lp.atBOL {
			io.WriteString(lp.w, lp.prefix)
			lp.atBOL = false
		}
		lp.w.Write([]byte{b})
		if b == '\n' {
			lp.atBOL = true
		}
	}
	return len(p), nil
}

func itoa(i int) string { return fmt.Sprintf("%d", i) }
