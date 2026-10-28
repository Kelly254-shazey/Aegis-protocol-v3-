# Aegis Protocol v3 — Control Plane API Specification

The Aegis Control Plane provides high-speed, database-free REST endpoints for client activation, Safaricom M-Pesa STK push billing, laptop QR pairing challenges, and real-time health telemetry.

- **Base URL**: `http://<CONTROL_IP>:8080` (or `https://portal.aegisprotocol.internal`)
- **Transport**: JSON over HTTP/1.1 or HTTP/2
- **Authentication**: Ed25519 Cryptographic Signatures

---

## 1. Endpoints

### 1.1 Initiate M-Pesa STK Push / Issue Activation Pass
Initiates mobile money billing via Safaricom Daraja API and issues a 30-day signed Ed25519 Session Ticket.

- **Path**: `POST /api/v3/pay/mpesa`
- **Headers**: `Content-Type: application/json`

#### Request Body
```json
{
  "phone": "254712345678",
  "plan": "duo",
  "amount": 2500
}
```

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `phone` | `string` | Yes | Safaricom phone number in `2547...` or `07...` format. |
| `plan` | `string` | Yes | Subscription tier: `"solo"` (1 dev), `"duo"` (2 devs), or `"home"` (5 devs). |
| `amount` | `integer`| Optional | Billed amount in Kenya Shillings (KES). |

#### Response (200 OK)
```json
{
  "status": "activated",
  "message": "Shield pass issued. Ready to connect.",
  "pass": {
    "t": {
      "uid": "user_254712345678",
      "did": "dev_primary",
      "tier": "duo",
      "max_dev": 2,
      "iat": 1793132800,
      "exp": 1795724800
    },
    "sig": "MEQCID7xY2UsS4Z...base64_ed25519_signature...=="
  },
  "pubKey": "base64_portal_public_key=="
}
```

---

### 1.2 Generate Laptop Pairing QR Challenge
Generates a time-limited (120-second) cryptographic pairing challenge for linking secondary devices (e.g. laptops) under Duo Pass or Home Mesh plans.

- **Path**: `POST /api/v3/device/pair-qr`
- **Headers**: `Content-Type: application/json`

#### Response (200 OK)
```json
{
  "challenge": "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=",
  "expiresIn": "120s",
  "qrPayload": "aegis://pair?c=47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=&ts=1793132800"
}
```

---

### 1.3 Service Health Telemetry
Returns operational health, software version, and UNIX timestamp.

- **Path**: `GET /api/v3/status`

#### Response (200 OK)
```json
{
  "status": "healthy",
  "version": "3.0.0",
  "timestamp": 1793132800
}
```

---

## 2. Ed25519 Session Ticket Canonical Format

The session ticket is verified by the Cloud Edge Relay in memory (`< 1 µs` per handshake) without needing database access.

### Signed Ticket Schema (`SignedTicket`)

```json
{
  "t": {
    "uid": "string",
    "did": "string",
    "tier": "solo | duo | home",
    "max_dev": 1,
    "iat": 1793132800,
    "exp": 1795724800
  },
  "sig": "string (Base64 Ed25519 signature of canonical JSON of 't')"
}
```
