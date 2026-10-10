#!/usr/bin/env bash
# Idempotently generates all PKI material needed by the e2e tests.
# Output goes to gen/ (next to this script) which is git-ignored.
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
if need ca.crt ca.key server.crt server.key \
        client1.crt client1.key \
        client2.crt client2.key \
        dh.pem ta.key; then
  echo "PKI material already present in $OUT, skipping generation."
  exit 0
fi

echo "Generating PKI material in $OUT ..."

# ---------------------------------------------------------------------------
# CA
# ---------------------------------------------------------------------------
openssl genrsa -out "$OUT/ca.key" 4096
openssl req -new -x509 -days 3650 -key "$OUT/ca.key" \
  -out "$OUT/ca.crt" \
  -subj "/CN=udpmux-e2e-ca"

sign_cert() {
  local name=$1 cn=$2
  openssl genrsa -out "$OUT/${name}.key" 2048
  openssl req -new -key "$OUT/${name}.key" \
    -out "$OUT/${name}.csr" \
    -subj "/CN=${cn}"
  openssl x509 -req -days 3650 \
    -in  "$OUT/${name}.csr" \
    -CA  "$OUT/ca.crt" \
    -CAkey "$OUT/ca.key" \
    -CAcreateserial \
    -out "$OUT/${name}.crt"
  rm -f "$OUT/${name}.csr"
}

# ---------------------------------------------------------------------------
# Server and client certs
# ---------------------------------------------------------------------------
sign_cert server   "udpmux-e2e-server"
sign_cert client1  "udpmux-e2e-client1"
sign_cert client2  "udpmux-e2e-client2"

# ---------------------------------------------------------------------------
# DH parameters (2048-bit)
# ---------------------------------------------------------------------------
openssl dhparam -out "$OUT/dh.pem" 2048

# ---------------------------------------------------------------------------
# TLS auth key
# ---------------------------------------------------------------------------
openvpn --genkey tls-auth "$OUT/ta.key"

echo "PKI generation complete."
