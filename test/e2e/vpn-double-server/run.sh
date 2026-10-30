#!/usr/bin/env bash
# vpn-double-server integration test.
#
# Topology:
#   [client-net]   vpn-client1 (172.20.0.10) + udp-proxy1 (172.20.0.20)
#                  vpn-client2 (172.20.0.50) + udp-proxy2 (172.20.0.60)
#   [transit-net]  udp-proxy1  (172.21.0.20) + udp-proxy2 (172.21.0.60)
#                  udp-mux     (172.21.0.30)
#   [server-net]   udp-mux     (172.22.0.30)
#                  vpn-server1 (172.22.0.40) — listens on UDP 1194
#                  vpn-server2 (172.22.0.50) — listens on UDP 1194
#
# One udp-mux multiplexes two independent VPN flows simultaneously:
#   client1 → udp-proxy1 → udp-mux → vpn-server1 (pool 10.200.0.0/24)
#   client2 → udp-proxy2 → udp-mux → vpn-server2 (pool 10.201.0.0/24)
#
# Both iperf3 tests run concurrently. Each client targets its server's tun IP.
#
# Environment variables:
#   MTU           - network MTU (default: 1500)
#   BW            - tc bandwidth rate (default: 1gbit)
#   THRESHOLD_MBPS - minimum acceptable throughput per flow in Mbit/s (default: 180)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="$SCRIPT_DIR/../common"
REPO_ROOT="$SCRIPT_DIR/../../.."
PKI_DIR="$COMMON/pki/gen"
VERSION="$(cat "$REPO_ROOT/VERSION")"
THRESHOLD="${THRESHOLD_MBPS:-180}"
SFX="ds"  # suffix to namespace Docker resources for this test

# shellcheck source=../common/lib.sh
source "$COMMON/lib.sh"

# Override template dir: this test has its own OpenVPN config templates.
OPENVPN_TMPL_DIR="$SCRIPT_DIR"

TMPDIR="$(mktemp -d)"

cleanup() {
  echo "--- Collecting logs: vpn-double-server ---"
  collect_logs "vpn-double-server" \
    "e2e-vpn-server1-$SFX:/var/log/openvpn/vpn-server1.log" \
    "e2e-vpn-server2-$SFX:/var/log/openvpn/vpn-server2.log" \
    "e2e-vpn-client1-$SFX:/var/log/openvpn/vpn-client1.log" \
    "e2e-vpn-client2-$SFX:/var/log/openvpn/vpn-client2.log" \
    "e2e-udp-proxy1-$SFX:docker" \
    "e2e-udp-proxy2-$SFX:docker" \
    "e2e-udp-mux-$SFX:docker"
  echo "--- Cleaning up vpn-double-server ---"
  stop_containers \
    "e2e-vpn-client1-$SFX" \
    "e2e-vpn-client2-$SFX" \
    "e2e-udp-proxy1-$SFX" \
    "e2e-udp-proxy2-$SFX" \
    "e2e-udp-mux-$SFX" \
    "e2e-vpn-server1-$SFX" \
    "e2e-vpn-server2-$SFX"
  remove_networks "$SFX"
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

title "vpn-double-server: MTU=$MTU BW=$BW threshold=${THRESHOLD} Mbit/s per flow"

# Render OpenVPN configs from this test's templates.
# server.conf.tmpl comes from common; server2/client-ds* come from this directory.
render_configs "$TMPDIR"
# Also render the shared server1 config (from common/openvpn/).
envsubst < "$COMMON/openvpn/server.conf.tmpl" > "$TMPDIR/server1.conf"

# ---------------------------------------------------------------------------
# Networks
# ---------------------------------------------------------------------------
create_networks "$SFX"

# ---------------------------------------------------------------------------
# Containers
# ---------------------------------------------------------------------------

# VPN server1 — on server-net, port 1194, pool 10.200.0.0/24.
docker run -d \
  --name "e2e-vpn-server1-$SFX" \
  --hostname vpn-server1 \
  --network "e2e-server-net-$SFX" \
  --ip 172.22.0.40 \
  --cap-add NET_ADMIN \
  -v "$PKI_DIR:/pki:ro" \
  -v "$TMPDIR/server1.conf:/etc/openvpn/server.conf:ro" \
  -v "$COMMON/openvpn/ccd:/etc/openvpn/ccd:ro" \
  local/vpn-node:latest \
  sleep infinity

# VPN server2 — on server-net, port 1195, pool 10.201.0.0/24.
docker run -d \
  --name "e2e-vpn-server2-$SFX" \
  --hostname vpn-server2 \
  --network "e2e-server-net-$SFX" \
  --ip 172.22.0.50 \
  --cap-add NET_ADMIN \
  -v "$PKI_DIR:/pki:ro" \
  -v "$TMPDIR/server2.conf:/etc/openvpn/server.conf:ro" \
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

# udp-proxy1 — forwards to vpn-server1:1194.
docker run -d \
  --name "e2e-udp-proxy1-$SFX" \
  --hostname udp-proxy1 \
  --network "e2e-client-net-$SFX" \
  --ip 172.20.0.20 \
  --cap-add NET_ADMIN \
  "local/udp-proxy:$VERSION" \
  --listenAddr :7070 \
  --muxAddr 172.21.0.30:8080 \
  --endpointAddr 172.22.0.40:1194

# udp-proxy2 — forwards to vpn-server2:1194.
docker run -d \
  --name "e2e-udp-proxy2-$SFX" \
  --hostname udp-proxy2 \
  --network "e2e-client-net-$SFX" \
  --ip 172.20.0.60 \
  --cap-add NET_ADMIN \
  "local/udp-proxy:$VERSION" \
  --listenAddr :7070 \
  --muxAddr 172.21.0.30:8080 \
  --endpointAddr 172.22.0.50:1194

# vpn-client1 — reaches vpn-server1 via udp-proxy1.
docker run -d \
  --name "e2e-vpn-client1-$SFX" \
  --hostname client1 \
  --network "e2e-client-net-$SFX" \
  --ip 172.20.0.10 \
  --cap-add NET_ADMIN \
  -v "$PKI_DIR:/pki:ro" \
  -v "$TMPDIR/client-ds1.conf:/etc/openvpn/client.conf:ro" \
  local/vpn-node:latest \
  sleep infinity

# vpn-client2 — reaches vpn-server2 via udp-proxy2.
docker run -d \
  --name "e2e-vpn-client2-$SFX" \
  --hostname client2 \
  --network "e2e-client-net-$SFX" \
  --ip 172.20.0.50 \
  --cap-add NET_ADMIN \
  -v "$PKI_DIR:/pki:ro" \
  -v "$TMPDIR/client-ds2.conf:/etc/openvpn/client.conf:ro" \
  local/vpn-node:latest \
  sleep infinity

# Connect second legs: mux and both proxies onto their second networks.
docker network connect --ip 172.22.0.30 "e2e-server-net-$SFX"  "e2e-udp-mux-$SFX"
docker network connect --ip 172.21.0.20 "e2e-transit-net-$SFX" "e2e-udp-proxy1-$SFX"
docker network connect --ip 172.21.0.60 "e2e-transit-net-$SFX" "e2e-udp-proxy2-$SFX"

# ---------------------------------------------------------------------------
# MTU + bandwidth shaping
# ---------------------------------------------------------------------------
set_mtu "e2e-vpn-server1-$SFX" eth0
set_mtu "e2e-vpn-server2-$SFX" eth0
set_mtu "e2e-vpn-client1-$SFX" eth0
set_mtu "e2e-vpn-client2-$SFX" eth0

shape_bw "e2e-vpn-server1-$SFX" eth0
shape_bw "e2e-vpn-server2-$SFX" eth0
shape_bw "e2e-vpn-client1-$SFX" eth0
shape_bw "e2e-vpn-client2-$SFX" eth0
shape_bw "e2e-udp-proxy1-$SFX"  eth0
shape_bw "e2e-udp-proxy1-$SFX"  eth1
shape_bw "e2e-udp-proxy2-$SFX"  eth0
shape_bw "e2e-udp-proxy2-$SFX"  eth1
shape_bw "e2e-udp-mux-$SFX"     eth0
shape_bw "e2e-udp-mux-$SFX"     eth1

# ---------------------------------------------------------------------------
# Start OpenVPN
# ---------------------------------------------------------------------------
mknod_tun "e2e-vpn-server1-$SFX"
mknod_tun "e2e-vpn-server2-$SFX"
mknod_tun "e2e-vpn-client1-$SFX"
mknod_tun "e2e-vpn-client2-$SFX"

docker exec -d "e2e-vpn-server1-$SFX" bash -c "openvpn /etc/openvpn/server.conf > /var/log/openvpn/vpn-server1.log 2>&1"
docker exec -d "e2e-vpn-server2-$SFX" bash -c "openvpn /etc/openvpn/server.conf > /var/log/openvpn/vpn-server2.log 2>&1"
docker exec -d "e2e-vpn-client1-$SFX" bash -c "openvpn /etc/openvpn/client.conf > /var/log/openvpn/vpn-client1.log 2>&1"
docker exec -d "e2e-vpn-client2-$SFX" bash -c "openvpn /etc/openvpn/client.conf > /var/log/openvpn/vpn-client2.log 2>&1"

# Wait for both VPN tunnels to come up on the client side.
wait_tun "e2e-vpn-client1-$SFX" 60
wait_tun "e2e-vpn-client2-$SFX" 60

# ---------------------------------------------------------------------------
# iperf3 throughput test — both flows run simultaneously (10 s TCP)
#   client1 → 10.200.0.1 (vpn-server1 tun IP)
#   client2 → 10.201.0.1 (vpn-server2 tun IP)
# ---------------------------------------------------------------------------
docker exec -d "e2e-vpn-server1-$SFX" bash -c "iperf3 -s > /var/log/iperf3.log 2>&1"
docker exec -d "e2e-vpn-server2-$SFX" bash -c "iperf3 -s > /var/log/iperf3.log 2>&1"
sleep 2  # give iperf3 servers a moment to start listening

echo "Running iperf3 on both flows simultaneously (10 s TCP) ..."
tmpmbps1=$(mktemp)
tmpmbps2=$(mktemp)
{ run_iperf3 "e2e-vpn-client1-$SFX" "10.200.0.1" 10 > "$tmpmbps1"; } &
{ run_iperf3 "e2e-vpn-client2-$SFX" "10.201.0.1" 10 > "$tmpmbps2"; } &
wait
MBPS1=$(cat "$tmpmbps1")
MBPS2=$(cat "$tmpmbps2")
rm -f "$tmpmbps1" "$tmpmbps2"

echo "Flow1 result: ${MBPS1} Mbit/s  (threshold: ${THRESHOLD} Mbit/s)"
echo "Flow2 result: ${MBPS2} Mbit/s  (threshold: ${THRESHOLD} Mbit/s)"

FAILED=0
[ "$MBPS1" -ge "$THRESHOLD" ] || { echo "FAIL: flow1 throughput ${MBPS1} Mbit/s is below ${THRESHOLD} Mbit/s"; FAILED=1; }
[ "$MBPS2" -ge "$THRESHOLD" ] || { echo "FAIL: flow2 throughput ${MBPS2} Mbit/s is below ${THRESHOLD} Mbit/s"; FAILED=1; }

if [ "$FAILED" -eq 0 ]; then
  echo "PASS: vpn-double-server"
  exit 0
else
  exit 1
fi
