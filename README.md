# udpmux
udpmux tunnels multiple UDP flows over a single shared port using a custom binary framing protocol.
The canonical use case is multiplexing several VPN tunnels through one public port.

## Architecture

![udpmux architecture](docs/architecture.svg)

![session lifecycle](docs/sequence.svg)

```
[Client] --plain UDP--> [udp-proxy :7070] --framed UDP--> [udp-mux :8080] --plain UDP--> [backend :1194]
```

### UDP Proxy (`udp-proxy`)
Client-side component. On the first packet from a new client it initiates a session handshake with
the mux (PROXY_HELLO), buffering up to 100 packets while waiting. Once the mux acknowledges
(MUX_HELLO) the session is established and subsequent packets are forwarded raw — no header
overhead. Sends KEEPALIVE frames when idle to prevent session expiry.

### UDP Mux (`udp-mux`)
Server-side component. Listens on a single UDP port and multiplexes traffic to multiple backends.
It identifies sessions by the client's source address: unknown source triggers a PROXY_HELLO
handshake (which carries the target backend endpoint); known source means the packet is forwarded
raw to the backend. Replies from the backend are sent back to the proxy raw. Also serves an HTTP
API on `:8081` (`GET /api/sessions`, `GET /healthz`, `GET /readyz`).

## Framing protocol

Control frames use an 8-byte header: `[Magic 4B][Flags 2B][Length 2B]`.
Magic bytes: `0x5544504D` ("UDPM").

Steady-state data is forwarded **raw with zero overhead** — no header on data packets.
Control frames are used only during session lifecycle and keepalive:

| Flag          | Direction   | Payload                                    |
|---------------|-------------|--------------------------------------------|
| `PROXY_HELLO` | proxy → mux | backend endpoint string (e.g. `host:port`) |
| `MUX_HELLO`   | mux → proxy | none                                       |
| `KEEPALIVE`   | both        | none                                       |
| `RESET`       | mux → proxy | none (triggers re-handshake)               |
| `PING`        | proxy → mux | optional padding; mux echoes it back       |

## Session lifecycle

```
IDLE → HANDSHAKING → ESTABLISHED
```

- **IDLE**: first packet triggers PROXY_HELLO retries (up to 5, 500 ms apart); packets are buffered
- **HANDSHAKING**: further packets are buffered
- **ESTABLISHED**: raw data forwarded directly; buffered packets flushed on MUX_HELLO receipt

Sessions expire after 30 s of inactivity. Both sides independently send KEEPALIVE when idle.
