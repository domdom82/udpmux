package config

import (
	"fmt"
	"net"
)

const (
	ProtocolV1 = "v1"
	ProtocolV2 = "v2"
)

func validateAddr(addr, title string) error {
	if addr == "" {
		return fmt.Errorf("%s is required", title)
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid %s '%s': %w", title, addr, err)
	}
	_, err = net.LookupPort("udp", port)
	if err != nil {
		return fmt.Errorf("invalid %s port '%s': %w", title, port, err)
	}
	return nil
}
