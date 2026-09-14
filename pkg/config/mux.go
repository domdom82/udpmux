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
	Protocol      string // The UDPM protocol version to use. V1 or V2
	mu            sync.RWMutex
	endpoints     map[frame.EndpointId]string // The mapping of endpoint ids to endpoint addresses. Only used in V2 protocol.
	endpointIds   map[string]frame.EndpointId // The mapping of endpoint addresses to endpoint ids. Only used in V2 protocol.
}

// NewUdpMuxConfig creates a new UdpMuxConfig.
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

// ListEndpointMappings returns a copy of all endpoint mappings.
func (cfg *UdpMuxConfig) ListEndpointMappings() map[frame.EndpointId]string {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	mappings := make(map[frame.EndpointId]string)
	maps.Copy(mappings, cfg.endpoints)
	return mappings
}

// GetEndpointId returns an endpoint id for an endpoint address.
func (cfg *UdpMuxConfig) GetEndpointId(addr string) (frame.EndpointId, error) {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	id, found := cfg.endpointIds[addr]
	if !found {
		return 0, fmt.Errorf("unknown endpoint '%s'", addr)
	}
	return id, nil
}

// GetEndpoint returns an endpoint address for an endpoint id.
func (cfg *UdpMuxConfig) GetEndpoint(id frame.EndpointId) (string, error) {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	addr, found := cfg.endpoints[id]
	if !found {
		return "", fmt.Errorf("unknown endpoint id '%d'", id)
	}
	return addr, nil
}

// RegisterEndpoint registers a single endpoint address.
func (cfg *UdpMuxConfig) RegisterEndpoint(addr string) frame.EndpointId {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	id := EndpointToId(addr)
	cfg.endpoints[id] = addr
	cfg.endpointIds[addr] = id
	return id
}

// RegisterEndpoints registers a list of endpoint addresses atomically.
func (cfg *UdpMuxConfig) RegisterEndpoints(addrs []string) {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	for _, addr := range addrs {
		id := EndpointToId(addr)
		cfg.endpoints[id] = addr
		cfg.endpointIds[addr] = id
	}
}

// UnregisterEndpoint unregisters a single endpoint address. It returns an error if the endpoint is unknown.
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

// NumEndpoints returns the number of registered endpoints.
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
