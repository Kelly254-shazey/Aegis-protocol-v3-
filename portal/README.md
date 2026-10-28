# Aegis Protocol v3 — Customer Activation Portal

The Aegis Customer Activation Portal is a responsive, standalone Single Page Application (SPA) designed for zero-friction user onboarding, tier selection, Safaricom M-Pesa STK push billing, and instant cryptographic device pairing.

## Architecture

- **Frontend**: Pure HTML5, CSS3 (glassmorphic dark cyber theme), and vanilla JavaScript.
- **Backend**: Served directly by `control/main.go` on port `:8080` (or reverse-proxied via Nginx / Cloudflare).
- **Billing**: Integrated with Safaricom Daraja API (`/api/v3/pay/mpesa`) for automated mobile money transactions in East Africa.
- **Pairing**: Generates Ed25519-signed session tickets encoded into dynamic QR codes for phone/laptop pairing.

## Quick Start

1. Start the Aegis Control Plane:
   ```bash
   cd control
   ./bin/aegis-control -http :8080 -portal-dir ../portal
   ```
2. Navigate to `http://localhost:8080` in any web browser.
3. Select plan (Solo, Duo, or Home Mesh), enter phone number, and trigger instant activation.
