package fec

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestFEC_EncodeDecode_NoLoss(t *testing.T) {
	enc, err := NewEncoder(10, 2)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	packets := make([][]byte, 10)
	for i := range packets {
		packets[i] = []byte(fmt.Sprintf("test packet payload #%d", i))
	}

	shards, err := enc.EncodeBlock(packets)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	if len(shards) != 12 {
		t.Fatalf("expected 12 shards, got %d", len(shards))
	}

	var wg sync.WaitGroup
	wg.Add(1)

	dec := NewDecoder(2*time.Second, func(blockID uint32, recovered [][]byte) {
		defer wg.Done()
		if len(recovered) != 10 {
			t.Errorf("expected 10 recovered packets, got %d", len(recovered))
		}
		for i := 0; i < 10; i++ {
			if !bytes.HasPrefix(recovered[i], packets[i]) {
				t.Errorf("shard %d mismatch: got %s, want %s", i, recovered[i], packets[i])
			}
		}
	})

	// Ingest all 12 shards
	for _, s := range shards {
		wire := s.Encode()
		decodedShard, err := DecodeShard(wire)
		if err != nil {
			t.Fatalf("shard decode wire failed: %v", err)
		}
		if err := dec.Ingest(decodedShard); err != nil {
			t.Fatalf("ingest failed: %v", err)
		}
	}

	wg.Wait()
}

func TestFEC_Reconstruct_WithDroppedPackets(t *testing.T) {
	// 6 data + 4 parity = total 10 shards. Can tolerate up to 4 lost shards!
	enc, err := NewEncoder(6, 4)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	packets := make([][]byte, 6)
	for i := range packets {
		packets[i] = []byte(fmt.Sprintf("critical payload data block %d", i))
	}

	shards, err := enc.EncodeBlock(packets)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	dec := NewDecoder(2*time.Second, func(blockID uint32, recovered [][]byte) {
		defer wg.Done()
		for i := 0; i < 6; i++ {
			if !bytes.HasPrefix(recovered[i], packets[i]) {
				t.Errorf("recovered packet %d corrupt: got %s, want %s", i, recovered[i], packets[i])
			}
		}
	})

	// Drop shards 0, 2, and 5 (3 lost data packets out of 6!)
	// Send shards 1, 3, 4, 6, 7, 8 (total 6 shards delivered out of 10)
	deliveredIndices := []int{1, 3, 4, 6, 7, 8}

	for _, idx := range deliveredIndices {
		wire := shards[idx].Encode()
		s, err := DecodeShard(wire)
		if err != nil {
			t.Fatalf("decode wire error: %v", err)
		}
		if err := dec.Ingest(s); err != nil {
			t.Fatalf("ingest error: %v", err)
		}
	}

	wg.Wait()
}
