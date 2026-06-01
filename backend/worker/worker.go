// Package worker runs enrichment jobs and exposes live status for HTTP observers.
package worker

import (
	"context"
	"log/slog"
	"time"

	"appie-insights/backend/enricher"
	"appie-insights/backend/status"
	"appie-insights/backend/store"
)

// Status tracks enrichment progress (items_total / items_processed).
type Status = status.Tracker

// Request is an enrichment work item. An empty ReceiptID triggers a full pass.
type Request struct {
	ReceiptID string
}

// Worker runs enrichment jobs and maintains status for HTTP observers.
type Worker struct {
	dbPath  string
	dataDir string
	status  *Status
}

// New returns a Worker ready to handle enrichment requests.
func New(dbPath, dataDir string) *Worker {
	return &Worker{
		dbPath:  dbPath,
		dataDir: dataDir,
		status:  status.New("items_total", "items_processed"),
	}
}

// EnrichStatus returns the worker's live status.
func (w *Worker) EnrichStatus() *Status {
	return w.status
}

// Handle dispatches a single enrichment request synchronously.
func (w *Worker) Handle(ctx context.Context, req Request) {
	if req.ReceiptID != "" {
		w.runReceipt(ctx, req.ReceiptID)
	} else {
		w.runOnce(ctx)
	}
}

func (w *Worker) runOnce(ctx context.Context) {
	total, err := enricher.CountUnenriched(w.dbPath)
	if err != nil {
		slog.Error("count unenriched", "err", err)
		return
	}
	if total == 0 {
		zero := 0
		w.status.Set("idle", &zero, &zero)
		return
	}

	w.status.Set("running", &total, intPtr(0))
	slog.Info("enriching items", "count", total)

	var lastUpdate time.Time
	progress := func(done, _ int, _ string) {
		if time.Since(lastUpdate) > time.Second || done == total {
			w.status.Set("running", nil, &done)
			lastUpdate = time.Now()
		}
	}

	n, err := enricher.Enrich(ctx, w.dbPath, w.dataDir, progress)
	if err != nil {
		slog.Error("enrichment error", "err", err)
		w.status.Set("idle", intPtr(0), intPtr(0))
		return
	}
	w.status.Set("idle", &n, &n)
	slog.Info("enriched items", "count", n)
}

func (w *Worker) runReceipt(ctx context.Context, receiptID string) {
	db, err := store.Open(w.dbPath)
	if err != nil {
		slog.Error("open db", "err", err)
		return
	}
	var count int
	err = db.QueryRow(`
		SELECT COUNT(DISTINCT web_id) FROM items
		WHERE receipt_id = ? AND web_id IS NOT NULL`, receiptID).Scan(&count)
	db.Close()
	if err != nil {
		slog.Error("count receipt items", "err", err)
		return
	}

	w.status.Set("running", &count, intPtr(0))
	slog.Info("re-enriching receipt", "receipt_id", receiptID, "count", count)

	var lastUpdate time.Time
	progress := func(done, _ int, _ string) {
		if time.Since(lastUpdate) > time.Second || done == count {
			w.status.Set("running", nil, &done)
			lastUpdate = time.Now()
		}
	}

	n, err := enricher.EnrichReceipt(ctx, w.dbPath, w.dataDir, receiptID, progress)
	if err != nil {
		slog.Error("receipt enrichment error", "err", err)
		w.status.Set("idle", intPtr(0), intPtr(0))
		return
	}
	w.status.Set("idle", &n, &n)
	slog.Info("re-enriched receipt", "receipt_id", receiptID, "count", n)
}

func intPtr(n int) *int { return &n }
