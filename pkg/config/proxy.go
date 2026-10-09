package config

import (
	"fmt"
	"time"
)

type UdpProxyConfig struct {
	ListenAddr        string        // The ip:port the udp proxy will listen on.
	MuxAddr           string        // The ip:port the udp proxy will forward to.
	EndpointAddr      string        // The ip:port the udp mux will forward to.
	Ping              bool          // If set, send pings to the mux to check network connectivity.
	PingSize          int           // Optional padding size for ping frames.
	SessionTimeout    time.Duration // How long an idle session lives before being garbage-collected.
	KeepaliveIdle     time.Duration // Inactivity duration before the first KEEPALIVE is sent.
	KeepaliveInterval time.Duration // Interval between subsequent KEEPALIVE packets while still idle.
}

func (cfg *UdpProxyConfig) Validate() error {
	if err := validateAddr(cfg.ListenAddr, "listen address"); err != nil {
		return err
	}

	if err := validateAddr(cfg.MuxAddr, "mux address"); err != nil {
		return err
	}

	if err := validateAddr(cfg.EndpointAddr, "endpoint address"); err != nil {
		return err
	}

	if cfg.SessionTimeout <= 0 {
		return fmt.Errorf("session_timeout must be positive")
	}
	if cfg.KeepaliveIdle <= 0 {
		return fmt.Errorf("keepalive_idle must be positive")
	}
	if cfg.KeepaliveInterval <= 0 {
		return fmt.Errorf("keepalive_interval must be positive")
	}
	if cfg.KeepaliveIdle >= cfg.SessionTimeout {
		return fmt.Errorf("keepalive_idle must be less than session_timeout")
	}

	if cfg.Ping && cfg.PingSize < 0 {
		return fmt.Errorf("ping size must be non-negative")
	}

	return nil
}
