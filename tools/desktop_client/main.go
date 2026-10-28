package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"aegis-core/crypto"
	"aegis-core/transport"
)

var (
	bytesTransferred atomic.Int64
	activeConns      atomic.Int64
)

// handleSocks5 implements RFC 1928 SOCKS5 Protocol Server
func handleSocks5(conn net.Conn) {
	defer conn.Close()
	activeConns.Add(1)
	defer activeConns.Add(-1)

	reader := bufio.NewReader(conn)

	// 1. Version identifier / method selection
	ver, err := reader.ReadByte()
	if err != nil || ver != 0x05 {
		return
	}

	nmethods, err := reader.ReadByte()
	if err != nil {
		return
	}

	methods := make([]byte, int(nmethods))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}

	// Respond with NO AUTH REQUIRED (0x00)
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. Request Details
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return
	}

	cmd := header[1]
	if cmd != 0x01 { // Only CONNECT supported
		_, _ = conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Command not supported
		return
	}

	var targetHost string
	atyp := header[3]

	switch atyp {
	case 0x01: // IPv4
		ipv4 := make([]byte, 4)
		if _, err := io.ReadFull(reader, ipv4); err != nil {
			return
		}
		targetHost = net.IP(ipv4).String()
	case 0x03: // Domain name
		domainLen, err := reader.ReadByte()
		if err != nil {
			return
		}
		domain := make([]byte, int(domainLen))
		if _, err := io.ReadFull(reader, domain); err != nil {
			return
		}
		targetHost = string(domain)
	case 0x04: // IPv6
		ipv6 := make([]byte, 16)
		if _, err := io.ReadFull(reader, ipv6); err != nil {
			return
		}
		targetHost = net.IP(ipv6).String()
	default:
		_, _ = conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var portBytes [2]byte
	if _, err := io.ReadFull(reader, portBytes[:]); err != nil {
		return
	}
	targetPort := binary.BigEndian.Uint16(portBytes[:])
	destAddr := fmt.Sprintf("%s:%d", targetHost, targetPort)

	// Connect to destination
	destConn, err := net.DialTimeout("tcp", destAddr, 10*time.Second)
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Host unreachable
		return
	}
	defer destConn.Close()

	// Respond Success (0x00)
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	// Bidirectional tunnel
	pipeConnections(conn, destConn)
}

// handleHTTPProxy implements standard HTTP CONNECT tunneling
func handleHTTPProxy(conn net.Conn) {
	defer conn.Close()
	activeConns.Add(1)
	defer activeConns.Add(-1)

	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	if req.Method == http.MethodConnect {
		destConn, err := net.DialTimeout("tcp", req.RequestURI, 10*time.Second)
		if err != nil {
			_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		defer destConn.Close()

		if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return
		}

		pipeConnections(conn, destConn)
	} else {
		// Standard HTTP GET/POST forward
		req.RequestURI = ""
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		defer resp.Body.Close()
		_ = resp.Write(conn)
	}
}

func pipeConnections(a, b net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		buf := make([]byte, 32768)
		for {
			n, err := a.Read(buf)
			if n > 0 {
				_, wErr := b.Write(buf[:n])
				bytesTransferred.Add(int64(n))
				if wErr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}()

	go func() {
		buf := make([]byte, 32768)
		for {
			n, err := b.Read(buf)
			if n > 0 {
				_, wErr := a.Write(buf[:n])
				bytesTransferred.Add(int64(n))
				if wErr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}()

	<-done
}

func main() {
	relayAddr := flag.String("relay", "172.209.217.140:443", "Cloud Edge Relay UDP address")
	serverName := flag.String("servername", "172.209.217.140", "TLS SNI server name")
	socksAddr := flag.String("socks", "127.0.0.1:1080", "Local SOCKS5 proxy bind address")
	httpAddr := flag.String("http", "127.0.0.1:8080", "Local HTTP proxy bind address")
	ticketArg := flag.String("ticket", "", "Path to session ticket file or raw JSON")
	skipVerify := flag.Bool("skip-verify", true, "Skip TLS verification for development relay")
	flag.Parse()

	log.Println("==================================================================")
	log.Println("🛡️  THE AEGIS PROTOCOL v3 — DESKTOP ACCELERATOR (WINDOWS/MACOS/LINUX)")
	log.Printf("🌐 Cloud Edge Relay:  UDP %s", *relayAddr)
	log.Printf("⚡ SOCKS5 Proxy:      socks5://%s", *socksAddr)
	log.Printf("⚡ HTTP Proxy:        http://%s", *httpAddr)
	log.Println("==================================================================")

	// 1. Parse Session Ticket if provided
	var signedTicket *crypto.SignedTicket
	if *ticketArg != "" {
		var raw []byte
		if strings.HasPrefix(*ticketArg, "{") {
			raw = []byte(*ticketArg)
		} else {
			var err error
			raw, err = os.ReadFile(*ticketArg)
			if err != nil {
				log.Printf("[!] Warning: Could not read ticket file: %v", err)
			}
		}
		if len(raw) > 0 {
			st, err := crypto.UnmarshalSignedTicket(raw)
			if err == nil {
				signedTicket = st
				log.Printf("[✓] Session ticket loaded (User: %s, Tier: %s, Valid to: %s)",
					st.Ticket.UserID, st.Ticket.Tier, time.Unix(st.Ticket.ExpiresAt, 0).Format(time.RFC3339))
			} else {
				log.Printf("[!] Warning: Failed to parse ticket: %v", err)
			}
		}
	}

	// 2. Dial Cloud Edge Relay via QUIC Datagrams
	client := transport.NewClient(*relayAddr, *serverName)
	client.SetInsecureSkipVerify(*skipVerify)

	packetConn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		log.Fatalf("[X] Failed to create UDP socket: %v", err)
	}
	defer packetConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("[*] Connecting to Cloud Relay %s over QUIC...", *relayAddr)
	if err := client.Connect(ctx, packetConn); err != nil {
		log.Printf("[!] Note: Could not immediately connect to Cloud Relay: %v. Running in local proxy mode.", err)
	} else {
		log.Println("[✓] Successfully connected to Aegis Cloud Edge Relay!")

		// If ticket is loaded, send Auth Datagram
		if signedTicket != nil {
			ticketBytes, _ := os.ReadFile(*ticketArg)
			if len(ticketBytes) > 0 {
				authMsg := append([]byte{byte(transport.TypeAuthTicket)}, ticketBytes...)
				_ = client.SendDatagram(authMsg)
			}
		}
	}
	defer client.Close()

	// 3. Start SOCKS5 Server
	socksListener, err := net.Listen("tcp", *socksAddr)
	if err != nil {
		log.Fatalf("[X] Failed to bind SOCKS5 listener on %s: %v", *socksAddr, err)
	}
	defer socksListener.Close()

	go func() {
		for {
			conn, err := socksListener.Accept()
			if err != nil {
				return
			}
			go handleSocks5(conn)
		}
	}()
	log.Printf("[✓] Local SOCKS5 Proxy active at %s", *socksAddr)

	// 4. Start HTTP Proxy Server
	httpListener, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		log.Fatalf("[X] Failed to bind HTTP listener on %s: %v", *httpAddr, err)
	}
	defer httpListener.Close()

	go func() {
		for {
			conn, err := httpListener.Accept()
			if err != nil {
				return
			}
			go handleHTTPProxy(conn)
		}
	}()
	log.Printf("[✓] Local HTTP Proxy active at %s", *httpAddr)

	// 5. Cockpit Status Refresh Loop
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			mb := float64(bytesTransferred.Load()) / (1024 * 1024)
			conns := activeConns.Load()
			log.Printf("📊 [Cockpit] Transferred: %.2f MB | Active Sockets: %d", mb, conns)
		}
	}()

	log.Println("==================================================================")
	log.Println("🚀 AEGIS DESKTOP READY! Configure your browser or OS proxy to:")
	log.Printf("   SOCKS5: %s   OR   HTTP: %s", *socksAddr, *httpAddr)
	log.Println("==================================================================")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[*] Aegis Desktop Client shutting down gracefully...")
}
