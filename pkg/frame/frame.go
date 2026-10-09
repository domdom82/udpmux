package frame

import (
	"encoding/binary"
	"fmt"
	"strconv"
)

// Protocol constants for the UDPM framing.
const (
	Magic uint32 = 0x5544504D // "UDPM"

	HeaderLength = 8 // 4 + 2 + 2 bytes (control frames only; data is raw)

	FlagPing       uint16 = 1 << 0 // Ping: mux echoes the frame back; payload is optional padding.
	FlagProxyHello uint16 = 1 << 1 // Proxy initiates session handshake; payload carries endpoint string.
	FlagMuxHello   uint16 = 1 << 2 // Mux acknowledges handshake; no payload.
	FlagKeepAlive  uint16 = 1 << 3 // Keepalive to prevent session expiry; no payload.
	FlagReset      uint16 = 1 << 4 // Mux requests re-handshake (unknown session); no payload.
)

// Header is the fixed-size control frame header.
// Data packets are forwarded raw with no header at all.
type Header struct {
	Magic  uint32 // Must be Magic.
	Flags  uint16 // Frame flags.
	Length uint16 // Payload length (0 for most control frames; endpoint string length for PROXY_HELLO).
}

func NewHeader(flags uint16, payloadLen int) *Header {
	return &Header{
		Magic:  Magic,
		Flags:  flags,
		Length: uint16(payloadLen),
	}
}

func Encode(h *Header) ([]byte, error) {
	buf := make([]byte, HeaderLength)
	_, err := binary.Encode(buf, binary.BigEndian, h)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func Decode(buf []byte) (*Header, error) {
	h := &Header{}
	_, err := binary.Decode(buf, binary.BigEndian, h)
	if err != nil {
		return nil, err
	}

	if h.Magic != Magic {
		return nil, fmt.Errorf("bad magic: %s (expected: %s)", strconv.Itoa(int(h.Magic)), strconv.Itoa(int(Magic)))
	}

	return h, nil
}
