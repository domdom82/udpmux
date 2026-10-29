#!/usr/bin/env bash
# Idempotently generates all PKI material needed by the e2e tests.
# Output goes to gen/ (next to this script) which is git-ignored.
# Requires only docker (uses local/vpn-node:latest for openssl + openvpn).
# Run from any directory.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$SCRIPT_DIR/gen"
mkdir -p "$OUT"

need() {
  for f in "$@"; do
    [ -f "$OUT/$f" ] && continue
    return 1
  done
  return 0
}

# Skip if everything already exists.
if need ca.crt ca.key server.crt server.key server2.crt server2.key \
        client1.crt client1.key \
        client2.crt client2.key \
        dh.pem ta.key; then
  echo "PKI material already present in $OUT, skipping generation."
  exit 0
fi

echo "Generating PKI material in $OUT ..."

docker run --rm -v "$OUT:/pki" local/vpn-node:latest bash -euo pipefail -c '
  sign_cert() {
    local name=$1 cn=$2
    openssl genrsa -out "/pki/${name}.key" 2048
    openssl req -new -key "/pki/${name}.key" -out "/pki/${name}.csr" -subj "/CN=${cn}"
    openssl x509 -req -days 3650 \
      -in "/pki/${name}.csr" -CA /pki/ca.crt -CAkey /pki/ca.key \
      -CAcreateserial -out "/pki/${name}.crt"
    rm -f "/pki/${name}.csr"
  }

  openssl genrsa -out /pki/ca.key 4096
  openssl req -new -x509 -days 3650 -key /pki/ca.key -out /pki/ca.crt -subj "/CN=udpmux-e2e-ca"

  sign_cert server  "udpmux-e2e-server"
  sign_cert server2 "udpmux-e2e-server2"
  sign_cert client1 "udpmux-e2e-client1"
  sign_cert client2 "udpmux-e2e-client2"

  openssl dhparam -out /pki/dh.pem 2048
  openvpn --genkey tls-auth /pki/ta.key
'

echo "PKI generation complete."
