package app

import (
	"net/http"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/go-logr/logr"
)

func addHealth(log logr.Logger, cfg *config.UdpMuxConfig, mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

}
