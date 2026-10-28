#!/bin/bash
# ==============================================================================
# THE AEGIS PROTOCOL v3 — COMPLETE PRODUCTION UPGRADE SCRIPT
# Replaces Version 2 with Version 3 on Ubuntu 22.04 / 24.04 (Azure VM)
# ==============================================================================

set -e

echo "=================================================================="
echo "🛡️  UPGRADING AEGIS CLOUD RELAY TO PROTOCOL v3 (AZURE PRODUCTION)"
echo "=================================================================="

# 1. Stop and Disable Version 2 Services
echo "[1/6] Stopping old Version 2 services and releasing ports..."
systemctl stop aegis aegis-relay aegis-control 2>/dev/null || true
systemctl disable aegis 2>/dev/null || true
if command -v docker &> /dev/null; then
    docker stop $(docker ps -aq) 2>/dev/null || true
    docker rm $(docker ps -aq) 2>/dev/null || true
fi
pkill -9 -f aegis-relay 2>/dev/null || true
pkill -9 -f aegis 2>/dev/null || true
fuser -k -9 443/udp 2>/dev/null || true
fuser -k -9 443/tcp 2>/dev/null || true

# Ensure Linux TUN module is active and clear stale interfaces
modprobe tun 2>/dev/null || true
ip link delete tun0 2>/dev/null || true

# Clean old binaries
rm -rf /opt/aegis_backup 2>/dev/null || true
if [ -d "/opt/aegis" ]; then
    mkdir -p /opt/aegis_backup
    cp -r /opt/aegis/* /opt/aegis_backup/ 2>/dev/null || true
fi
mkdir -p /opt/aegis

# 2. Install Networking and Build Tools
echo "[2/6] Installing dependencies..."
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y git curl iptables iptables-persistent ca-certificates

# Install Go if not present
if ! command -v /usr/local/go/bin/go &> /dev/null && ! command -v go &> /dev/null; then
    echo "[*] Installing Go compiler..."
    curl -fsSL https://go.dev/dl/go1.23.2.linux-amd64.tar.gz -o /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm -f /tmp/go.tar.gz
fi
export PATH=/usr/local/go/bin:$PATH

# 3. Clone and Compile Aegis Protocol v3
echo "[3/6] Fetching latest Aegis Protocol v3 source and compiling..."
rm -rf /tmp/aegis-v3-build
git clone https://github.com/Kelly254-shazey/Aegis-protocol-v3-.git /tmp/aegis-v3-build
cd /tmp/aegis-v3-build
go work sync 2>/dev/null || true
cd /tmp/aegis-v3-build/relay
go build -ldflags="-w -s" -o /opt/aegis/aegis-relay main.go nat.go
chmod +x /opt/aegis/aegis-relay

# Build Control Plane if needed
if [ -d "/tmp/aegis-v3-build/control" ]; then
    cd /tmp/aegis-v3-build/control
    go build -ldflags="-w -s" -o /opt/aegis/aegis-control . 2>/dev/null || true
    chmod +x /opt/aegis/aegis-control 2>/dev/null || true
fi

# Copy portal web assets
if [ -d "/tmp/aegis-v3-build/portal" ]; then
    cp -r /tmp/aegis-v3-build/portal /opt/aegis/
fi

rm -rf /tmp/aegis-v3-build

# 4. Kernel Tuning (BBRv3 + Fast-Path)
echo "[4/6] Applying Linux kernel optimizations..."
cat << 'EOF' > /etc/sysctl.d/99-aegis-relay.conf
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.core.rmem_max = 67108864
net.core.wmem_max = 67108864
net.ipv4.tcp_rmem = 4096 87380 67108864
net.ipv4.tcp_wmem = 4096 65536 67108864
net.core.netdev_max_backlog = 10000
net.ipv4.tcp_syncookies = 1
EOF
sysctl --system > /dev/null 2>&1 || true

# 5. Linux iptables NAT Masquerade
echo "[5/6] Configuring WAN routing for virtual tunnel (10.77.77.0/24)..."
WAN_INT=$(ip route | grep default | awk '{print $5}' | head -n1)
if [ -z "$WAN_INT" ]; then WAN_INT="eth0"; fi

iptables -t nat -C POSTROUTING -s 10.77.77.0/24 -o "$WAN_INT" -j MASQUERADE 2>/dev/null || \
iptables -t nat -A POSTROUTING -s 10.77.77.0/24 -o "$WAN_INT" -j MASQUERADE

iptables -C FORWARD -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || \
iptables -A FORWARD -m state --state RELATED,ESTABLISHED -j ACCEPT

iptables -C FORWARD -s 10.77.77.0/24 -o "$WAN_INT" -j ACCEPT 2>/dev/null || \
iptables -A FORWARD -s 10.77.77.0/24 -o "$WAN_INT" -j ACCEPT

netfilter-persistent save > /dev/null 2>&1 || true

# 6. Install & Start Systemd Service
echo "[6/6] Registering and starting aegis-relay.service..."
cat << 'EOF' > /etc/systemd/system/aegis-relay.service
[Unit]
Description=Aegis Protocol v3 Cloud Edge Relay (QUIC Datagrams + BBRv3)
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/aegis
ExecStart=/opt/aegis/aegis-relay -bind :443 -tun tun0 -tun-ip 10.77.77.1/24 -mtu 1280
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now aegis-relay
sleep 2

echo "=================================================================="
echo "🎉 AEGIS PROTOCOL v3 RELAY STATUS"
echo "=================================================================="
systemctl status aegis-relay --no-pager || true
echo "------------------------------------------------------------------"
echo "📋 RECENT DAEMON LOGS:"
journalctl -u aegis-relay -n 25 --no-pager
