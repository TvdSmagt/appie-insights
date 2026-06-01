// Package status provides a concurrency-safe progress tracker shared by the
// syncer and enrichment worker. Each tracker carries a state string plus two
// named counters (e.g. found/synced or total/processed) rendered under
// caller-chosen JSON keys.
package status

import (
	"sync"
	"time"
)

// Tracker holds live progress for a long-running job. Safe for concurrent use.
type Tracker struct {
	mu        sync.RWMutex
	totalKey  string
	doneKey   string
	state     string
	total     int
	done      int
	updatedAt *string
}

// New returns an idle tracker whose two counters are reported under totalKey
// and doneKey in Snapshot (e.g. "items_total"/"items_processed").
func New(totalKey, doneKey string) *Tracker {
	return &Tracker{state: "idle", totalKey: totalKey, doneKey: doneKey}
}

// Set updates the state and bumps UpdatedAt. A nil total or done leaves that
// counter unchanged.
func (t *Tracker) Set(state string, total, done *int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = state
	now := time.Now().UTC().Format(time.RFC3339)
	t.updatedAt = &now
	if total != nil {
		t.total = *total
	}
	if done != nil {
		t.done = *done
	}
}

// IsRunning reports whether the tracked job is currently running.
func (t *Tracker) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state == "running"
}

// Snapshot returns a JSON-serialisable view using the tracker's configured keys.
func (t *Tracker) Snapshot() map[string]any {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]any{
		"status":     t.state,
		t.totalKey:   t.total,
		t.doneKey:    t.done,
		"updated_at": t.updatedAt,
	}
}
