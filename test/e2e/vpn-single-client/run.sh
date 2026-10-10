#!/usr/bin/env bash
# vpn-single-client integration test.
#
# Topology:
#   [client-net]  client (172.20.0.10) + udp-proxy (172.20.0.20)
#   [transit-net] udp-proxy (172.21.0.20) + udp-mux (172.21.0.30)
#   [server-net]  udp-mux (172.22.0.30)   + vpn-server (172.22.0.40)
#
# The client can only reach the VPN server via udp-proxy → udp-mux.
#
# Environment variables:
#   MTU  - network MTU (default: 1500)
#   BW   - tc bandwidth rate (default: 1gbit)
#   THRESHOLD_MBPS - minimum acceptable throughput in Mbit/s (default: 400)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="$SCRIPT_DIR/../common"
REPO_ROOT="$SCRIPT_DIR/../../.."
PKI_DIR="$COMMON/pki/gen"
VERSION="$(cat "$REPO_ROOT/VERSION")"
THRESHOLD="${THRESHOLD_MBPS:-400}"
SFX="sc"  # suffix to namespace Docker resources for this test

# shellcheck source=../common/lib.sh
source "$COMMON/lib.sh"

TMPDIR="$(mktemp -d)"

cleanup() {
  echo "--- Collecting logs: vpn-single-client ---"
  collect_logs "vpn-single-client" \
    "e2e-vpn-server-$SFX:/var/log/openvpn/vpn-server.log" \
    "e2e-vpn-client-$SFX:/var/log/openvpn/vpn-client1.log" \
    "e2e-udp-proxy-$SFX:docker" \
    "e2e-udp-mux-$SFX:docker"
  echo "--- Cleaning up vpn-single-client ---"
  stop_containers \
    "e2e-vpn-client-$SFX" \
    "e2e-udp-proxy-$SFX" \
    "e2e-udp-mux-$SFX" \
    "e2e-vpn-server-$SFX"
  remove_networks "$SFX"
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

title "vpn-single-client: MTU=$MTU BW=$BW threshold=${THRESHOLD} Mbit/s"

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
  --hostname "vpn-server" \
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
  --hostname "udp-mux" \
  --network "e2e-transit-net-$SFX" \
  --ip 172.21.0.30 \
  --cap-add NET_ADMIN \
  "local/udp-mux:$VERSION" \
  --listenAddr :8080

# udp-proxy — primary leg on client-net; second leg (transit-net) added below.
docker run -d \
  --name "e2e-udp-proxy-$SFX" \
  --hostname "udp-proxy" \
  --network "e2e-client-net-$SFX" \
  --ip 172.20.0.20 \
  --cap-add NET_ADMIN \
  "local/udp-proxy:$VERSION" \
  --listenAddr :7070 \
  --muxAddr 172.21.0.30:8080 \
  --endpointAddr 172.22.0.40:1194

# Client — on client-net only; reaches VPN server only via udp-proxy.
docker run -d \
  --name "e2e-vpn-client-$SFX" \
  --hostname "vpn-client"  \
  --network "e2e-client-net-$SFX" \
  --ip 172.20.0.10 \
  --cap-add NET_ADMIN \
  -v "$PKI_DIR:/pki:ro" \
  -v "$TMPDIR/client1.conf:/etc/openvpn/client1.conf:ro" \
  local/vpn-node:latest \
  sleep infinity

# Connect second legs.
docker network connect --ip 172.22.0.30 "e2e-server-net-$SFX"  "e2e-udp-mux-$SFX"
docker network connect --ip 172.21.0.20 "e2e-transit-net-$SFX" "e2e-udp-proxy-$SFX"

# ---------------------------------------------------------------------------
# MTU + bandwidth shaping
# ---------------------------------------------------------------------------
set_mtu "e2e-vpn-server-$SFX" eth0
set_mtu "e2e-vpn-client-$SFX" eth0

shape_bw "e2e-vpn-server-$SFX" eth0
shape_bw "e2e-vpn-client-$SFX" eth0
shape_bw "e2e-udp-proxy-$SFX"  eth0
shape_bw "e2e-udp-proxy-$SFX"  eth1
shape_bw "e2e-udp-mux-$SFX"    eth0
shape_bw "e2e-udp-mux-$SFX"    eth1

# ---------------------------------------------------------------------------
# Start OpenVPN
# ---------------------------------------------------------------------------
mknod_tun "e2e-vpn-server-$SFX"
mknod_tun "e2e-vpn-client-$SFX"

docker exec -d "e2e-vpn-server-$SFX" bash -c "openvpn /etc/openvpn/server.conf > /var/log/openvpn/vpn-server.log 2>&1"
docker exec -d "e2e-vpn-client-$SFX" bash -c "openvpn /etc/openvpn/client1.conf > /var/log/openvpn/vpn-client1.log 2>&1"

# Wait for VPN tunnel to come up on the client side.
wait_tun "e2e-vpn-client-$SFX" 60

# The OpenVPN server is assigned 10.200.0.1 (the first address of the pool).
VPN_SERVER_IP="10.200.0.1"

# ---------------------------------------------------------------------------
# iperf3 throughput test (TCP, 10 s)
# ---------------------------------------------------------------------------
docker exec -d "e2e-vpn-server-$SFX" iperf3 -s
sleep 1  # give iperf3 server a moment to start listening

echo "Running iperf3: client → $VPN_SERVER_IP (10 s TCP) ..."
MBPS=$(run_iperf3 "e2e-vpn-client-$SFX" "$VPN_SERVER_IP" 10)
echo "Result: ${MBPS} Mbit/s  (threshold: ${THRESHOLD} Mbit/s)"

if [ "$MBPS" -ge "$THRESHOLD" ]; then
  echo "PASS: vpn-single-client"
  exit 0
else
  echo "FAIL: vpn-single-client — throughput ${MBPS} Mbit/s is below ${THRESHOLD} Mbit/s"
  exit 1
fi
