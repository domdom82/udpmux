package app

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/go-logr/logr"
)

const (
	pingInterval = 1 * time.Second
	pingTimeout  = 5 * time.Second
)

func runPing(ctx context.Context, log logr.Logger, cfg *config.UdpProxyConfig) error {
	muxAddr, err := net.ResolveUDPAddr("udp", cfg.MuxAddr)
	if err != nil {
		return fmt.Errorf("invalid mux address '%s': %w", cfg.MuxAddr, err)
	}
	conn, err := net.DialUDP("udp", nil, muxAddr)
	if err != nil {
		return fmt.Errorf("failed to dial mux '%s': %w", cfg.MuxAddr, err)
	}
	defer conn.Close()

	// Build the ping frame once (reused every tick).
	// Payload is cfg.PingSize bytes of 'A' padding (or empty when PingSize == 0).
	var payload []byte
	if cfg.PingSize > 0 {
		payload = make([]byte, cfg.PingSize)
		for i := range payload {
			payload[i] = 'A'
		}
	}
	h := frame.NewHeader(frame.FlagPing, len(payload))
	headerBytes, err := frame.Encode(h)
	if err != nil {
		return fmt.Errorf("failed to encode ping frame: %w", err)
	}
	pingFrame := append(headerBytes, payload...)

	tick := time.NewTicker(pingInterval)
	defer tick.Stop()

	buf := make([]byte, frame.HeaderLength+cfg.PingSize+64) // extra headroom
	for {
		select {
		case <-ctx.Done():
			log.Info("ping loop stopped")
			return nil
		case <-tick.C:
			tStart := time.Now()
			n, err := conn.Write(pingFrame)
			if err != nil {
				log.Error(err, "failed to send ping frame", "bytes written", n)
				continue
			}
			log.Info("-> ping", "mux", cfg.MuxAddr, "bytes", n)

			_ = conn.SetReadDeadline(time.Now().Add(pingTimeout))
			n, err = conn.Read(buf)
			if err != nil {
				log.Error(err, "failed to receive pong frame", "bytes read", n)
				continue
			}
			tStop := time.Now()
			log.Info("<- pong", "mux", cfg.MuxAddr, "bytes", n, "rtt", tStop.Sub(tStart).String())
		}
	}
}
