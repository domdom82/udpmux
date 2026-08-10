package app

import (
	"context"
	"net"
	"net/http"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/go-logr/logr"
)

func runHTTPServer(ctx context.Context, log logr.Logger, cfg *config.UdpMuxConfig) error {
	mux := http.NewServeMux()
	addHealth(log, cfg, mux)
	addReadiness(log, cfg, mux)
	addMetrics(log, cfg, mux)
	addApi(log, cfg, mux)

	httpServer := &http.Server{
		Addr:    cfg.ApiListenAddr,
		Handler: mux,
		BaseContext: func(listener net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		log.Info("Shutting down HTTP server...")
		_ = httpServer.Shutdown(context.Background())
	}()

	return httpServer.ListenAndServe() //TODO: add TLS
}
