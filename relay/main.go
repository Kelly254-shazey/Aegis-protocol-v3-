package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"aegis-core/crypto"
	"aegis-core/fec"
	"aegis-core/transport"
	"aegis-core/tun"
)

func generateDevCertificate() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Aegis Protocol Cloud Edge Relay"},
			CommonName:   "relay.aegisprotocol.internal",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
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

// configureLinuxInterface assigns IP and brings up the TUN interface on Linux.
func configureLinuxInterface(devName string, ipCidr string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	// ip addr replace 10.77.77.1/24 dev tun0
	cmdAddr := exec.Command("ip", "addr", "replace", ipCidr, "dev", devName)
	if out, err := cmdAddr.CombinedOutput(); err != nil {
		return fmt.Errorf("ip addr replace failed (%s): %w", string(out), err)
	}

	// ip link set dev tun0 up mtu 1280
	cmdLink := exec.Command("ip", "link", "set", "dev", devName, "up", "mtu", "1280")
	if out, err := cmdLink.CombinedOutput(); err != nil {
		return fmt.Errorf("ip link set up failed (%s): %w", string(out), err)
	}
	return nil
}

type ClientQueue struct {
	mu        sync.Mutex
	destIP    string
	encoder   *fec.Encoder
	batch     [][]byte
	lastFlush time.Time
}

func main() {
	bindAddr := flag.String("bind", ":443", "UDP bind address for QUIC relay")
	certFile := flag.String("cert", "/opt/aegis/cert.pem", "Path to TLS certificate")
	keyFile := flag.String("key", "/opt/aegis/key.pem", "Path to TLS private key")
	tunName := flag.String("tun", "tun0", "TUN device name")
	tunIP := flag.String("tun-ip", "10.77.77.1/24", "Virtual subnet IP assignment for TUN")
	mtu := flag.Int("mtu", 1280, "Tunnel MTU size")
	portalPubB64 := flag.String("portal-pubkey", "", "Base64 Ed25519 public key of Control Portal")
	flag.Parse()

	log.Println("==================================================================")
	log.Println("🛡️  THE AEGIS PROTOCOL v3 — CLOUD EDGE RELAY (BBRv3 + QUIC)")
	log.Printf("🌐 UDP Bind:        %s (RFC 9221 Datagrams)", *bindAddr)
	log.Printf("🔌 TUN Interface:  %s (%s, MTU %d)", *tunName, *tunIP, *mtu)
	log.Println("⚡ Transport:       QUIC Datagrams + Reed-Solomon GF(2^8) FEC")
	log.Println("🔒 Security:        TLS 1.3 / Ed25519 Session Tickets / BBRv3 Blind Forwarder")
	log.Println("==================================================================")

	// 1. Load or Generate TLS Certificate
	tlsCert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		log.Printf("[!] Note: Could not load %s / %s (%v). Bootstrapping dev certificate...", *certFile, *keyFile, err)
		tlsCert, err = generateDevCertificate()
		if err != nil {
			log.Fatalf("[X] Failed to generate fallback TLS certificate: %v", err)
		}
		log.Println("[✓] Dev TLS certificate bootstrapped.")
	}

	// 2. Parse Portal Public Key for Session Ticket Verification
	var portalPubKey ed25519.PublicKey
	if *portalPubB64 != "" {
		pubBytes, err := base64.StdEncoding.DecodeString(*portalPubB64)
		if err == nil && len(pubBytes) == ed25519.PublicKeySize {
			portalPubKey = pubBytes
			log.Println("[✓] Control Portal Ed25519 Public Key loaded for session tickets.")
		} else {
			log.Printf("[!] Warning: Invalid portal public key: %v", err)
		}
	}

	// 3. Allocate Virtual Network Device
	tunDev, err := tun.Create(*tunName, *mtu)
	if err != nil {
		log.Printf("[!] TUN allocation notice: %v. Running in userspace packet bridge mode.", err)
	} else {
		defer tunDev.Close()
		log.Printf("[✓] Virtual Layer 3 Network Device '%s' established.", tunDev.Name())

		if err := configureLinuxInterface(tunDev.Name(), *tunIP); err != nil {
			log.Printf("[!] Note: Could not auto-configure IP on %s: %v (run iptables setup script if on Linux)", tunDev.Name(), err)
		} else {
			log.Printf("[✓] Interface %s configured with IP %s and brought UP.", tunDev.Name(), *tunIP)
		}
	}

	// Telemetry stats
	var bytesUplink atomic.Int64
	var bytesDownlink atomic.Int64
	var packetsRecovered atomic.Int64

	// Downlink Client Queues: clientIP -> *ClientQueue
	var clientQueues sync.Map
	var lastClientAddr sync.Map // clientIP -> net.Addr

	// 4. Server Transport Reference
	var server *transport.Server

	enqueueDownlink := func(destIP string, rawIPv4Packet []byte) {
		val, ok := clientQueues.Load(destIP)
		if !ok {
			enc, _ := fec.NewEncoder(10, 1)
			val, _ = clientQueues.LoadOrStore(destIP, &ClientQueue{
				destIP:    destIP,
				encoder:   enc,
				batch:     make([][]byte, 0, 10),
				lastFlush: time.Now(),
			})
		}

		cq := val.(*ClientQueue)
		cq.mu.Lock()
		cq.batch = append(cq.batch, rawIPv4Packet)
		bytesDownlink.Add(int64(len(rawIPv4Packet)))

		if len(cq.batch) >= 10 || time.Since(cq.lastFlush) > 5*time.Millisecond {
			shards, err := cq.encoder.EncodeBlock(cq.batch)
			if err == nil && server != nil {
				for _, s := range shards {
					wire := s.Encode()
					_ = server.SendToVirtualIP(destIP, wire)
				}
			}
			cq.batch = cq.batch[:0]
			cq.lastFlush = time.Now()
		}
		cq.mu.Unlock()
	}

	nat := NewUserspaceNAT(enqueueDownlink)
	defer nat.Close()

	// 5. FEC Decoder for Incoming Traffic (Mobile -> Cloud Relay -> Internet)
	decoder := fec.NewDecoder(3*time.Second, func(blockID uint32, packets [][]byte) {
		for _, pkt := range packets {
			if len(pkt) == 0 {
				continue
			}

			// Validate IP packet and register route for client
			if len(pkt) >= 20 && pkt[0]>>4 == 4 {
				srcIP := net.IP(pkt[12:16]).String()
				// Track client queue for downlink
				if _, ok := clientQueues.Load(srcIP); !ok {
					enc, _ := fec.NewEncoder(10, 1)
					clientQueues.Store(srcIP, &ClientQueue{
						destIP:    srcIP,
						encoder:   enc,
						batch:     make([][]byte, 0, 10),
						lastFlush: time.Now(),
					})
					log.Printf("[+] Registered active client session for virtual IP: %s", srcIP)
				}
				if val, ok := lastClientAddr.Load("latest"); ok {
					server.RegisterRoute(srcIP, val.(net.Addr))
				}
				if runtime.GOOS == "windows" {
					// Pass packet to Userspace NAT on Windows (where no kernel TUN routing exists)
					nat.IngestPacket(pkt)
				}
			}

			if tunDev != nil {
				_, _ = tunDev.Write(pkt)
			}
			bytesUplink.Add(int64(len(pkt)))
			packetsRecovered.Add(1)
		}
	})

	// 6. QUIC Datagram Server Handler
	server = transport.NewServer(*bindAddr, tlsCert, func(clientAddr net.Addr, payload []byte) {
		if len(payload) == 0 {
			return
		}

		server.RegisterRoute("10.77.77.2", clientAddr)
		lastClientAddr.Store("10.77.77.2", clientAddr)
		lastClientAddr.Store("latest", clientAddr)

		// Check for Auth Ticket
		if payload[0] == byte(transport.TypeAuthTicket) && len(payload) > 1 {
			ticket, err := crypto.UnmarshalSignedTicket(payload[1:])
			if err == nil && portalPubKey != nil {
				st, err := ticket.Verify(portalPubKey)
				if err != nil {
					log.Printf("[!] Client %s failed ticket verification: %v", clientAddr, err)
					return
				}
				log.Printf("[✓] Client %s authenticated (Tier: %s, Device: %s, Valid to: %s)",
					clientAddr, st.Tier, st.DeviceID, time.Unix(st.ExpiresAt, 0).Format(time.RFC3339))
			}
			return
		}

		// Ingest into FEC decoder
		shard, err := fec.DecodeShard(payload)
		if err == nil {
			_ = decoder.Ingest(shard)
		}
	})

	if err := server.Start(); err != nil {
		log.Fatalf("[X] Failed to start Aegis Cloud Relay: %v", err)
	}
	defer server.Close()

	// 6a. HTTP Telemetry Status API on Port 80 (Azure NSG Rule 300)
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			fmt.Fprintf(w, `{"status":"online","protocol":"RFC9221-QUIC","clients":%d,"up_bytes":%d,"down_bytes":%d,"fec_recovered":%d}`+"\n",
				server.ConnectedClientCount(), bytesUplink.Load(), bytesDownlink.Load(), packetsRecovered.Load())
		})
		_ = http.ListenAndServe(":80", mux)
	}()

	// 6b. Periodic Downlink Batch Flush Timer (Sub-5ms response latency)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			clientQueues.Range(func(key, val any) bool {
				cq := val.(*ClientQueue)
				cq.mu.Lock()
				if len(cq.batch) > 0 {
					shards, err := cq.encoder.EncodeBlock(cq.batch)
					if err == nil && server != nil {
						for _, s := range shards {
							wire := s.Encode()
							_ = server.SendToVirtualIP(cq.destIP, wire)
						}
					}
					cq.batch = cq.batch[:0]
					cq.lastFlush = time.Now()
				}
				cq.mu.Unlock()
				return true
			})
		}
	}()

	// 7. Downlink Loop: Read responses from TUN (Internet -> Cloud Relay -> Mobile)
	if tunDev != nil {
		go func() {
			buf := make([]byte, 1500)
			for {
				n, err := tunDev.Read(buf)
				if err != nil {
					return
				}
				if n <= 0 {
					continue
				}

				pkt := make([]byte, n)
				copy(pkt, buf[:n])
				bytesDownlink.Add(int64(n))

				var destIP string
				if n >= 20 && pkt[0]>>4 == 4 {
					destIP = net.IP(pkt[16:20]).String()
				}

				// Look up client queue
				val, ok := clientQueues.Load(destIP)
				if !ok {
					// Fallback: If only 1 client or broadcast
					server.BroadcastDatagram(pkt)
					continue
				}

				cq := val.(*ClientQueue)
				cq.mu.Lock()
				cq.batch = append(cq.batch, pkt)
				if len(cq.batch) >= 10 || time.Since(cq.lastFlush) > 5*time.Millisecond {
					shards, err := cq.encoder.EncodeBlock(cq.batch)
					if err == nil {
						for _, s := range shards {
							wire := s.Encode()
							_ = server.SendToVirtualIP(destIP, wire)
						}
					}
					cq.batch = cq.batch[:0]
					cq.lastFlush = time.Now()
				}
				cq.mu.Unlock()
			}
		}()
	}

	// 8. Background Telemetry Reporter (Every 10 seconds)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			activeClients := server.ConnectedClientCount()
			upMB := float64(bytesUplink.Load()) / (1024 * 1024)
			downMB := float64(bytesDownlink.Load()) / (1024 * 1024)
			rec := packetsRecovered.Load()

			log.Printf("📊 [Telemetry] Clients: %d | Up: %.2f MB | Down: %.2f MB | FEC Recovered: %d packets",
				activeClients, upMB, downMB, rec)
		}
	}()

	log.Println("[✓] Aegis Cloud Relay daemon fully operational. Ready for client traffic.")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[*] Shutting down Aegis Cloud Relay daemon gracefully...")
}
