package transport

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"
)

func generateTestCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Aegis Test"},
			CommonName:   "127.0.0.1",
		},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}, nil
}

func TestQUICDatagramTransport(t *testing.T) {
	cert, err := generateTestCert()
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	receivedServer := make(chan []byte, 10)
	var serverClientAddr net.Addr

	// Start server on dynamic loopback port
	server := NewServer("127.0.0.1:0", cert, func(clientAddr net.Addr, payload []byte) {
		serverClientAddr = clientAddr
		receivedServer <- payload
	})
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Close()

	serverAddr := server.listener.Addr().String()

	// Connect client
	client := NewClient(serverAddr, "127.0.0.1")
	client.SetInsecureSkipVerify(true)

	packetConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to listen UDP: %v", err)
	}
	defer packetConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, packetConn); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer client.Close()

	// Test 1: Send datagram Client -> Server
	testMsg := []byte("Aegis-v3-QUIC-Datagram-Test")
	if err := client.SendDatagram(testMsg); err != nil {
		t.Fatalf("Client failed to send datagram: %v", err)
	}

	select {
	case payload := <-receivedServer:
		if string(payload) != string(testMsg) {
			t.Fatalf("Expected '%s', got '%s'", testMsg, payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Timeout waiting for server to receive datagram")
	}

	// Test 2: Server -> Client Datagram
	if serverClientAddr == nil {
		t.Fatalf("Server client address not recorded")
	}
	replyMsg := []byte("Aegis-v3-Relay-Ack")
	if err := server.SendDatagram(serverClientAddr, replyMsg); err != nil {
		t.Fatalf("Server failed to send datagram to client: %v", err)
	}

	select {
	case reply := <-client.Datagrams():
		if string(reply) != string(replyMsg) {
			t.Fatalf("Expected '%s', got '%s'", replyMsg, reply)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Timeout waiting for client to receive server datagram")
	}

	// Test 3: Virtual IP routing
	server.RegisterRoute("10.77.77.2", serverClientAddr)
	routeMsg := []byte("Routed-Virtual-IP-Packet")
	if err := server.SendToVirtualIP("10.77.77.2", routeMsg); err != nil {
		t.Fatalf("Server failed to send to virtual IP: %v", err)
	}

	select {
	case reply := <-client.Datagrams():
		if string(reply) != string(routeMsg) {
			t.Fatalf("Expected '%s', got '%s'", routeMsg, reply)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Timeout waiting for client to receive routed virtual IP packet")
	}
}
