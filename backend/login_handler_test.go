package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"appie-insights/backend/config"
)

func TestIsLoggedIn_NoConfig(t *testing.T) {
	if got := isLoggedIn(filepath.Join(t.TempDir(), "noexist.json")); got {
		t.Error("expected false when config file does not exist")
	}
}

func TestIsLoggedIn_EnvVar(t *testing.T) {
	t.Setenv("AH_ACCESS_TOKEN", "tok")
	if !isLoggedIn(filepath.Join(t.TempDir(), "noexist.json")) {
		t.Error("expected true when AH_ACCESS_TOKEN is set")
	}
}

func TestIsLoggedIn_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "appie.json")
	if err := os.WriteFile(cfg, []byte(`{"access_token":"tok","refresh_token":"ref"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if !isLoggedIn(cfg) {
		t.Error("expected true when config file contains a token")
	}
}

func TestAuthStatusHandler_NotLoggedIn(t *testing.T) {
	mux := http.NewServeMux()
	syncCh := make(chan struct{}, 1)
	registerLoginHandlers(mux, config.Server{ConfigPath: filepath.Join(t.TempDir(), "noexist.json")}, syncCh)

	req := httptest.NewRequest("GET", "/auth/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["logged_in"] != false {
		t.Errorf("expected logged_in=false, got %v", body["logged_in"])
	}
	if body["in_progress"] != false {
		t.Errorf("expected in_progress=false, got %v", body["in_progress"])
	}
	if body["error"] != "" {
		t.Errorf("expected empty error, got %v", body["error"])
	}
}

func TestAuthStatusHandler_LoggedIn(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "appie.json")
	if err := os.WriteFile(cfg, []byte(`{"access_token":"tok","refresh_token":"ref"}`), 0600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerLoginHandlers(mux, config.Server{ConfigPath: cfg}, make(chan struct{}, 1))

	req := httptest.NewRequest("GET", "/auth/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["logged_in"] != true {
		t.Errorf("expected logged_in=true, got %v", body["logged_in"])
	}
}

func TestLoginStartHandler_AlreadyLoggedIn(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "appie.json")
	if err := os.WriteFile(cfg, []byte(`{"access_token":"tok","refresh_token":"ref"}`), 0600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerLoginHandlers(mux, config.Server{ConfigPath: cfg}, make(chan struct{}, 1))

	req := httptest.NewRequest("POST", "/login/start", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
}
