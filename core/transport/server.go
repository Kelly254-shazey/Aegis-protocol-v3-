package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// Server handles incoming QUIC connections from mobile devices at the Cloud Edge.
type Server struct {
	mu           sync.RWMutex
	bindAddr     string
	tlsConfig    *tls.Config
	quicConfig   *quic.Config
	listener     *quic.Listener
	onDatagram   func(clientAddr net.Addr, payload []byte)
	ctx          context.Context
	cancel       context.CancelFunc
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

// acceptLoop accepts new incoming client connections from mobile devices.
func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept(s.ctx)
		if err != nil {
			return
		}
		go s.handleClient(conn)
	}
}

// handleClient continuously processes datagrams from an individual client connection.
func (s *Server) handleClient(conn *quic.Conn) {
	defer conn.CloseWithError(0, "session ended")

	clientAddr := conn.RemoteAddr()

	for {
		data, err := conn.ReceiveDatagram(s.ctx)
		if err != nil {
			return
		}

		if s.onDatagram != nil {
			s.onDatagram(clientAddr, data)
		}
	}
}

// Close gracefully stops the Cloud Edge Relay listener.
func (s *Server) Close() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
