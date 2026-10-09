# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build          # build both binaries to bin/
make build-udp-proxy
make build-udp-mux
make check          # go fmt + go vet
make tidy           # go mod tidy
make clean

make test           # benchmarks + race-detector tests (always use this, not go test)
go test ./pkg/frame/... -run TestFrame  # single package/test during development
```

Docker images (requires `docker buildx`):
```bash
make docker-images
```

## Architecture

udpmux tunnels multiple UDP flows over a single shared port using a custom binary framing protocol. The canonical use case is multiplexing several VPN tunnels through one public port.

```
[Client] --plain UDP--> [udp-proxy :7070] --framed UDP--> [udp-mux :8080] --plain UDP--> [backend :1194]
```

### Two binaries

**`cmd/udp-proxy`** — client-side: manages the session handshake with the mux, buffers packets during handshake, forwards raw data once established, and sends keepalives when idle.

**`cmd/udp-mux`** — server-side: handles session handshakes, dispatches raw data to the right backend, and sends keepalives when idle. Also serves an HTTP API on `:8081`:
- `GET /api/sessions` — current active session count
- `GET /healthz`, `GET /readyz`

### Framing protocol (`pkg/frame/frame.go`)

Magic bytes: `0x5544504D` ("UDPM"). 8-byte control frame header: `[Magic 4B][Flags 2B][Length 2B]`.

Steady-state data is forwarded **raw with zero overhead** — no header on data packets. Control frames are only used for session lifecycle and keepalive.

Flag constants:
- `FlagProxyHello` — proxy initiates handshake; payload carries the backend endpoint string
- `FlagMuxHello` — mux acknowledges handshake; no payload
- `FlagKeepAlive` — keepalive to prevent session expiry; no payload
- `FlagReset` — mux requests re-handshake (unknown session); no payload
- `FlagPing` — proxy pings the mux directly to check connectivity; mux echoes it back

### Session state machine (proxy side, `cmd/udp-proxy/app/hooks.go`)

```
IDLE → HANDSHAKING → ESTABLISHED
```

- **IDLE**: first packet triggers PROXY_HELLO retries (up to 5, 500 ms apart); packets are buffered (up to 100)
- **HANDSHAKING**: further packets are buffered
- **ESTABLISHED**: raw data forwarded directly; buffered packets flushed on MUX_HELLO receipt

The mux side (`cmd/udp-mux/app/hooks.go`) disambiguates by backend conn: known session → raw data path; no conn → expect PROXY_HELLO.

### Keepalive (`pkg/proxy/session.go`)

Both sides independently send KEEPALIVE when idle. Keepalive is integrated into `ClientSession` via `StartKeepalive(idle, interval, sendFn)` / `StopKeepalive()`. The goroutine reads `lastActive atomic.Int64` for idle measurement and does **not** refresh on send — only received traffic (any packet) refreshes the expiry timer.

### Core proxy engine (`pkg/proxy/`)

- `proxy.go` — `Proxy` struct: uses `ipv4.ReadBatch` (recvmmsg, batch size 32). Hashes client addresses to fixed worker goroutines to preserve per-client packet ordering.
- `session.go` — `ClientSession` / `SessionManager`: one session per unique client address, each with a write-to-backend and read-from-backend goroutine. Sessions expire after 30 s of inactivity (cleanup every 10 s). Socket buffers are maximised to 16 MB. `StartKeepalive` / `StopKeepalive` manage the keepalive goroutine.
- `hook.go` — `Hook` type (`func(*ClientSession, []byte) ([]byte, error)`): framing logic is injected as read/write hooks, keeping the proxy engine protocol-agnostic. Returning `nil, nil` suppresses forwarding without error.

### Config (`pkg/config/`)

- `proxy.go` — `UdpProxyConfig`: listen/mux/endpoint addresses, session timeout, keepalive idle/interval, optional ping
- `mux.go` — `UdpMuxConfig`: listen/api addresses, session timeout, keepalive idle/interval
- `validate.go` — address validation shared by both configs

### Integration test environment

`test/openvpn/` contains certs, configs, and `start.sh`/`stop.sh` for a local OpenVPN multiplexing scenario.
