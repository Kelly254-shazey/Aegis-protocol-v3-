package telemetry

import (
	"sync"
	"time"
)

// CellularMetrics holds real-time modem indicators reported by the Android OS.
type CellularMetrics struct {
	RSRP          int       // Reference Signal Received Power in dBm (-140 to -44)
	RSRQ          int       // Reference Signal Received Quality in dB (-20 to -3)
	RSSNR         int       // Signal-to-Noise Ratio in dB
	CellID        string    // Active Tower ECI / CGI
	NetworkType   string    // e.g., "LTE", "NR_NSA", "NR_SA", "HSPA"
	MeasuredLoss  float64   // Moving average packet loss percentage (0.0 to 100.0)
	LastUpdated   time.Time
}

// Listener provides callbacks when cellular metrics change.
type Listener interface {
	OnRadioMetricsUpdated(metrics CellularMetrics)
}

// Manager manages telemetry subscriptions from the Android JNI layer.
type Manager struct {
	mu        sync.RWMutex
	current   CellularMetrics
	listeners []Listener
}

// NewManager initializes the telemetry observer.
func NewManager() *Manager {
	return &Manager{
		listeners: make([]Listener, 0),
	}
}

// RegisterListener adds an observer to be notified on metric updates.
func (m *Manager) RegisterListener(l Listener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, l)
}

// UpdateFromAndroid is invoked by the Android Kotlin/Java JNI bridge.
func (m *Manager) UpdateFromAndroid(rsrp, rsrq, rssnr int, cellID, networkType string, loss float64) {
	m.mu.Lock()
	m.current = CellularMetrics{
		RSRP:         rsrp,
		RSRQ:         rsrq,
		RSSNR:        rssnr,
		CellID:       cellID,
		NetworkType:  networkType,
		MeasuredLoss: loss,
		LastUpdated:  time.Now(),
	}
	listeners := make([]Listener, len(m.listeners))
	copy(listeners, m.listeners)
	m.mu.Unlock()

	for _, l := range listeners {
		l.OnRadioMetricsUpdated(m.current)
	}
}

// Current returns the latest cellular metrics snapshot.
func (m *Manager) Current() CellularMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}
