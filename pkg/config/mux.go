package config

import (
	"fmt"

	"github.com/domdom82/udpmux/pkg/frame"
)

type UdpMuxConfig struct {
	ListenAddr    string // The ip:port the udp proxy will listen on for UDP traffic
	ApiListenAddr string // The ip:port the udp proxy will listen on for API traffic
	Protocol      string // The protocol version to use
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

func (cfg *UdpMuxConfig) GetEndpointId(addr string) (frame.EndpointId, error) {
	id, found := cfg.endpointIds[addr]
	if !found {
		return 0, fmt.Errorf("unknown endpoint '%s'", addr)
	}
	return id, nil
}

func (cfg *UdpMuxConfig) GetEndpoint(id frame.EndpointId) (string, error) {
	addr, found := cfg.endpoints[id]
	if !found {
		return "", fmt.Errorf("unknown endpoint id '%d'", id)
	}
	return addr, nil
}

func (cfg *UdpMuxConfig) RegisterEndpoint(addr string) frame.EndpointId {
	id := EndpointToId(addr)
	cfg.endpoints[id] = addr
	cfg.endpointIds[addr] = id
	return id
}

func (cfg *UdpMuxConfig) UnregisterEndpoint(addr string) error {
	if _, err := cfg.GetEndpointId(addr); err != nil {
		return err
	}
	id := EndpointToId(addr)
	delete(cfg.endpoints, id)
	delete(cfg.endpointIds, addr)
	return nil
}

func (cfg *UdpMuxConfig) NumEndpoints() int {
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
