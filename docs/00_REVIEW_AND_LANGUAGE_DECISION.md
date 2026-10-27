# Aegis v3 – Phase 0: Review, Corrections & Language Decision

Date: 2026-10-28
Status: For review, before any setup or code

---

## 1. Language decision

### 1.1 What we use for each part

| Part | Language | Why |
|---|---|---|
| **Core engine** (tunnel, FEC, QUIC transport, crypto) | **Go** | The libraries we need are already production-grade in Go: `quic-go` (QUIC + RFC 9221 datagrams), `connect-ip-go` (MASQUE / RFC 9484 IP proxying), `klauspost/reedsolomon` (SIMD-accelerated FEC) and `wireguard-go` (the official WireGuard Android app runs on it too). `gomobile` compiles the same code for Android (`.aar`) and iOS (`.xcframework`). |
| **Relay server** | **Go** | It's the same module as the core, so the phone and the server run identical encoding and decoding code. That rules out a whole class of mismatch bugs. |
| **Control plane** (accounts, M-Pesa/Stripe webhooks, token signing) | **Go** | One backend language to learn, deploy and secure. Daraja and Stripe are plain REST and webhooks, so no Node-specific SDK is needed. |
| **Android app** (UI, `VpnService`, permissions) | **Kotlin** | Google's primary language for Android. The `VpnService` and `ConnectivityManager` APIs are Java/Kotlin APIs, and Kotlin calls the Go `.aar` directly. |
| **Web portal** (later) | TypeScript | Browser UI only. It talks to the Go control-plane API. |
| **iOS app** (later) | Swift | Required for `NEPacketTunnelProvider`. It reuses the Go core. |

### 1.2 Alternatives considered

| Option | Verdict |
|---|---|
| **Rust** (quinn, boringtun, raptorq) | Strong choice: no garbage collector and a smaller memory footprint, which matters for iOS. Rejected for now because the learning curve is steeper, MASQUE CONNECT-IP support is less mature, and mobile bindings (uniffi) take more work. We can revisit it if Go's memory use on iOS becomes a blocker. |
| **Node.js / TypeScript for the data plane** | Rejected. v2 showed the problem: a single-threaded event loop handling packets at Layer 7 can't carry real IP traffic at Mbps rates. |
| **Java only (as in v2)** | Rejected for the core. There's no mature QUIC-datagram or MASQUE stack, and the code couldn't be shared with the relay or with iOS. |

### 1.3 Known costs of choosing Go
- The Go runtime adds roughly 8–15 MB to the APK.
- Garbage-collector pauses: buffers will be pooled (`sync.Pool`) and GC tuned (`debug.SetMemoryLimit`).
- `gomobile` only exposes simple types across the boundary, so the Go↔Kotlin API has to stay small: start, stop, a pass-through for the TUN file descriptor, and stats.

---

## 2. Corrections to the earlier discussion (please read)

Some things said in the design conversation were stated too strongly. These corrections are now part of the requirements.

| # | Earlier claim | Correction |
|---|---|---|
| C1 | "FEC gives 0% drops, and a 1-bar border signal becomes fast" | FEC **cannot create capacity** (the Shannon limit). It recovers *random* loss without waiting for a retransmission. On a weak uplink, 40% parity uses up 40% more of an already scarce channel. If the loss comes from congestion, extra parity makes things **worse**. The realistic goal is **fewer stalls and smoother calls on lossy links**, not "zero drops". |
| C2 | "Pick FEC parity from the radio signal (RSRP)" | RSRP is only a hint. The main input must be **packet loss and RTT measured by the tunnel itself**, which needs no extra permissions. Reading cell info needs `ACCESS_FINE_LOCATION` at runtime and draws Play Store policy scrutiny. |
| C3 | "Ed25519 token check on every packet in 0.1 µs" | The token is verified **once per session** at handshake (tens of microseconds). After that, packets are authenticated by the session's AEAD keys (ChaCha20-Poly1305 / AES-GCM). |
| C4 | "0 ms handover" | QUIC connection migration avoids a **session reset and re-handshake**. There is still a short physical gap while the radio switches. The target is "no reconnect", not "zero gap". |
| C5 | "\$20 server = 1,500 users, 85%+ margin" | **Unsupported.** Bandwidth (egress), not the VM, is the main cost. Hyperscalers like Azure/AWS/GCP charge roughly \$0.05–0.09 per GB of egress. One user streaming 100 GB a month can cost **more than a \$3–5 subscription**. Relays need providers with bundled traffic, a fair-use policy, or data caps per plan. This needs a real cost model (Phase 0 task). |
| C6 | "Hotspot sharing with WPAD auto-discovery" | On a non-rooted Android phone, an app **can't control the hotspot's DHCP/DNS**, so WPAD can't be injected. Hotspot traffic also doesn't pass through `VpnService` by default. Realistic options: (a) **desktop client on the laptop** (Duo Pass, recommended), or (b) manual proxy settings on the tethered device. |
| C7 | "Tunnel through zero-rated carrier sites / DNS / SNI tricks when you have no balance" | **Removed from scope.** That means taking carrier service without paying for it: legal and fraud risk for the business. Aegis always rides on a connection the user already has. |
| C8 | "Traffic disguised as HTTPS can't be seen or blocked" | Using UDP/443 helps get through hotel and airport firewalls. It **doesn't guarantee** the traffic can't be detected or blocked. We don't promise invisibility to customers. |
| C9 | "Satellite solves location" | Direct-to-Cell is carrier-partnered, limited in data, and not available everywhere yet. Aegis **runs unchanged** on top of it when it's present, but it's **not a dependency**. With no tower, no satellite and no Wi-Fi, nothing can connect. |
| C10 | "iOS network extension limit is 15 MB" | That was the historical limit. Newer iOS versions allow more (around 50 MB), but it's still tight. We keep the Go core's memory small from day one. |

---

## 3. Security debt in v2 (must not carry over)

Found in `C:\Users\w\Projects\The-Aegis-Protocol-v2`:

| Problem | Location | Action |
|---|---|---|
| WireGuard **server and client private keys + PSK hard-coded** | `cloud-server/setup-wireguard-relay.sh`, `android-native/.../MainActivity.java` | Treat as **compromised**. Shut down or re-key the relay at `172.209.217.140`. Never reuse these keys. |
| Shared HMAC "mesh secret" hard-coded | `laptop_ingress_provider.js` | Treat as compromised. |
| **Trust-all TLS** (accepts any certificate and hostname) | `MainActivity.java` (config sync), `rejectUnauthorized: false` in Node | Open to man-in-the-middle attacks. v3 uses proper TLS plus pinned relay keys. |
| One shared client identity for all users | Embedded config | v3: **keys generated per device, on the device**. The private key never leaves the phone. |

**v3 rule:** no secrets in git. Use `.gitignore`, environment variables and per-device key generation.

---

## 4. Toolchain status on this PC (checked 2026-10-28)

| Tool | Status | Needed for |
|---|---|---|
| Git 2.54 | ✅ Installed | Version control |
| Java 21 (JDK) | ✅ Installed | Android build |
| Node 24 | ✅ Installed | Web portal only (later) |
| Android SDK (`C:\Users\w\Android\Sdk`) | ✅ build-tools, platforms, platform-tools | Android build |
| `adb` | ⚠️ Present in SDK but **not on PATH** | Installing on phone |
| **Go** | ❌ **Missing** | Core, relay, control plane |
| **Android NDK** | ❌ **Missing** | `gomobile bind` for Android |
| **Gradle / Android Studio** | ❌ Missing | v2 used a hand-written aapt2/d8 script. v3 should use Gradle because we'll depend on `.aar` libraries. |
| `gomobile` | ❌ Missing (installed via Go) | Go → Android `.aar` |

---

## 5. Proposed folder layout (created in the setup step, after approval)

```
Aegis-v3/
├── README.md
├── docs/            Requirements, decisions, measurements
├── core/            Go module: tunnel, fec, transport, session auth (shared)
├── relay/           Go: cloud edge relay server (Linux)
├── control/         Go: accounts, payments webhooks, token signing API
├── android/         Kotlin: Gradle app, VpnService, UI
├── portal/          TypeScript: customer web portal (later)
└── deploy/          Relay provisioning scripts (no secrets)
```

---

## 6. Phase 0 tasks (analysis before code)

1. **Measure the problem first.** On a real Kenyan mobile connection (strong signal *and* weak signal), record baseline RTT, packet loss and throughput: with no VPN, then with plain WireGuard to a relay. If loss on the tunnel stays below about 1%, FEC adds little and we should prioritise transport and migration instead. This data justifies (or rules out) the FEC work.
2. **Relay cost model.** Pick 2–3 hosting providers, compare price per TB of egress, and set plan data limits so every tier has a positive margin.
3. **Legal check.** Confirm with the Communications Authority of Kenya and/or a lawyer whether a paid VPN/OTT service needs a licence, and register with the ODPC (Data Protection Act 2019), because we'll hold phone numbers and payment records.
4. **Install toolchain.** Go, Android Studio (Gradle + NDK), `gomobile`, and `adb` on PATH.

---

## 7. Decisions needed from you

1. Do you approve the language choices in §1?
2. Folder layout in §5: OK as-is?
3. Should we do the **Phase 0 measurement** (§6.1) before writing the FEC code? (Recommended.)
4. Was the old relay `172.209.217.140` shut down or re-keyed?
