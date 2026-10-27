package fec

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// HeaderSize: Magic (2B) + BlockID (4B) + ShardIndex (1B) + TotalShards (1B) + DataShards (1B) + PayloadLen (2B)
	HeaderSize = 11

	MagicByte1 = 0xAE
	MagicByte2 = 0x61
)

var (
	ErrInvalidMagic   = errors.New("fec: invalid packet magic header")
	ErrBufferTooShort = errors.New("fec: buffer smaller than header size")
)

// Shard represents an individual FEC chunk (either data or parity).
type Shard struct {
	BlockID     uint32
	ShardIndex  uint8
	TotalShards uint8
	DataShards  uint8
	Payload     []byte
}

// EncodeHeader writes the wire header for a shard into dest buffer.
func (s *Shard) Encode() []byte {
	buf := make([]byte, HeaderSize+len(s.Payload))
	buf[0] = MagicByte1
	buf[1] = MagicByte2
	binary.BigEndian.PutUint32(buf[2:6], s.BlockID)
	buf[6] = s.ShardIndex
	buf[7] = s.TotalShards
	buf[8] = s.DataShards
	binary.BigEndian.PutUint16(buf[9:11], uint16(len(s.Payload)))
	copy(buf[HeaderSize:], s.Payload)
	return buf
}

// DecodeShard parses a wire buffer into a Shard struct.
func DecodeShard(data []byte) (*Shard, error) {
	if len(data) < HeaderSize {
		return nil, ErrBufferTooShort
	}
	if data[0] != MagicByte1 || data[1] != MagicByte2 {
		return nil, ErrInvalidMagic
	}

	blockID := binary.BigEndian.Uint32(data[2:6])
	shardIdx := data[6]
	totalShards := data[7]
	dataShards := data[8]
	payloadLen := int(binary.BigEndian.Uint16(data[9:11]))

	if len(data) < HeaderSize+payloadLen {
		return nil, fmt.Errorf("fec: truncated payload, expected %d bytes, got %d", payloadLen, len(data)-HeaderSize)
	}

	payload := make([]byte, payloadLen)
	copy(payload, data[HeaderSize:HeaderSize+payloadLen])

	return &Shard{
		BlockID:     blockID,
		ShardIndex:  shardIdx,
		TotalShards: totalShards,
		DataShards:  dataShards,
		Payload:     payload,
	}, nil
}
