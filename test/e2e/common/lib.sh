#!/usr/bin/env bash
# Shared helpers for e2e integration tests.
# Source this file; do not execute directly.
#
# Required environment (set by the caller before sourcing):
#   MTU     - network and link MTU (default: 1500)
#   TUN_MTU - OpenVPN tun MTU (default: MTU-28)
#   BW      - tc bandwidth shaping rate (default: 1gbit)
#   PKI_DIR - absolute path to the pki/ directory containing certs/keys

E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKI_DIR="${PKI_DIR:-$E2E_DIR/pki/gen}"
OPENVPN_TMPL_DIR="$E2E_DIR/openvpn"

export MTU="${MTU:-1500}"
export TUN_MTU="${TUN_MTU:-$((MTU - 28))}"
export BW="${BW:-1gbit}"

#---------------------------------------------------------------------------
#  title <text>
#    Prints a title banner to stdout.
#---------------------------------------------------------------------------
title() {
  echo
  echo "========================================================================"
  echo "=== $* ==="
  echo "========================================================================"
  echo
}

# ---------------------------------------------------------------------------
# render_configs <outdir>
#   Renders all *.conf.tmpl files from the openvpn template directory into
#   <outdir> using envsubst.  The resulting filenames drop the .tmpl suffix.
# ---------------------------------------------------------------------------
render_configs() {
  local outdir=$1
  for tmpl in "$OPENVPN_TMPL_DIR"/*.conf.tmpl; do
    local base
    base=$(basename "$tmpl" .tmpl)
    envsubst < "$tmpl" > "$outdir/$base"
  done
}

# ---------------------------------------------------------------------------
# mknod_tun <container>
#   Creates /dev/net/tun inside the container (needed by OpenVPN).
# ---------------------------------------------------------------------------
mknod_tun() {
  local ctr=$1
  docker exec "$ctr" sh -c "mkdir -p /dev/net && [ -e /dev/net/tun ] || mknod /dev/net/tun c 10 200 && chmod 600 /dev/net/tun"
}

# ---------------------------------------------------------------------------
# wait_tun <container> <timeout_seconds>
#   Polls until tun0 appears inside the container or timeout expires.
#   Returns 0 on success, 1 on timeout.
# ---------------------------------------------------------------------------
wait_tun() {
  local ctr=$1
  local timeout=$2
  local elapsed=0
  echo "Waiting for tun0 in $ctr (timeout: ${timeout}s)..."
  while [ "$elapsed" -lt "$timeout" ]; do
    if docker exec "$ctr" ip addr show tun0 >/dev/null 2>&1; then
      echo "tun0 is up in $ctr after ${elapsed}s"
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  echo "ERROR: tun0 did not appear in $ctr after ${timeout}s"
  docker exec "$ctr" ip addr show || true
  docker logs "$ctr" 2>&1 | tail -30 || true
  return 1
}

# ---------------------------------------------------------------------------
# get_tun_ip <container>
#   Prints the first IPv4 address assigned to tun0 inside the container.
# ---------------------------------------------------------------------------
get_tun_ip() {
  local ctr=$1
  docker exec "$ctr" ip -4 addr show tun0 | grep -oP '(?<=inet )\d+\.\d+\.\d+\.\d+'
}

# ---------------------------------------------------------------------------
# shape_bw <container> <iface> [rate]
#   Applies a tc TBF qdisc to <iface> inside <container>.
#   rate defaults to $BW (1gbit if unset).
# ---------------------------------------------------------------------------
shape_bw() {
  local ctr=$1
  local iface=$2
  local rate="${3:-${BW:-1gbit}}"
  docker exec "$ctr" tc qdisc replace dev "$iface" root tbf \
    rate "$rate" burst 32kbit latency 400ms
}

# ---------------------------------------------------------------------------
# set_mtu <container> <iface>
#   Explicitly sets the ethernet MTU on <iface> inside <container>.
#   (Belt-and-suspenders: Docker network driver.mtu already handles this.)
# ---------------------------------------------------------------------------
set_mtu() {
  local ctr=$1
  local iface=$2
  docker exec "$ctr" ip link set dev "$iface" mtu "$MTU"
}

# ---------------------------------------------------------------------------
# create_networks <suffix>
#   Creates the three Docker bridge networks for an e2e test run.
#   <suffix> is appended to network names to avoid collisions (e.g. "sc" or "dc").
# ---------------------------------------------------------------------------
create_networks() {
  local sfx=$1
  docker network create \
    --subnet 172.20.0.0/24 \
    --opt "com.docker.network.driver.mtu=$MTU" \
    "e2e-client-net-$sfx"
  docker network create \
    --subnet 172.21.0.0/24 \
    --opt "com.docker.network.driver.mtu=$MTU" \
    "e2e-transit-net-$sfx"
  docker network create \
    --subnet 172.22.0.0/24 \
    --opt "com.docker.network.driver.mtu=$MTU" \
    "e2e-server-net-$sfx"
}

# ---------------------------------------------------------------------------
# remove_networks <suffix>
# ---------------------------------------------------------------------------
remove_networks() {
  local sfx=$1
  docker network rm "e2e-client-net-$sfx" "e2e-transit-net-$sfx" "e2e-server-net-$sfx" 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# stop_containers <name...>
#   Force-removes named containers (ignores errors for already-gone containers).
# ---------------------------------------------------------------------------
stop_containers() {
  docker rm -f "$@" 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# collect_logs <test_name> <container:logpath_or_"docker"> ...
#   Copies logs from running (or recently stopped) containers into
#   test/e2e/logs/<test_name>/ before teardown.
#
#   For OpenVPN containers pass the in-container log path:
#     collect_logs vpn-single-client \
#       "e2e-vpn-server-sc:/var/log/openvpn/vpn-server.log" \
#       "e2e-client-sc:/var/log/openvpn/vpn-client1.log"
#
#   For udp-proxy / udp-mux (stdout/stderr via docker logs) pass "docker":
#     "e2e-udp-proxy-sc:docker" \
#     "e2e-udp-mux-sc:docker"
# ---------------------------------------------------------------------------
collect_logs() {
  local test_name=$1
  shift
  local log_dir="$E2E_DIR/../logs/$test_name"
  mkdir -p "$log_dir"
  for spec in "$@"; do
    local ctr="${spec%%:*}"
    local src="${spec#*:}"
    # Derive a filename from the container name (strip the e2e- prefix and suffix).
    local fname="${ctr}.log"
    if [ "$src" = "docker" ]; then
      echo "Collecting docker logs: $ctr → $log_dir/$fname"
      docker logs "$ctr" > "$log_dir/$fname" 2>&1 || true
    else
      echo "Collecting file log: $ctr:$src → $log_dir/$fname"
      docker cp "$ctr:$src" "$log_dir/$fname" 2>/dev/null || true
    fi
  done
  echo "Logs saved to $log_dir"
}

# ---------------------------------------------------------------------------
# run_iperf3 <container> <target_ip> <duration_seconds>
#   Runs iperf3 in TCP client mode with live interval output streaming to the
#   terminal.  Captures the output to a temp file on the host, then prints
#   the sender throughput in Mbit/s (integer) to stdout.
#
#   Usage:
#     MBPS=$(run_iperf3 e2e-client-sc 10.200.0.1 10)
# ---------------------------------------------------------------------------
run_iperf3() {
  local ctr=$1 target=$2 duration=$3
  local tmpout
  tmpout=$(mktemp)
  # Stream live output to the terminal (or stderr if no tty) while also saving
  # to a temp file for parsing.
  local tty_out
  { tty_out=/dev/tty; } 2>/dev/null
  { true > /dev/tty; } 2>/dev/null || tty_out=/dev/stderr
  docker exec "$ctr" iperf3 -c "$target" -t "$duration" 2>&1 | tee "$tmpout" > "$tty_out"
  # Extract the receiver line and parse Gbits/sec or Mbits/sec.
  # iperf3 text summary looks like:
  #   [  8]   0.00-10.01  sec  X.XX GBytes  X.XX Gbits/sec  receiver
  local receiver_line
  receiver_line=$(grep -i "receiver" "$tmpout" | tail -1)
  rm -f "$tmpout"
  # Parse value and unit (Gbits/sec or Mbits/sec or Kbits/sec)
  local val unit mbps
  val=$(echo "$receiver_line"  | awk '{for(i=1;i<=NF;i++) if($(i+1)~/bits\/sec/) {print $i; exit}}')
  unit=$(echo "$receiver_line" | awk '{for(i=1;i<=NF;i++) if($i~/bits\/sec/)     {print $i; exit}}')
  case "$unit" in
    Gbits/sec) mbps=$(echo "$val * 1000" | bc | cut -d. -f1) ;;
    Mbits/sec) mbps=$(echo "$val"        | cut -d. -f1)      ;;
    Kbits/sec) mbps=0 ;;
    *)         mbps=0 ;;
  esac
  echo "$mbps"
}