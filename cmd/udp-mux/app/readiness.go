package app

import (
	"net/http"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/go-logr/logr"
)

func addReadiness(log logr.Logger, cfg *config.UdpMuxConfig, mux *http.ServeMux) {
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {

		if cfg.Protocol == config.ProtocolV2 && cfg.NumEndpoints() == 0 {
			log.Info("readiness check failed: v2 protocol required and no endpoints configured")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

}
