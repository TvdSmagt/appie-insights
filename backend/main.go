package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"appie-insights/backend/analytics"
	"appie-insights/backend/config"
	"appie-insights/backend/enricher"
	"appie-insights/backend/store"
	"appie-insights/backend/syncer"
	"appie-insights/backend/worker"
)

var (
	dbPath     = envOr("DB_PATH", "./data/groceries.db")
	dataDir    = envOr("ENRICHMENT_DATA_DIR", "./backend/data")
	configPath = envOr("CONFIG_PATH", defaultConfigPath())
	httpPort   = envOr("BACKEND_PORT", "8001")
)

func defaultConfigPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "appie", "appie.json")
	}
	return "appie.json"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	once := flag.Bool("once", false, "enrich all pending products and exit (no HTTP server)")
	syncOnce := flag.Bool("sync-once", false, "run one sync cycle and exit (no HTTP server)")
	login := flag.Bool("login", false, "run the interactive AH login flow, save tokens to the config path, and exit")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if *login {
		if err := runLogin(configPath); err != nil {
			slog.Error("login failed", "err", err)
			os.Exit(1)
		}
		return
	}

	if *once {
		n, err := enricher.Enrich(context.Background(), dbPath, dataDir, func(done, total int, label string) {
			slog.Info("enriching", "done", done, "total", total, "product", label)
		})
		if err != nil {
			slog.Error("enrichment failed", "err", err)
			os.Exit(1)
		}
		slog.Info("done", "enriched", n)
		return
	}

	if *syncOnce {
		db, err := store.Open(dbPath)
		if err != nil {
			slog.Error("open db", "err", err)
			os.Exit(1)
		}
		if err := initDB(db); err != nil {
			db.Close()
			slog.Error("init db", "err", err)
			os.Exit(1)
		}
		db.Close()
		ctx := context.Background()
		syncer.Run(ctx, configPath, dbPath, syncer.NewStatus())
		return
	}

	// Initialize DB schema once at startup before starting workers.
	if err := func() error {
		db, err := store.Open(dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		return initDB(db)
	}(); err != nil {
		slog.Error("init db", "err", err)
		os.Exit(1)
	}

	w := worker.New(dbPath, dataDir)
	enrichReqCh := make(chan worker.Request, 1)

	syncSt := syncer.NewStatus()
	syncReqCh := make(chan struct{}, 1)
	syncReqCh <- struct{}{} // trigger sync on startup

	cfg := config.Server{DBPath: dbPath, ConfigPath: configPath, DataDir: dataDir}

	mux := http.NewServeMux()
	registerHandlers(mux, cfg, enrichReqCh, w.EnrichStatus(), syncReqCh, syncSt)
	registerLoginHandlers(mux, cfg, syncReqCh)
	registerProductHandlers(mux, cfg)
	analytics.RegisterHandlers(mux, cfg)

	srv := &http.Server{Addr: ":" + httpPort, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		slog.Info("HTTP API listening", "port", httpPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "err", err)
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-syncReqCh:
				syncer.Run(ctx, configPath, dbPath, syncSt)
				select {
				case enrichReqCh <- worker.Request{}:
				default:
				}
			}
		}
	}()

	slog.Info("worker started", "db", dbPath, "data_dir", dataDir, "port", httpPort)

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := srv.Shutdown(shutCtx); err != nil {
				slog.Warn("server shutdown", "err", err)
			}
			cancel()
			return
		case req := <-enrichReqCh:
			w.Handle(ctx, req)
		}
	}
}

func registerHandlers(mux *http.ServeMux, cfg config.Server, reqCh chan<- worker.Request, st *worker.Status, syncReqCh chan<- struct{}, syncSt *syncer.Status) {
	type enrichBody struct {
		ReceiptID *string `json:"receipt_id"`
	}

	mux.HandleFunc("POST /enrich", func(w http.ResponseWriter, r *http.Request) {
		var body enrichBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			slog.Warn("POST /enrich: decode body", "err", err)
		}
		req := worker.Request{}
		if body.ReceiptID != nil {
			req.ReceiptID = *body.ReceiptID
		}
		select {
		case reqCh <- req:
		default:
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, st.Snapshot())
	})

	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"version": Version})
	})

	mux.HandleFunc("GET /categories", func(w http.ResponseWriter, r *http.Request) {
		entries, err := enricher.LoadCO2EqCategories(cfg.DataDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, entries)
	})

	mux.HandleFunc("GET /corrections", func(w http.ResponseWriter, r *http.Request) {
		corrections, err := enricher.LoadCorrections(cfg.DataDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if corrections == nil {
			corrections = []enricher.Correction{}
		}
		writeJSON(w, corrections)
	})

	mux.HandleFunc("POST /corrections", func(w http.ResponseWriter, r *http.Request) {
		var entries []enricher.Correction
		if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := enricher.SaveCorrections(cfg.DataDir, entries); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /sync", func(w http.ResponseWriter, r *http.Request) {
		select {
		case syncReqCh <- struct{}{}:
		default:
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /sync/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, syncSt.Snapshot())
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("writeJSON", "err", err)
	}
}
