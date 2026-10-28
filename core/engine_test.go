package core

import (
	"testing"
	"time"
)

func TestEngineLifecycle(t *testing.T) {
	eng := NewEngine()
	if eng == nil {
		t.Fatalf("Failed to create engine")
	}

	if eng.IsRunning() {
		t.Fatalf("Engine should not be running initially")
	}

	// Update radio metrics before connection
	eng.UpdateRadioMetrics(-85, -10, 15, "Cell-404-01-A", "LTE", 0.02)

	up, down, rec := eng.Metrics()
	if up != 0 || down != 0 || rec != 0 {
		t.Fatalf("Initial metrics should be 0, got up=%d down=%d rec=%d", up, down, rec)
	}

	// Calling Stop when not running should be a graceful no-op
	eng.Stop()

	// Wait briefly to ensure no goroutine leaks
	time.Sleep(50 * time.Millisecond)
}
