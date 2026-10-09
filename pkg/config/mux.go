package config

import (
	"fmt"
	"time"
)

type UdpMuxConfig struct {
	ListenAddr        string        // The ip:port the udp mux will listen on for UDP traffic.
	ApiListenAddr     string        // The ip:port the udp mux will listen on for API traffic.
	SessionTimeout    time.Duration // How long an idle session lives before being garbage-collected.
	KeepaliveIdle     time.Duration // Idle duration before sending KEEPALIVE toward the proxy.
	KeepaliveInterval time.Duration // Interval between subsequent KEEPALIVE packets while idle.
}

// NewUdpMuxConfig creates a new UdpMuxConfig.
func NewUdpMuxConfig(listenAddr string, apiListenAddr string, sessionTimeout, keepaliveIdle, keepaliveInterval time.Duration) *UdpMuxConfig {
	return &UdpMuxConfig{
		ListenAddr:        listenAddr,
		ApiListenAddr:     apiListenAddr,
		SessionTimeout:    sessionTimeout,
		KeepaliveIdle:     keepaliveIdle,
		KeepaliveInterval: keepaliveInterval,
	}
}

func (cfg *UdpMuxConfig) Validate() error {
	if err := validateAddr(cfg.ListenAddr, "listen address"); err != nil {
		return err
	}
	if err := validateAddr(cfg.ApiListenAddr, "api listen address"); err != nil {
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
	return nil
}
