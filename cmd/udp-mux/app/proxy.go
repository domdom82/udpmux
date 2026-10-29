package app

import (
	"context"

	"github.com/go-logr/logr"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/proxy"
)

func runProxy(ctx context.Context, log logr.Logger, cfg *config.UdpMuxConfig, p *proxy.Proxy) error {
	p.AddWriteHook(buildMuxHooks(log, cfg, p))
	return p.ListenAndServe(ctx, log)
}
