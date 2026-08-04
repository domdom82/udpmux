package config

import "time"

type UdpMuxConfig struct {
	Listen          string
	BufferSize      int
	ReadTimeout     time.Duration
	SessionTimeout  time.Duration
}
