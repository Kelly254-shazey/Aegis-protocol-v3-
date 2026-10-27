package transport

const (
	// DefaultPort is UDP 443 (standard HTTPS/QUIC port)
	DefaultPort = 443

	// DefaultMTU is the IPv6 minimum guarantee to prevent carrier fragmentation
	DefaultMTU = 1280

	// ALPN identifies our QUIC protocol negotiation string
	ALPN = "aegis-h3"
)

// DatagramType prefixes each raw QUIC datagram packet.
type DatagramType byte

const (
	// TypeData carries an encrypted Layer 3 IP packet or FEC shard
	TypeData DatagramType = 0x01

	// TypeHeartbeat is sent every 15s to keep mobile carrier CGNAT pinholes open
	TypeHeartbeat DatagramType = 0x02

	// TypeAuthTicket delivers the Ed25519 customer session token at handshake
	TypeAuthTicket DatagramType = 0x03

	// TypeTelemetry delivers radio quality indicators (RSRP/SNR)
	TypeTelemetry DatagramType = 0x04
)
