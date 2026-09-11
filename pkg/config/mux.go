package config

import (
	"fmt"
	"maps"
	"sync"

	"github.com/domdom82/udpmux/pkg/frame"
)

type UdpMuxConfig struct {
	ListenAddr    string // The ip:port the udp proxy will listen on for UDP traffic
	ApiListenAddr string // The ip:port the udp proxy will listen on for API traffic
	Protocol      string // The protocol version to use
	mu            sync.RWMutex
	endpoints     map[frame.EndpointId]string
	endpointIds   map[string]frame.EndpointId
}

func NewUdpMuxConfig(listenAddr string, apiListenAddr string, protocol string) *UdpMuxConfig {
	cfg := &UdpMuxConfig{
		ListenAddr:    listenAddr,
		ApiListenAddr: apiListenAddr,
		Protocol:      protocol,
		endpoints:     make(map[frame.EndpointId]string),
		endpointIds:   make(map[string]frame.EndpointId),
	}

	return cfg
}

func (cfg *UdpMuxConfig) ListEndpointMappings() map[frame.EndpointId]string {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	mappings := make(map[frame.EndpointId]string)
	maps.Copy(mappings, cfg.endpoints)
	return mappings
}

func (cfg *UdpMuxConfig) GetEndpointId(addr string) (frame.EndpointId, error) {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	id, found := cfg.endpointIds[addr]
	if !found {
		return 0, fmt.Errorf("unknown endpoint '%s'", addr)
	}
	return id, nil
}

func (cfg *UdpMuxConfig) GetEndpoint(id frame.EndpointId) (string, error) {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	addr, found := cfg.endpoints[id]
	if !found {
		return "", fmt.Errorf("unknown endpoint id '%d'", id)
	}
	return addr, nil
}

func (cfg *UdpMuxConfig) RegisterEndpoint(addr string) frame.EndpointId {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	id := EndpointToId(addr)
	cfg.endpoints[id] = addr
	cfg.endpointIds[addr] = id
	return id
}

func (cfg *UdpMuxConfig) UnregisterEndpoint(addr string) error {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	// inline the lookup to avoid lock re-entry
	if _, found := cfg.endpointIds[addr]; !found {
		return fmt.Errorf("unknown endpoint '%s'", addr)
	}
	id := EndpointToId(addr)
	delete(cfg.endpoints, id)
	delete(cfg.endpointIds, addr)
	return nil
}

func (cfg *UdpMuxConfig) NumEndpoints() int {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	return len(cfg.endpoints)
}

func (cfg *UdpMuxConfig) Validate() error {
	if err := validateAddr(cfg.ListenAddr, "listen address"); err != nil {
		return err
	}
	if err := validateAddr(cfg.ApiListenAddr, "api listen address"); err != nil {
		return err
	}

	return nil
}
