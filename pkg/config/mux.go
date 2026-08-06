package config

import (
	"fmt"
	"hash/fnv"
)

type UdpMuxConfig struct {
	ListenAddr  string
	endpoints   map[uint32]string
	endpointIds map[string]uint32
}

func (cfg *UdpMuxConfig) GetEndpointId(addr string) (uint32, error) {
	id, found := cfg.endpointIds[addr]
	if !found {
		return 0, fmt.Errorf("unknown endpoint '%s'", addr)
	}
	return id, nil
}

func (cfg *UdpMuxConfig) GetEndpoint(id uint32) (string, error) {
	addr, found := cfg.endpoints[id]
	if !found {
		return "", fmt.Errorf("unknown endpoint id '%d'", id)
	}
	return addr, nil
}

func (cfg *UdpMuxConfig) RegisterEndpoint(addr string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(addr))
	id := h.Sum32()

	cfg.endpoints[id] = addr
	cfg.endpointIds[addr] = id
	return id
}

func (cfg *UdpMuxConfig) UnregisterEndpoint(addr string) {
	h := fnv.New32a()
	h.Write([]byte(addr))
	id := h.Sum32()

	delete(cfg.endpoints, id)
	delete(cfg.endpointIds, addr)
}
