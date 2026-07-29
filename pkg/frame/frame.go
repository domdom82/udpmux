package frame

import (
	"encoding/binary"
	"fmt"
)

// Protocol constants for the outer mux header.
const (
	MagicV1   uint32 = 0x5544504D // "UDPM"
	VersionV1 uint8  = 1

	HeaderV1Length = 266 // 4 + 1 + 2 + 2 + 256 + 1 = 266 bytes
)

// HeaderV1 is the parsed logical header of an udp mux frame.
// Requires fixed-width fields for encoding.
type HeaderV1 struct {
	Magic       uint32    // Must be MagicV1.
	Version     uint8     // Must be VersionV1.
	Flags       uint16    // Reserved for future use
	Length      uint16    // Payload length
	Endpoint    [256]byte // Destination endpoint
	EndpointLen uint8     // Endpoint string length
}

func NewHeader(endpoint string) (*HeaderV1, error) {

	if len(endpoint) > 256 {
		return nil, fmt.Errorf("endpoint too long")
	}

	epBytes := make([]byte, 256)
	copy(epBytes, endpoint)

	h := &HeaderV1{
		Magic:       MagicV1,
		Version:     VersionV1,
		Flags:       0,
		Endpoint:    [256]byte(epBytes),
		EndpointLen: uint8(len(endpoint)),
	}

	return h, nil
}

func Encode(h *HeaderV1) ([]byte, error) {
	buf := make([]byte, HeaderV1Length)

	_, err := binary.Encode(buf, binary.BigEndian, h)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func Decode(buf []byte) (*HeaderV1, error) {
	h := &HeaderV1{}
	_, err := binary.Decode(buf, binary.BigEndian, h)
	if err != nil {
		return nil, err
	}
	return h, nil
}
