package status

import (
	"sync"
	"testing"
)

func TestSetAndSnapshot(t *testing.T) {
	s := New("items_total", "items_processed")

	n5, n3 := 5, 3
	s.Set("running", &n5, &n3)

	snap := s.Snapshot()
	if snap["status"] != "running" {
		t.Errorf("status: got %v, want running", snap["status"])
	}
	if snap["items_total"] != 5 {
		t.Errorf("items_total: got %v, want 5", snap["items_total"])
	}
	if snap["items_processed"] != 3 {
		t.Errorf("items_processed: got %v, want 3", snap["items_processed"])
	}
	if snap["updated_at"] == nil {
		t.Error("updated_at should be set")
	}
}

func TestSetNilArgsLeavesCountersUnchanged(t *testing.T) {
	s := New("items_total", "items_processed")

	n10, n2 := 10, 2
	s.Set("running", &n10, &n2)
	s.Set("running", nil, nil)

	snap := s.Snapshot()
	if snap["items_total"] != 10 {
		t.Errorf("items_total should be unchanged: got %v, want 10", snap["items_total"])
	}
	if snap["items_processed"] != 2 {
		t.Errorf("items_processed should be unchanged: got %v, want 2", snap["items_processed"])
	}
}

func TestIsRunning(t *testing.T) {
	s := New("a", "b")
	if s.IsRunning() {
		t.Error("new tracker should be idle")
	}
	s.Set("running", nil, nil)
	if !s.IsRunning() {
		t.Error("expected running")
	}
	s.Set("done", nil, nil)
	if s.IsRunning() {
		t.Error("expected not running after done")
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := New("items_total", "items_processed")
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			s.Set("running", &n, &n)
		}(i)
		go func() {
			defer wg.Done()
			_ = s.Snapshot()
		}()
	}

	wg.Wait()
	snap := s.Snapshot()
	if snap["status"] == nil {
		t.Error("status should not be nil after concurrent access")
	}
}
