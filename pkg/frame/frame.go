package frame

import (
	"encoding/binary"
	"fmt"
)

// Protocol constants for the outer mux header.
const (
	Magic     uint32 = 0x5544504D // "UDPM"
	VersionV1 uint8  = 1
	VersionV2 uint8  = 2

	HeaderV1Length = 266 // 4 + 1 + 2 + 2 + 256 + 1 = 266 bytes
	HeaderV2Length = 11  // 4 + 1 + 2 + 2 + 2 = 11 bytes
)

// HeaderV1 is the parsed logical header of an udp mux frame.
// Requires fixed-width fields for encoding.
type HeaderV1 struct {
	Magic       uint32    // Must be Magic.
	Version     uint8     // Must be VersionV1.
	Flags       uint16    // Reserved for later use
	Length      uint16    // Payload length
	Endpoint    [256]byte // Destination endpoint
	EndpointLen uint8     // Endpoint string length
}

// HeaderV2 is the parsed logical header of an udp mux frame V2.
// V2 requires endpoints be registered at the mux ahead of time.
type HeaderV2 struct {
	Magic      uint32 // Must be Magic.
	Version    uint8  // Must be VersionV2.
	Flags      uint16 // Reserved for later use
	Length     uint16 // Payload length
	EndpointId uint16 // Destination endpoint id
}

func NewHeader(endpoint string) (*HeaderV1, error) {
	if len(endpoint) > 256 {
		return nil, fmt.Errorf("endpoint too long")
	}

	epBytes := make([]byte, 256)
	copy(epBytes, endpoint)

	h := &HeaderV1{
		Magic:       Magic,
		Version:     VersionV1,
		Flags:       0,
		Endpoint:    [256]byte(epBytes),
		EndpointLen: uint8(len(endpoint)),
	}

	return h, nil
}

func NewHeaderV2(endpointId uint16) *HeaderV2 {
	h := &HeaderV2{
		Magic:      Magic,
		Version:    VersionV2,
		Flags:      0,
		EndpointId: endpointId,
	}

	return h
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

func EncodeV2(h *HeaderV2) ([]byte, error) {
	buf := make([]byte, HeaderV2Length)

	_, err := binary.Encode(buf, binary.BigEndian, h)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func DecodeV2(buf []byte) (*HeaderV2, error) {
	h := &HeaderV2{}
	_, err := binary.Decode(buf, binary.BigEndian, h)
	if err != nil {
		return nil, err
	}
	return h, nil
}
