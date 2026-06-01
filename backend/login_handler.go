package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	appie "github.com/gwillem/appie-go"

	"appie-insights/backend/config"
)

type loginManager struct {
	mu         sync.Mutex
	inProgress bool
	loginErr   string
	loginURL   string
	cancel     context.CancelFunc
}

// runLogin drives the interactive browser-based AH login flow and persists the
// resulting tokens to configPath. It is used by the `-login` CLI flag so a
// developer can obtain credentials for the integration tests without the Docker
// named volume. The flow opens the user's browser automatically; the login URL
// is also printed in case it needs to be opened manually.
func runLogin(configPath string) error {
	if isLoggedIn(configPath) {
		slog.Info("already logged in", "config", configPath)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := appie.New(
		appie.WithConfigPath(configPath),
		appie.WithOnLoginURL(func(u string) {
			slog.Info("open this URL in your browser to log in", "url", u)
		}),
	)
	if err := client.Login(ctx); err != nil {
		return err
	}

	slog.Info("login successful, tokens saved", "config", configPath)
	return nil
}

func isLoggedIn(configPath string) bool {
	if os.Getenv("AH_ACCESS_TOKEN") != "" {
		return true
	}
	client, err := appie.NewWithConfig(configPath)
	if err != nil {
		return false
	}
	return client.IsAuthenticated()
}

func (lm *loginManager) start(configPath string, syncReqCh chan<- struct{}) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if lm.inProgress {
		return
	}
	lm.inProgress = true
	lm.loginErr = ""

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	lm.cancel = cancel

	go func() {
		if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
			slog.Warn("login: mkdir config dir", "err", err)
		}

		client := appie.New(
			appie.WithConfigPath(configPath),
			appie.WithOnLoginURL(func(u string) {
				lm.mu.Lock()
				lm.loginURL = u
				lm.mu.Unlock()
			}),
		)
		err := client.Login(ctx)
		cancel()

		lm.mu.Lock()
		lm.inProgress = false
		lm.loginURL = ""
		if err != nil && err != context.Canceled {
			lm.loginErr = err.Error()
			slog.Error("login failed", "err", err)
		} else if err == nil {
			lm.loginErr = ""
			slog.Info("login successful, triggering sync")
			select {
			case syncReqCh <- struct{}{}:
			default:
			}
		}
		lm.mu.Unlock()
	}()
}

func (lm *loginManager) snapshot() map[string]any {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return map[string]any{
		"in_progress": lm.inProgress,
		"error":       lm.loginErr,
		"login_url":   lm.loginURL,
	}
}

func registerLoginHandlers(mux *http.ServeMux, cfg config.Server, syncReqCh chan<- struct{}) {
	lm := &loginManager{}

	mux.HandleFunc("GET /auth/status", func(w http.ResponseWriter, r *http.Request) {
		snap := lm.snapshot()
		snap["logged_in"] = isLoggedIn(cfg.ConfigPath)
		writeJSON(w, snap)
	})

	mux.HandleFunc("POST /login/start", func(w http.ResponseWriter, r *http.Request) {
		if isLoggedIn(cfg.ConfigPath) {
			writeJSON(w, map[string]bool{"ok": true})
			return
		}
		lm.start(cfg.ConfigPath, syncReqCh)
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		if err := os.Remove(cfg.ConfigPath); err != nil && !os.IsNotExist(err) {
			http.Error(w, "failed to logout: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
}
