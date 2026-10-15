package config

import (
	"fmt"
	"regexp"
	"time"
)

type UdpMuxConfig struct {
	ListenAddr           string        // The ip:port the udp mux will listen on for UDP traffic.
	ApiListenAddr        string        // The ip:port the udp mux will listen on for API traffic.
	SessionTimeout       time.Duration // How long an idle session lives before being garbage-collected.
	CleanupInterval      time.Duration // How often to check for idle sessions to clean up.
	MaxSessionsPerClient int           // Max concurrent sessions per client IP (0 = unlimited).
	EndpointAllowList    []string      // Allowed backend endpoint patterns (regular expressions); empty = allow all.

	compiledAllowList []*regexp.Regexp
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
	cfg.compiledAllowList = make([]*regexp.Regexp, len(cfg.EndpointAllowList))
	for i, pat := range cfg.EndpointAllowList {
		re, err := regexp.Compile(pat)
		if err != nil {
			return fmt.Errorf("invalid endpointAllowList pattern %q: %w", pat, err)
		}
		cfg.compiledAllowList[i] = re
	}
	return nil
}

// CompiledAllowList returns the pre-compiled endpoint allow-list patterns.
// Must be called after Validate().
func (cfg *UdpMuxConfig) CompiledAllowList() []*regexp.Regexp {
	return cfg.compiledAllowList
}

func (cfg *UdpMuxConfig) String() string {
	return fmt.Sprintf("UdpMuxConfig{ListenAddr: %s, ApiListenAddr: %s, SessionTimeout: %s, CleanupInterval: %s, MaxSessionsPerClient: %d, EndpointAllowList: %v}",
		cfg.ListenAddr, cfg.ApiListenAddr, cfg.SessionTimeout, cfg.CleanupInterval, cfg.MaxSessionsPerClient, cfg.EndpointAllowList)
}
