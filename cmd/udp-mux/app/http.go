package app

import (
	"context"
	"net"
	"net/http"
	_ "net/http/pprof" // #nosec: G108 -- default http mux is only used when profiling is enabled
	"time"

	"github.com/go-logr/logr"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/proxy"
)

var pprofEnabled bool

func runHTTPServer(ctx context.Context, log logr.Logger, cfg *config.UdpMuxConfig, p *proxy.Proxy) error {
	mux := http.NewServeMux()
	addHealth(log, cfg, mux)
	addReadiness(log, mux)
	_ = addMetrics(log, p, mux)

	// pprof: forward /debug/pprof/* to the default mux where the import above registered them.
	if pprofEnabled {
		mux.Handle("/debug/pprof/", http.DefaultServeMux)
	}

	httpServer := &http.Server{
		Addr:              cfg.ApiListenAddr,
		ReadHeaderTimeout: 10 * time.Second,
		Handler:           mux,
		BaseContext: func(listener net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		log.Info("Shutting down HTTP server...")
		_ = httpServer.Close()
	}()

	return httpServer.ListenAndServe()
}
