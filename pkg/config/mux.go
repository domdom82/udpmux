package config

import (
	"fmt"
)

type UdpMuxConfig struct {
	ListenAddr  string
	endpoints   map[uint32]string
	endpointIds map[string]uint32
}

func NewUdpMuxConfig(listenAddr string) *UdpMuxConfig {
	cfg := &UdpMuxConfig{
		ListenAddr:  listenAddr,
		endpoints:   make(map[uint32]string),
		endpointIds: make(map[string]uint32),
	}

	return cfg
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
	id := EndpointToId(addr)
	cfg.endpoints[id] = addr
	cfg.endpointIds[addr] = id
	return id
}

func (cfg *UdpMuxConfig) UnregisterEndpoint(addr string) {
	id := EndpointToId(addr)
	delete(cfg.endpoints, id)
	delete(cfg.endpointIds, addr)
}
