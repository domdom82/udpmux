#!/usr/bin/env bash
# vpn-double-client integration test.
#
# Topology:
#   [client-net]   client (172.20.0.10) + udp-proxy (172.20.0.20)
#   [transit-net]  udp-proxy (172.21.0.20) + udp-mux (172.21.0.30)
#   [server-net]   udp-mux (172.22.0.30) + vpn-server (172.22.0.40)
#                  + vpn-client2 (172.22.0.50) — connects to vpn-server directly
#
# iperf3 path: client → (VPN tun) → vpn-server → (VPN tun) → vpn-client2 → iperf3-server
#
# Environment variables:
#   MTU  - network MTU (default: 1500)
#   BW   - tc bandwidth rate (default: 1gbit)
#   THRESHOLD_MBPS - minimum acceptable throughput in Mbit/s (default: 300)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="$SCRIPT_DIR/../common"
REPO_ROOT="$SCRIPT_DIR/../../.."
PKI_DIR="$COMMON/pki/gen"
VERSION="$(cat "$REPO_ROOT/VERSION")"
THRESHOLD="${THRESHOLD_MBPS:-300}"
SFX="dc"  # suffix to namespace Docker resources for this test

# shellcheck source=../common/lib.sh
source "$COMMON/lib.sh"

TMPDIR="$(mktemp -d)"

cleanup() {
  echo "--- Collecting logs: vpn-double-client ---"
  collect_logs "vpn-double-client" \
    "e2e-vpn-server-$SFX:/var/log/openvpn/vpn-server.log" \
    "e2e-vpn-client-$SFX:/var/log/openvpn/vpn-client1.log" \
    "e2e-vpn-client2-$SFX:/var/log/openvpn/vpn-client2.log" \
    "e2e-udp-proxy-$SFX:docker" \
    "e2e-udp-mux-$SFX:docker"
  echo "--- Cleaning up vpn-double-client ---"
  stop_containers \
    "e2e-vpn-client-$SFX" \
    "e2e-vpn-client2-$SFX" \
    "e2e-udp-proxy-$SFX" \
    "e2e-udp-mux-$SFX" \
    "e2e-vpn-server-$SFX"
  remove_networks "$SFX"
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

title "vpn-double-client: MTU=$MTU BW=$BW threshold=${THRESHOLD} Mbit/s"

# Render OpenVPN configs from templates.
render_configs "$TMPDIR"

# ---------------------------------------------------------------------------
# Networks
# ---------------------------------------------------------------------------
create_networks "$SFX"

# ---------------------------------------------------------------------------
# Containers
# ---------------------------------------------------------------------------

# VPN server — on server-net only.
docker run -d \
  --name "e2e-vpn-server-$SFX" \
  --hostname vpn-server \
  --network "e2e-server-net-$SFX" \
  --ip 172.22.0.40 \
  --cap-add NET_ADMIN \
  -v "$PKI_DIR:/pki:ro" \
  -v "$TMPDIR/server.conf:/etc/openvpn/server.conf:ro" \
  -v "$COMMON/openvpn/ccd:/etc/openvpn/ccd:ro" \
  local/vpn-node:latest \
  sleep infinity

# udp-mux — primary leg on transit-net; second leg (server-net) added below.
docker run -d \
  --name "e2e-udp-mux-$SFX" \
  --hostname udp-mux \
  --network "e2e-transit-net-$SFX" \
  --ip 172.21.0.30 \
  --cap-add NET_ADMIN \
  "local/udp-mux:$VERSION" \
  --listenAddr :8080

# udp-proxy — primary leg on client-net; second leg (transit-net) added below.
docker run -d \
  --name "e2e-udp-proxy-$SFX" \
  --hostname udp-proxy \
  --network "e2e-client-net-$SFX" \
  --ip 172.20.0.20 \
  --cap-add NET_ADMIN \
  "local/udp-proxy:$VERSION" \
  --listenAddr :7070 \
  --muxAddr 172.21.0.30:8080 \
  --endpointAddr 172.22.0.40:1194

# Client (first OpenVPN client) — on client-net only; reaches VPN via udp-proxy.
docker run -d \
  --name "e2e-vpn-client-$SFX" \
  --hostname client \
  --network "e2e-client-net-$SFX" \
  --ip 172.20.0.10 \
  --cap-add NET_ADMIN \
  -v "$PKI_DIR:/pki:ro" \
  -v "$TMPDIR/client1.conf:/etc/openvpn/client1.conf:ro" \
  local/vpn-node:latest \
  sleep infinity

# Client2 (second OpenVPN client) — on server-net; connects directly to vpn-server.
# iperf3 server runs here.
docker run -d \
  --name "e2e-vpn-client2-$SFX" \
  --hostname client2 \
  --network "e2e-server-net-$SFX" \
  --ip 172.22.0.50 \
  --cap-add NET_ADMIN \
  -v "$PKI_DIR:/pki:ro" \
  -v "$TMPDIR/client2.conf:/etc/openvpn/client2.conf:ro" \
  local/vpn-node:latest \
  sleep infinity

# Connect second legs.
docker network connect --ip 172.22.0.30 "e2e-server-net-$SFX"  "e2e-udp-mux-$SFX"
docker network connect --ip 172.21.0.20 "e2e-transit-net-$SFX" "e2e-udp-proxy-$SFX"

# ---------------------------------------------------------------------------
# MTU + bandwidth shaping
# ---------------------------------------------------------------------------
set_mtu "e2e-vpn-server-$SFX"   eth0
set_mtu "e2e-vpn-client-$SFX"   eth0
set_mtu "e2e-vpn-client2-$SFX"  eth0

shape_bw "e2e-vpn-server-$SFX"   eth0
shape_bw "e2e-vpn-client-$SFX"   eth0
shape_bw "e2e-vpn-client2-$SFX"  eth0
shape_bw "e2e-udp-proxy-$SFX"    eth0
shape_bw "e2e-udp-proxy-$SFX"    eth1
shape_bw "e2e-udp-mux-$SFX"      eth0
shape_bw "e2e-udp-mux-$SFX"      eth1

# ---------------------------------------------------------------------------
# Start OpenVPN
# ---------------------------------------------------------------------------
mknod_tun "e2e-vpn-server-$SFX"
mknod_tun "e2e-vpn-client-$SFX"
mknod_tun "e2e-vpn-client2-$SFX"

docker exec "e2e-vpn-server-$SFX" sysctl -w net.ipv4.ip_forward=1

docker exec -d "e2e-vpn-server-$SFX"   bash -c "openvpn /etc/openvpn/server.conf > /var/log/openvpn/vpn-server.log 2>&1"
docker exec -d "e2e-vpn-client-$SFX"       bash -c "openvpn /etc/openvpn/client1.conf > /var/log/openvpn/vpn-client1.log 2>&1"
docker exec -d "e2e-vpn-client2-$SFX"  bash -c "openvpn /etc/openvpn/client2.conf > /var/log/openvpn/vpn-client2.log 2>&1"

# Wait for both VPN tunnels to come up.
wait_tun "e2e-vpn-client-$SFX"      60
wait_tun "e2e-vpn-client2-$SFX" 60

# Discover the VPN IP assigned to vpn-client2.
CLIENT2_VPN_IP=$(get_tun_ip "e2e-vpn-client2-$SFX")
echo "vpn-client2 VPN IP: $CLIENT2_VPN_IP"

# ---------------------------------------------------------------------------
# iperf3 throughput test (TCP, 10 s)
#   iperf3 server: vpn-client2 (reachable via two VPN hops from client)
#   iperf3 client: e2e-vpn-client
# ---------------------------------------------------------------------------
docker exec -d "e2e-vpn-client2-$SFX" bash -c "iperf3 -s > /var/log/iperf3-server.log 2>&1"
sleep 2  # give iperf3 server a moment to start listening

echo "Running iperf3: client → $CLIENT2_VPN_IP (10 s TCP) ..."
MBPS=$(run_iperf3 "e2e-vpn-client-$SFX" "$CLIENT2_VPN_IP" 10)
echo "Result: ${MBPS} Mbit/s  (threshold: ${THRESHOLD} Mbit/s)"

if [ "$MBPS" -ge "$THRESHOLD" ]; then
  echo "PASS: vpn-double-client"
  exit 0
else
  echo "FAIL: vpn-double-client — throughput ${MBPS} Mbit/s is below ${THRESHOLD} Mbit/s"
  exit 1
fi
