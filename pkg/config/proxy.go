package config

import (
	"fmt"
	"hash/fnv"

	"github.com/domdom82/udpmux/pkg/frame"
)

const (
	ProtocolV1 = "v1"
	ProtocolV2 = "v2"
)

type UdpProxyConfig struct {
	ListenAddr   string // The ip:port the udp proxy will listen on.
	MuxAddr      string // The ip:port the udp proxy will forward to.
	EndpointAddr string // The ip:port the udp mux will forward to.
	Protocol     string // The protocol version to use
}

func EndpointToId(endpoint string) frame.EndpointId {
	h := fnv.New64a()
	h.Write([]byte(endpoint))
	return frame.EndpointId(h.Sum64())
}

func (cfg *UdpProxyConfig) Validate() error {

	if err := validateAddr(cfg.ListenAddr, "listen address"); err != nil {
		return err
	}

	if err := validateAddr(cfg.MuxAddr, "mux address"); err != nil {
		return err
	}

	if err := validateAddr(cfg.MuxAddr, "endpoint address"); err != nil {
		return err
	}

	if cfg.Protocol == "" {
		return fmt.Errorf("protocol is required")
	}
	switch cfg.Protocol {
	case ProtocolV1, ProtocolV2:
	default:
		return fmt.Errorf("invalid protocol '%s'", cfg.Protocol)
	}

	return nil
}
