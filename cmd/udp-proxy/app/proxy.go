package app

import (
	"context"
	"runtime"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
)

func runProxy(ctx context.Context, log logr.Logger, cfg *config.UdpProxyConfig) error {
	p := proxy.NewProxy(cfg.ListenAddr, cfg.MuxAddr, runtime.GOMAXPROCS(0), cfg.SessionTimeout)
	write, read := buildHooks(cfg, log)
	p.AddWriteHook(write)
	p.AddReadHook(read)
	return p.ListenAndServe(ctx, log)
}
