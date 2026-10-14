package config

import (
	"fmt"
	"time"
)

type UdpMuxConfig struct {
	ListenAddr           string        // The ip:port the udp mux will listen on for UDP traffic.
	ApiListenAddr        string        // The ip:port the udp mux will listen on for API traffic.
	SessionTimeout       time.Duration // How long an idle session lives before being garbage-collected.
	CleanupInterval      time.Duration // How often to check for idle sessions to clean up.
	MaxSessionsPerClient int           // Max concurrent sessions per client IP (0 = unlimited).
}

func (cfg *UdpMuxConfig) Validate() error {
	if err := validateAddr(cfg.ListenAddr, "listen address"); err != nil {
		return err
	}
	if err := validateAddr(cfg.ApiListenAddr, "api listen address"); err != nil {
		return err
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
	return nil
}

func (cfg *UdpMuxConfig) String() string {
	return fmt.Sprintf("UdpMuxConfig{ListenAddr: %s, ApiListenAddr: %s, SessionTimeout: %s, CleanupInterval: %s, MaxSessionsPerClient: %d}",
		cfg.ListenAddr, cfg.ApiListenAddr, cfg.SessionTimeout, cfg.CleanupInterval, cfg.MaxSessionsPerClient)
}
