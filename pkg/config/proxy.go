package config

import (
	"fmt"
	"time"
)

const (
	pingSizeLimit = 9000
)

type UdpProxyConfig struct {
	ListenAddr           string        // The ip:port the udp proxy will listen on.
	MuxAddr              string        // The ip:port the udp proxy will forward to.
	EndpointAddr         string        // The ip:port the udp mux will forward to.
	Ping                 bool          // If set, send pings to the mux to check network connectivity.
	PingSize             int           // Optional padding size for ping frames.
	SessionTimeout       time.Duration // How long an idle session lives before being garbage-collected.
	CleanupInterval      time.Duration // How often to check for idle sessions to clean up.
	MaxSessionsPerClient int           // Max concurrent sessions per client IP (0 = unlimited).
}

func (cfg *UdpProxyConfig) Validate() error {
	if err := validateAddr(cfg.ListenAddr, "listen address"); err != nil {
		return err
	}

	if err := validateAddr(cfg.MuxAddr, "mux address"); err != nil {
		return err
	}

	if !cfg.Ping {
		if err := validateAddr(cfg.EndpointAddr, "endpoint address"); err != nil {
			return err
		}
	}

	if cfg.SessionTimeout <= 0 {
		return fmt.Errorf("sessionTimeout must be positive")
	}

	if cfg.CleanupInterval <= 0 {
		return fmt.Errorf("cleanupInterval must be positive")
	}

	if cfg.MaxSessionsPerClient < 0 {
		return fmt.Errorf("maxSessionsPerClient must be non-negative")
	}

	if cfg.Ping && cfg.PingSize < 0 {
		return fmt.Errorf("pingSize must be non-negative")
	}

	if cfg.Ping && cfg.PingSize > pingSizeLimit {
		return fmt.Errorf("pingSize must be less than or equal to %d", pingSizeLimit)
	}

	return nil
}

func (cfg *UdpProxyConfig) String() string {
	return fmt.Sprintf("UdpProxyConfig{ListenAddr: %s, MuxAddr: %s, EndpointAddr: %s, Ping: %t, PingSize: %d, SessionTimeout: %s, CleanupInterval: %s, MaxSessionsPerClient: %d}",
		cfg.ListenAddr, cfg.MuxAddr, cfg.EndpointAddr, cfg.Ping, cfg.PingSize, cfg.SessionTimeout, cfg.CleanupInterval, cfg.MaxSessionsPerClient)
}
