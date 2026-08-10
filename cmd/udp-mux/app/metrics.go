package app

import (
	"net/http"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/go-logr/logr"
)

func addMetrics(log logr.Logger, cfg *config.UdpMuxConfig, mux *http.ServeMux) {
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})

}
