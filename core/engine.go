package core

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"aegis-core/fec"
	"aegis-core/telemetry"
	"aegis-core/transport"
)

var (
	ErrAlreadyRunning = errors.New("aegis: engine already running")
	ErrNotRunning     = errors.New("aegis: engine not running")
)

// Engine is the central Aegis Protocol coordinator running inside the Android app process.
type Engine struct {
	mu           sync.Mutex
	isRunning    atomic.Bool
	ctx          context.Context
	cancel       context.CancelFunc
	fecEncoder   *fec.Encoder
	fecDecoder   *fec.Decoder
	adaptive     *fec.AdaptiveController
	telemetry    *telemetry.Manager
	client       *transport.Client
	tunFile      *os.File
	bytesUp      atomic.Int64
	bytesDown    atomic.Int64
	recoveredPkt atomic.Int64
}

// NewEngine creates a new unstarted Aegis engine instance.
func NewEngine() *Engine {
	return &Engine{
		telemetry: telemetry.NewManager(),
	}
}

// Start activates the tunnel with the Android VpnService TUN file descriptor.
func (e *Engine) Start(relayEndpoint string, serverName string, tunFd int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.isRunning.Load() {
		return ErrAlreadyRunning
	}

	// 1. Initialize FEC Encoder (Default 10:1 healthy link)
	enc, err := fec.NewEncoder(10, 1)
	if err != nil {
		return err
	}
	e.fecEncoder = enc
	e.adaptive = fec.NewAdaptiveController(enc)

	// 2. Wrap TUN FileDescriptor from Android VpnService
	e.tunFile = os.NewFile(uintptr(tunFd), "tun0")

	// 3. Initialize FEC Decoder
	e.fecDecoder = fec.NewDecoder(3*time.Second, func(blockID uint32, packets [][]byte) {
		for _, pkt := range packets {
			if len(pkt) > 0 && e.tunFile != nil {
				_, _ = e.tunFile.Write(pkt)
				e.bytesDown.Add(int64(len(pkt)))
				e.recoveredPkt.Add(1)
			}
		}
	})

	// 4. Create QUIC Client to Cloud Relay
	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.client = transport.NewClient(relayEndpoint, serverName)

	// In Android, the caller passes a protected UDP socket or connects directly
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return err
	}

	if err := e.client.Connect(e.ctx, udpConn); err != nil {
		return err
	}

	e.isRunning.Store(true)

	// Start TUN -> FEC -> QUIC outbound loop
	go e.outboundLoop()

	// Start QUIC -> FEC -> TUN inbound loop
	go e.inboundLoop()

	return nil
}

// outboundLoop reads IP packets from Android tun0, encodes them into FEC shards, and sends QUIC datagrams.
func (e *Engine) outboundLoop() {
	buf := make([]byte, 1500)
	packetBatch := make([][]byte, 0, 10)
	batchTicker := time.NewTicker(5 * time.Millisecond)
	defer batchTicker.Stop()

	for e.isRunning.Load() {
		select {
		case <-e.ctx.Done():
			return
		default:
			n, err := e.tunFile.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				pkt := make([]byte, n)
				copy(pkt, buf[:n])
				packetBatch = append(packetBatch, pkt)
				e.bytesUp.Add(int64(n))

				strategy := e.adaptive.Current()
				if len(packetBatch) >= strategy.DataShards {
					e.flushBatch(packetBatch)
					packetBatch = packetBatch[:0]
				}
			}
		}
	}
}

func (e *Engine) flushBatch(batch [][]byte) {
	if len(batch) == 0 {
		return
	}
	shards, err := e.fecEncoder.EncodeBlock(batch)
	if err != nil {
		return
	}
	for _, shard := range shards {
		wire := shard.Encode()
		_ = e.client.SendDatagram(wire)
	}
}

// inboundLoop receives QUIC datagrams from the Cloud Relay and passes them to the FEC decoder.
func (e *Engine) inboundLoop() {
	datagrams := e.client.Datagrams()
	for {
		select {
		case <-e.ctx.Done():
			return
		case data, ok := <-datagrams:
			if !ok {
				return
			}
			shard, err := fec.DecodeShard(data)
			if err == nil {
				_ = e.fecDecoder.Ingest(shard)
			}
		}
	}
}

// UpdateRadioMetrics updates telemetry from Android TelephonyManager (RSRP/loss).
func (e *Engine) UpdateRadioMetrics(rsrp, rsrq, rssnr int, cellID, networkType string, loss float64) {
	if e.telemetry != nil {
		e.telemetry.UpdateFromAndroid(rsrp, rsrq, rssnr, cellID, networkType, loss)
	}
	if e.adaptive != nil {
		e.adaptive.UpdateMetrics(loss)
	}
}

// Stop shuts down the engine cleanly.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.isRunning.Load() {
		return
	}
	e.isRunning.Store(false)
	if e.cancel != nil {
		e.cancel()
	}
	if e.client != nil {
		_ = e.client.Close()
	}
	if e.tunFile != nil {
		_ = e.tunFile.Close()
	}
}
