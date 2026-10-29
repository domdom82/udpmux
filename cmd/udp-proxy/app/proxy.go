package app

import (
	"context"
	"runtime"

	"github.com/go-logr/logr"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/proxy"
)

func runProxy(ctx context.Context, log logr.Logger, cfg *config.UdpProxyConfig) error {
	p := proxy.NewProxy(cfg.ListenAddr, cfg.MuxAddr, runtime.GOMAXPROCS(0), cfg.SessionTimeout, cfg.CleanupInterval, cfg.MaxSessionsPerClient)
	write, read := buildHooks(cfg, log)
	p.AddWriteHook(write)
	p.AddReadHook(read)
	return p.ListenAndServe(ctx, log)
}
