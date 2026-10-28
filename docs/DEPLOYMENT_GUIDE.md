# Aegis Protocol v3 — Production Deployment Guide

This guide details the step-by-step instructions for provisioning and operating an enterprise Aegis Protocol v3 Cloud Edge Relay and Control Plane on cloud infrastructure (Hetzner, DigitalOcean, Azure, AWS).

---

## 1. Prerequisites & Server Sizing

| Workload Tier | CPU | RAM | Network Port | Recommended Cloud Providers |
| :--- | :--- | :--- | :--- | :--- |
| **Small Edge (Up to 250 Devices)** | 2 vCPU | 2 GB | 1 Gbps | Hetzner Cloud (CPX11), DigitalOcean Droplet |
| **Mid Edge (Up to 2,000 Devices)** | 4 vCPU | 8 GB | 2.5 Gbps | Hetzner Cloud (CPX31), Azure D4s_v5 |
| **Enterprise Core (10,000+ Devices)**| 16 vCPU | 32 GB | 10 Gbps | Bare Metal / AWS c6in.4xlarge |

- **OS**: Ubuntu 22.04 LTS or Ubuntu 24.04 LTS.
- **Firewall Ports**:
  - `443/UDP`: QUIC Datagram Overlay Relay.
  - `8080/TCP`: Control Plane & Activation Portal (or proxy via port 443 with Nginx/Caddy).
  - `80/TCP`: Let's Encrypt ACME HTTP validation.

---

## 2. Automated Single-Script Provisioning

1. SSH into the clean Ubuntu server as `root`:
   ```bash
   ssh root@<SERVER_IP>
   ```

2. Download and run the automated provisioning script:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/Kelly254-shazey/Aegis-protocol-v3-/main/deploy/setup-edge-relay.sh -o setup.sh
   chmod +x setup.sh
   ./setup.sh
   ```

What the script provisions:
- Enables `net.ipv4.ip_forward = 1` and `net.ipv6.conf.all.forwarding = 1`.
- Configures Google BBRv3 congestion control with `net.core.default_qdisc = fq`.
- Sets 64 MB kernel socket buffer ceilings for 100+ Mbps throughput over 200ms links.
- Configures persistent `iptables` NAT Masquerade on the default WAN interface for virtual subnet `10.77.77.0/24`.
- Installs `aegis-relay.service` and `aegis-control.service` systemd units.

---

## 3. Compiling & Deploying Binaries

### Option A: Direct Native Systemd Binaries

1. On your build workstation (or on the server if Go is installed), compile Linux amd64 binaries:
   ```bash
   # Build Relay
   cd relay
   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-w -s" -o aegis-relay .

   # Build Control Plane
   cd ../control
   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-w -s" -o aegis-control .
   ```

2. Transfer the binaries and portal assets to the server:
   ```bash
   scp relay/aegis-relay root@<SERVER_IP>:/opt/aegis/aegis-relay
   scp control/aegis-control root@<SERVER_IP>:/opt/aegis/aegis-control
   scp -r portal root@<SERVER_IP>:/opt/aegis/portal
   ```

3. Configure Let's Encrypt TLS Certificate for your domain:
   ```bash
   apt-get install -y certbot
   certbot certonly --standalone -d relay.yourdomain.com
   cp /etc/letsencrypt/live/relay.yourdomain.com/fullchain.pem /opt/aegis/cert.pem
   cp /etc/letsencrypt/live/relay.yourdomain.com/privkey.pem /opt/aegis/key.pem
   ```

4. Enable and start the systemd services:
   ```bash
   chmod +x /opt/aegis/aegis-relay /opt/aegis/aegis-control
   systemctl enable --now aegis-relay
   systemctl enable --now aegis-control
   ```

5. Check live status:
   ```bash
   systemctl status aegis-relay
   systemctl status aegis-control
   journalctl -u aegis-relay -f
   ```

---

### Option B: Docker Compose Deployment

If deploying with Docker Compose:

1. Clone repository on the server:
   ```bash
   git clone https://github.com/Kelly254-shazey/Aegis-protocol-v3-.git /opt/aegis-repo
   cd /opt/aegis-repo/deploy
   ```

2. Start the multi-container stack:
   ```bash
   docker compose up -d --build
   ```

3. Monitor logs:
   ```bash
   docker compose logs -f
   ```

---

## 4. Verification & Health Checklist

### Check UDP Port 443 Listening
```bash
ss -ulnp | grep 443
```
Output must show `aegis-relay` listening on `*:443`.

### Verify TUN Network Device
```bash
ip addr show tun0
```
Output must show `tun0` with `inet 10.77.77.1/24 scope global tun0`.

### Verify Google BBR
```bash
sysctl net.ipv4.tcp_congestion_control
```
Output: `net.ipv4.tcp_congestion_control = bbr`.

### Test Control Plane API
```bash
curl -s http://127.0.0.1:8080/api/v3/status | jq .
```
Expected output:
```json
{
  "status": "healthy",
  "version": "3.0.0",
  "timestamp": 1793132800
}
```
