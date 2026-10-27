package fec

import (
	"fmt"
	"sync"
	"time"

	"github.com/klauspost/reedsolomon"
)

type blockBuffer struct {
	blockID     uint32
	dataShards  int
	totalShards int
	received    int
	shards      [][]byte
	createdAt   time.Time
}

// Decoder caches incoming shards and reconstructs complete data blocks in memory.
type Decoder struct {
	mu           sync.Mutex
	blocks       map[uint32]*blockBuffer
	decoders     map[string]reedsolomon.Encoder
	maxAge       time.Duration
	onRecovered  func(blockID uint32, packets [][]byte)
}

// NewDecoder initializes a new FEC block decoder.
func NewDecoder(maxAge time.Duration, onRecovered func(blockID uint32, packets [][]byte)) *Decoder {
	if maxAge <= 0 {
		maxAge = 3 * time.Second
	}
	d := &Decoder{
		blocks:      make(map[uint32]*blockBuffer),
		decoders:    make(map[string]reedsolomon.Encoder),
		maxAge:      maxAge,
		onRecovered: onRecovered,
	}
	go d.gcLoop()
	return d
}

// Ingest processes an incoming Shard. If the block has enough shards to reconstruct, it invokes onRecovered.
func (d *Decoder) Ingest(s *Shard) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	b, exists := d.blocks[s.BlockID]
	if !exists {
		b = &blockBuffer{
			blockID:     s.BlockID,
			dataShards:  int(s.DataShards),
			totalShards: int(s.TotalShards),
			shards:      make([][]byte, s.TotalShards),
			createdAt:   time.Now(),
		}
		d.blocks[s.BlockID] = b
	}

	if int(s.ShardIndex) >= b.totalShards {
		return fmt.Errorf("fec: shard index %d out of bounds (total %d)", s.ShardIndex, b.totalShards)
	}

	// Store shard if not already received
	if b.shards[s.ShardIndex] == nil {
		b.shards[s.ShardIndex] = s.Payload
		b.received++
	}

	// Check if we have reached the recovery threshold (any DataShards out of TotalShards)
	if b.received >= b.dataShards {
		delete(d.blocks, s.BlockID)

		// Check if any data shard is missing
		needsReconstruction := false
		for i := 0; i < b.dataShards; i++ {
			if b.shards[i] == nil {
				needsReconstruction = true
				break
			}
		}

		if needsReconstruction {
			key := fmt.Sprintf("%d:%d", b.dataShards, b.totalShards-b.dataShards)
			rs, ok := d.decoders[key]
			if !ok {
				var err error
				rs, err = reedsolomon.New(b.dataShards, b.totalShards-b.dataShards)
				if err != nil {
					return fmt.Errorf("fec: failed to create decoder: %w", err)
				}
				d.decoders[key] = rs
			}

			if err := rs.Reconstruct(b.shards); err != nil {
				return fmt.Errorf("fec: reconstruction failed: %w", err)
			}
		}

		// Extract original data packets
		dataPackets := make([][]byte, b.dataShards)
		for i := 0; i < b.dataShards; i++ {
			dataPackets[i] = b.shards[i]
		}

		if d.onRecovered != nil {
			go d.onRecovered(b.blockID, dataPackets)
		}
	}

	return nil
}

// gcLoop purges expired, incomplete blocks from memory to prevent leaks.
func (d *Decoder) gcLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		d.mu.Lock()
		now := time.Now()
		for id, b := range d.blocks {
			if now.Sub(b.createdAt) > d.maxAge {
				delete(d.blocks, id)
			}
		}
		d.mu.Unlock()
	}
}
