package fec

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/klauspost/reedsolomon"
)

var (
	ErrNotEnoughShards = errors.New("fec: insufficient shards to reconstruct block")
	ErrInvalidConfig   = errors.New("fec: dataShards must be > 0 and parityShards >= 0")
)

// Encoder handles splitting outgoing packets and generating Reed-Solomon parity shards.
type Encoder struct {
	mu           sync.RWMutex
	dataShards   int
	parityShards int
	enc          reedsolomon.Encoder
	nextBlockID  atomic.Uint32
}

// NewEncoder creates a new Reed-Solomon encoder for a given (data, parity) configuration.
func NewEncoder(dataShards, parityShards int) (*Encoder, error) {
	if dataShards <= 0 || parityShards < 0 {
		return nil, ErrInvalidConfig
	}
	enc, err := reedsolomon.New(dataShards, parityShards)
	if err != nil {
		return nil, fmt.Errorf("fec: failed to init reedsolomon: %w", err)
	}
	return &Encoder{
		dataShards:   dataShards,
		parityShards: parityShards,
		enc:          enc,
	}, nil
}

// UpdateRatio updates the (data, parity) ratio dynamically without restarting.
func (e *Encoder) UpdateRatio(dataShards, parityShards int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.dataShards == dataShards && e.parityShards == parityShards {
		return nil
	}
	enc, err := reedsolomon.New(dataShards, parityShards)
	if err != nil {
		return err
	}
	e.dataShards = dataShards
	e.parityShards = parityShards
	e.enc = enc
	return nil
}

// EncodeBlock takes a slice of data packets (up to dataShards in size) and returns all shards (data + parity).
func (e *Encoder) EncodeBlock(packets [][]byte) ([]*Shard, error) {
	e.mu.RLock()
	dataCount := e.dataShards
	parityCount := e.parityShards
	rs := e.enc
	e.mu.RUnlock()

	if len(packets) == 0 {
		return nil, nil
	}

	blockID := e.nextBlockID.Add(1)

	// Determine max packet size to pad all shards to uniform length
	maxLen := 0
	for _, p := range packets {
		if len(p) > maxLen {
			maxLen = len(p)
		}
	}
	if maxLen == 0 {
		maxLen = 1
	}

	totalShards := dataCount + parityCount
	rawShards := make([][]byte, totalShards)

	// Fill data shards
	for i := 0; i < dataCount; i++ {
		rawShards[i] = make([]byte, maxLen)
		if i < len(packets) {
			copy(rawShards[i], packets[i])
		}
	}

	// Allocate parity shards
	for i := dataCount; i < totalShards; i++ {
		rawShards[i] = make([]byte, maxLen)
	}

	// Generate parity using SIMD-accelerated Reed-Solomon
	if parityCount > 0 {
		if err := rs.Encode(rawShards); err != nil {
			return nil, fmt.Errorf("fec: encode failed: %w", err)
		}
	}

	// Package into wire shards
	result := make([]*Shard, totalShards)
	for i := 0; i < totalShards; i++ {
		result[i] = &Shard{
			BlockID:     blockID,
			ShardIndex:  uint8(i),
			TotalShards: uint8(totalShards),
			DataShards:  uint8(dataCount),
			Payload:     rawShards[i],
		}
	}

	return result, nil
}
