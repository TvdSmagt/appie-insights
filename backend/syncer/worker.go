package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	appie "github.com/gwillem/appie-go"

	"appie-insights/backend/status"
	"appie-insights/backend/store"
)

// Status tracks sync progress (receipts_found / receipts_synced).
type Status = status.Tracker

func NewStatus() *Status { return status.New("receipts_found", "receipts_synced") }

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func InitClient(configPath string) (*appie.Client, error) {
	if at := os.Getenv("AH_ACCESS_TOKEN"); at != "" {
		// Prefer loading from config so expiresAt is populated and ensureFreshToken
		// can proactively refresh an expired access token before the first request.
		// loadConfig() overwrites any opts, so we rely on config tokens matching the
		// env vars (they do when the env vars were read from the same config file).
		// Falls back to env-var-only client if the config file is absent or lacks tokens.
		client, err := appie.NewWithConfig(configPath)
		if err == nil && client.IsAuthenticated() {
			return client, nil
		}
		return appie.New(appie.WithTokens(at, os.Getenv("AH_REFRESH_TOKEN"))), nil
	}
	client, err := appie.NewWithConfig(configPath)
	if err != nil || !client.IsAuthenticated() {
		return nil, fmt.Errorf("no AH credentials: set AH_ACCESS_TOKEN/AH_REFRESH_TOKEN or mount %s", configPath)
	}
	return client, nil
}

// Run executes a full sync cycle: receipts → orders → product backfill.
// The DB schema must already be initialized before calling Run.
func Run(ctx context.Context, configPath, dbPath string, st *Status) {
	if st.IsRunning() {
		slog.Info("sync already running, skipping")
		return
	}

	client, err := InitClient(configPath)
	if err != nil {
		slog.Info("sync: skipping, no credentials")
		return
	}

	db, err := store.Open(dbPath)
	if err != nil {
		slog.Error("sync: open db", "err", err)
		return
	}
	defer db.Close()

	delay := 50 * time.Millisecond
	apiTimeout := 20 * time.Second

	zero := 0
	st.Set("running", &zero, &zero)
	slog.Info("sync started")

	progress := func(found, synced int) {
		st.Set("running", &found, &synced)
	}

	maxReceipts := envOrInt("SYNC_MAX_RECEIPTS", 0)
	maxOrders := envOrInt("SYNC_MAX_ORDERS", maxOrderHistory)

	if err := syncReceipts(ctx, client, db, delay, apiTimeout, maxReceipts, progress); err != nil {
		slog.Error("sync receipts", "err", err)
		st.Set("idle", nil, nil)
		return
	}
	if err := syncOrders(ctx, client, db, delay, apiTimeout, maxOrders); err != nil {
		slog.Warn("sync orders", "err", err)
	}
	if err := backfillMappedProductDetails(ctx, client, db, delay, apiTimeout); err != nil {
		slog.Warn("sync backfill products", "err", err)
	}
	if err := backfillMissingNutriscores(ctx, client, db, delay, apiTimeout); err != nil {
		slog.Warn("sync backfill nutriscores", "err", err)
	}
	if err := backfillServingSizes(ctx, client, db, delay, apiTimeout); err != nil {
		slog.Warn("sync backfill serving sizes", "err", err)
	}
	if err := backfillNetContents(ctx, client, db, delay, apiTimeout); err != nil {
		slog.Warn("sync backfill net contents", "err", err)
	}

	st.Set("done", nil, nil)
	slog.Info("sync done")
}
