# Aegis Core (`core/`)

The shared networking engine for the Aegis Protocol. Written in pure Go, compiled to:
- Android `.aar` library via `gomobile bind` (for the Android client)
- iOS `.xcframework` via `gomobile bind -target=ios` (for iOS client)
- Native Linux binary / package (for the Cloud Edge Relay)

---

## Package Architecture

```
core/
├── fec/           # Forward Error Correction (Reed-Solomon erasure coding)
│   ├── encoder.go # Shards outgoing IP packets into K data + M parity shards
│   ├── decoder.go # Reconstructs missing packets in-memory (Galois Field matrix solver)
│   └── adaptive.go# Dynamically shifts (K, M) ratio (10:1 to 6:4) based on telemetry
│
├── transport/     # QUIC Datagram Tunnel (RFC 9000 & RFC 9221)
│   ├── client.go  # Mobile client transport (outbound UDP Port 443)
│   ├── server.go  # Cloud relay listener & multiplexer
│   └── session.go # Stateless QUIC Connection ID migration (0ms dropouts)
│
├── telemetry/     # Radio & Link Telemetry Sensors
│   ├── observer.go# Common interface for telemetry inputs
│   ├── cellular.go# Cellular RSRP/RSRQ/SNR metric listener (Android bridge)
│   └── starlink.go# Optional Starlink gRPC poller (192.168.100.1:9200)
│
├── tunnel/        # Layer 3 Virtual Network Interface (TUN) Bridge
│   ├── tun_android.go # Pass-through for Android VpnService FileDescriptor
│   ├── tun_linux.go   # Native Linux tun0 allocation
│   └── loop.go        # Bidirectional packet read/write worker loop
│
└── crypto/        # Ed25519 Session Licensing & Ephemeral Keys
    ├── auth.go    # In-memory ticket validation (<1µs)
    └── cert.go    # Device public key signing
```
