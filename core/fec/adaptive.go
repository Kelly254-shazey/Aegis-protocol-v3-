package fec

import "sync"

// Strategy dictates how aggressively parity shards are generated.
type Strategy struct {
	DataShards   int
	ParityShards int
}

var (
	// HealthyLink: 10:1 (10% overhead) when packet loss is < 1%
	HealthyLink = Strategy{DataShards: 10, ParityShards: 1}

	// ModerateJitter: 8:2 (25% overhead) when packet loss is 1% - 5%
	ModerateJitter = Strategy{DataShards: 8, ParityShards: 2}

	// SevereDegradation: 6:4 (40% overhead) when packet loss > 5% (e.g. 1-bar border signal or handoff)
	SevereDegradation = Strategy{DataShards: 6, ParityShards: 4}
)

// AdaptiveController monitors network health and adjusts FEC parity dynamically.
type AdaptiveController struct {
	mu              sync.RWMutex
	currentStrategy Strategy
	encoder         *Encoder
}

// NewAdaptiveController creates a controller linked to an Encoder.
func NewAdaptiveController(enc *Encoder) *AdaptiveController {
	return &AdaptiveController{
		currentStrategy: HealthyLink,
		encoder:         enc,
	}
}

// UpdateMetrics receives measured packet loss percentage and updates the FEC encoder ratio.
func (ac *AdaptiveController) UpdateMetrics(lossPercentage float64) Strategy {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	var target Strategy
	switch {
	case lossPercentage > 5.0:
		target = SevereDegradation
	case lossPercentage >= 1.0:
		target = ModerateJitter
	default:
		target = HealthyLink
	}

	if target != ac.currentStrategy {
		ac.currentStrategy = target
		_ = ac.encoder.UpdateRatio(target.DataShards, target.ParityShards)
	}

	return target
}

// Current returns the active FEC strategy.
func (ac *AdaptiveController) Current() Strategy {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.currentStrategy
}
