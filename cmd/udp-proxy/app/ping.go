package app

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
)

const (
	pingInterval = 1 * time.Second
	pingTimeout  = 5 * time.Second
	pingDefault  = "PING"
)

func runPing(ctx context.Context, log logr.Logger, cfg *config.UdpProxyConfig) error {
	tick := time.NewTicker(pingInterval)
	localProxyAddr, err := net.ResolveUDPAddr("udp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("invalid local proxy address '%s': %w", localProxyAddr.String(), err)
	}
	localProxyConn, err := proxy.DialBackend(localProxyAddr)
	if err != nil {
		return fmt.Errorf("failed to dial local proxy '%s': %w", localProxyAddr, err)
	}

	var (
		pingData []byte
		pongData []byte
		n        int
	)

	for {
		select {
		case <-ctx.Done():
			log.Info("ping loop stopped")
			return nil
		case <-tick.C:
			pingData = []byte(pingDefault)
			if cfg.PingSize > 0 {
				pingData = make([]byte, cfg.PingSize)
				for i := 0; i < cfg.PingSize; i++ {
					pingData[i] = byte('A') // Fill with some data
				}
			}
			tStart := time.Now()
			n, err = localProxyConn.Write(pingData)
			if err != nil {
				log.Error(err, "failed to send ping frame", "bytes written", n)
				continue
			}
			log.Info("-> ping frame", "mux", cfg.MuxAddr, "bytes", n)
			pongData = []byte(pingDefault)
			if cfg.PingSize > 0 {
				pongData = make([]byte, cfg.PingSize)
			}
			_ = localProxyConn.SetReadDeadline(time.Now().Add(pingTimeout))
			n, err = localProxyConn.Read(pongData)
			if err != nil {
				log.Error(err, "failed to receive pong frame", "bytes read", n)
				continue
			}
			tStop := time.Now()
			log.Info("<- pong frame", "mux", cfg.MuxAddr, "bytes", n, "rtt", tStop.Sub(tStart).String())
		}
	}
}
