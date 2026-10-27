package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

var (
	ErrNotConnected = errors.New("transport: client is not connected to cloud relay")
)

// Client handles the QUIC connection from the mobile device to the Cloud Edge Relay.
type Client struct {
	mu           sync.RWMutex
	endpoint     string
	tlsConfig    *tls.Config
	quicConfig   *quic.Config
	connection   *quic.Conn
	datagramChan chan []byte
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewClient initializes a mobile QUIC transport client.
func NewClient(endpoint string, serverName string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		endpoint: endpoint,
		tlsConfig: &tls.Config{
			ServerName:         serverName,
			InsecureSkipVerify: false, // Strict TLS verification against Relay certificate
			NextProtos:         []string{ALPN},
		},
		quicConfig: &quic.Config{
			EnableDatagrams:    true,
			MaxIdleTimeout:     30 * time.Second,
			KeepAlivePeriod:    10 * time.Second,
			InitialPacketSize:  DefaultMTU,
		},
		datagramChan: make(chan []byte, 1024),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Connect dials the Cloud Edge Relay over UDP on Port 443.
func (c *Client) Connect(ctx context.Context, udpConn net.PacketConn) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	udpAddr, err := net.ResolveUDPAddr("udp", c.endpoint)
	if err != nil {
		return fmt.Errorf("transport: failed to resolve relay endpoint: %w", err)
	}

	tr := quic.Transport{
		Conn: udpConn,
	}

	conn, err := tr.Dial(ctx, udpAddr, c.tlsConfig, c.quicConfig)
	if err != nil {
		return fmt.Errorf("transport: failed to dial QUIC relay: %w", err)
	}

	c.connection = conn
	go c.receiveLoop()

	return nil
}

// SendDatagram sends an RFC 9221 unreliable datagram (IP packet or FEC shard) into space/cloud.
func (c *Client) SendDatagram(payload []byte) error {
	c.mu.RLock()
	conn := c.connection
	c.mu.RUnlock()

	if conn == nil {
		return ErrNotConnected
	}

	return conn.SendDatagram(payload)
}

// Datagrams returns the receive channel for incoming packets.
func (c *Client) Datagrams() <-chan []byte {
	return c.datagramChan
}

// receiveLoop continuously reads incoming datagrams from the Cloud Relay.
func (c *Client) receiveLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			c.mu.RLock()
			conn := c.connection
			c.mu.RUnlock()

			if conn == nil {
				return
			}

			data, err := conn.ReceiveDatagram(c.ctx)
			if err != nil {
				return
			}

			select {
			case c.datagramChan <- data:
			default:
				// Channel full, drop oldest or overflow
			}
		}
	}
}

// Close shuts down the client connection gracefully.
func (c *Client) Close() error {
	c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connection != nil {
		return c.connection.CloseWithError(0, "client disconnected")
	}
	return nil
}
