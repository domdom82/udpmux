package app

import (
	"context"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
)

func runProxy(ctx context.Context, log logr.Logger, cfg *config.UdpMuxConfig, p *proxy.Proxy) error {
	p.AddWriteHook(buildMuxHooks(cfg, log))
	return p.ListenAndServe(ctx, log)
}
