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
	ErrClientNotFound = errors.New("transport: client connection not found")
)

// Server handles incoming QUIC connections from mobile devices and laptops at the Cloud Edge.
type Server struct {
	mu           sync.RWMutex
	bindAddr     string
	tlsConfig    *tls.Config
	quicConfig   *quic.Config
	listener     *quic.Listener
	onDatagram   func(clientAddr net.Addr, payload []byte)
	ctx          context.Context
	cancel       context.CancelFunc

	// Active client connections indexed by clientAddr string
	clientsByAddr sync.Map // string -> *quic.Conn

	// Routing table mapping virtual IP (e.g. "10.77.77.2") -> clientAddr string
	routeTable sync.Map // string -> string
}

// NewServer initializes a Cloud Edge Relay listener.
func NewServer(bindAddr string, tlsCert tls.Certificate, onDatagram func(clientAddr net.Addr, payload []byte)) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		bindAddr: bindAddr,
		tlsConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			NextProtos:   []string{ALPN},
		},
		quicConfig: &quic.Config{
			EnableDatagrams:   true,
			MaxIdleTimeout:    30 * time.Second,
			KeepAlivePeriod:   10 * time.Second,
			InitialPacketSize: DefaultMTU,
		},
		onDatagram: onDatagram,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start begins listening on the UDP bind address (e.g., ":443").
func (s *Server) Start() error {
	udpAddr, err := net.ResolveUDPAddr("udp", s.bindAddr)
	if err != nil {
		return fmt.Errorf("transport: failed to resolve bind addr: %w", err)
	}

	listener, err := quic.ListenAddr(udpAddr.String(), s.tlsConfig, s.quicConfig)
	if err != nil {
		return fmt.Errorf("transport: failed to start QUIC listener: %w", err)
	}

	s.listener = listener
	go s.acceptLoop()

	return nil
}

// acceptLoop accepts new incoming client connections.
func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept(s.ctx)
		if err != nil {
			return
		}
		go s.handleClient(conn)
	}
}

// handleClient registers and continuously processes datagrams from an individual client connection.
func (s *Server) handleClient(conn *quic.Conn) {
	addrKey := conn.RemoteAddr().String()
	s.clientsByAddr.Store(addrKey, conn)
	log.Printf("[+] New QUIC client session connected: %s", addrKey)

	defer func() {
		log.Printf("[-] QUIC client session disconnected: %s", addrKey)
		s.clientsByAddr.Delete(addrKey)
		// Clean up routeTable entries pointing to this addrKey
		s.routeTable.Range(func(key, value any) bool {
			if value.(string) == addrKey {
				s.routeTable.Delete(key)
			}
			return true
		})
		_ = conn.CloseWithError(0, "session ended")
	}()

	for {
		data, err := conn.ReceiveDatagram(s.ctx)
		if err != nil {
			return
		}

		if s.onDatagram != nil {
			s.onDatagram(conn.RemoteAddr(), data)
		}
	}
}

// RegisterRoute associates a virtual IP address (e.g. "10.77.77.2") with a physical client address.
func (s *Server) RegisterRoute(virtualIP string, clientAddr net.Addr) {
	if virtualIP != "" && clientAddr != nil {
		s.routeTable.Store(virtualIP, clientAddr.String())
	}
}

// SendDatagram sends a raw datagram payload to a specific client address.
func (s *Server) SendDatagram(clientAddr net.Addr, payload []byte) error {
	if clientAddr == nil {
		return ErrClientNotFound
	}
	val, ok := s.clientsByAddr.Load(clientAddr.String())
	if !ok {
		return ErrClientNotFound
	}
	conn := val.(*quic.Conn)
	return conn.SendDatagram(payload)
}

// SendToVirtualIP routes a datagram to the client holding the specified virtual IP address.
func (s *Server) SendToVirtualIP(virtualIP string, payload []byte) error {
	val, ok := s.routeTable.Load(virtualIP)
	if !ok {
		// Fallback: If only 1 client is connected, route to it
		var fallbackConn *quic.Conn
		count := 0
		s.clientsByAddr.Range(func(k, v any) bool {
			fallbackConn = v.(*quic.Conn)
			count++
			return count <= 1
		})
		if count == 1 && fallbackConn != nil {
			return fallbackConn.SendDatagram(payload)
		}
		return ErrClientNotFound
	}
	addrKey := val.(string)
	connVal, ok := s.clientsByAddr.Load(addrKey)
	if !ok {
		return ErrClientNotFound
	}
	return connVal.(*quic.Conn).SendDatagram(payload)
}

// BroadcastDatagram delivers a datagram to all currently connected clients.
func (s *Server) BroadcastDatagram(payload []byte) {
	s.clientsByAddr.Range(func(k, v any) bool {
		conn := v.(*quic.Conn)
		_ = conn.SendDatagram(payload)
		return true
	})
}

// ConnectedClientCount returns the number of active clients.
func (s *Server) ConnectedClientCount() int {
	count := 0
	s.clientsByAddr.Range(func(k, v any) bool {
		count++
		return true
	})
	return count
}

// Close gracefully stops the Cloud Edge Relay listener and all active sessions.
func (s *Server) Close() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
