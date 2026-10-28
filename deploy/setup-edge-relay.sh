#!/bin/bash
# ==============================================================================
# THE AEGIS PROTOCOL v3 — CLOUD EDGE RELAY PRODUCTION PROVISIONER
# Target: Ubuntu 22.04 / 24.04 LTS (Azure / AWS / Hetzner / DigitalOcean)
# Configures: Google BBRv3, RFC 9221 QUIC UDP 443, iptables MASQUERADE & systemd
# ==============================================================================

set -euo pipefail

echo "🛡️  [Aegis v3] Provisioning Production Cloud Edge Relay..."

# 1. Update and install required networking tools
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y iptables iptables-persistent curl ufw ca-certificates

# 2. Kernel Network Hardening & Google BBR Fast-Path
echo "[*] Optimizing Linux network stack with BBR and fq qdisc..."
cat << 'EOF' > /etc/sysctl.d/99-aegis-relay.conf
# Core Routing & BBR
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr

# Buffer Tuning for High-Bandwidth Undersea Links (100+ Mbps at 200ms RTT)
net.core.rmem_max = 67108864
net.core.wmem_max = 67108864
net.ipv4.tcp_rmem = 4096 87380 67108864
net.ipv4.tcp_wmem = 4096 65536 67108864
net.core.netdev_max_backlog = 10000

# Anti-DDoS & Syn Flood Defense
net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_rfc1337 = 1
EOF
sysctl --system

# 3. Detect default WAN interface
WAN_INT=$(ip route | grep default | awk '{print $5}' | head -n1)
if [ -z "$WAN_INT" ]; then
    WAN_INT="eth0"
fi
echo "[*] Default WAN interface detected: $WAN_INT"

# 4. Configure Firewall & NAT Masquerade
echo "[*] Setting up iptables NAT rules for virtual subnet (10.77.77.0/24)..."
iptables -t nat -C POSTROUTING -s 10.77.77.0/24 -o "$WAN_INT" -j MASQUERADE 2>/dev/null || \
iptables -t nat -A POSTROUTING -s 10.77.77.0/24 -o "$WAN_INT" -j MASQUERADE

iptables -C FORWARD -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || \
iptables -A FORWARD -m state --state RELATED,ESTABLISHED -j ACCEPT

iptables -C FORWARD -s 10.77.77.0/24 -o "$WAN_INT" -j ACCEPT 2>/dev/null || \
iptables -A FORWARD -s 10.77.77.0/24 -o "$WAN_INT" -j ACCEPT

# Save iptables rules across reboots
netfilter-persistent save

# 5. Setup Aegis Relay Systemd Service
echo "[*] Installing systemd daemon: aegis-relay.service..."
cat << 'EOF' > /etc/systemd/system/aegis-relay.service
[Unit]
Description=Aegis Protocol v3 Cloud Edge Relay (QUIC Datagrams + BBRv3)
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/aegis
ExecStart=/opt/aegis/aegis-relay -bind :443 -cert /opt/aegis/cert.pem -key /opt/aegis/key.pem
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
mkdir -p /opt/aegis

echo "=================================================================="
echo "🎉 AEGIS v3 CLOUD EDGE RELAY PROVISIONING COMPLETE!"
echo "Place 'aegis-relay' binary in /opt/aegis/ and run:"
echo "  systemctl enable --now aegis-relay"
echo "=================================================================="
