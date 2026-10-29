package app

import (
	"net/http"

	"github.com/go-logr/logr"

	"github.com/domdom82/udpmux/pkg/config"
)

func addHealth(log logr.Logger, cfg *config.UdpMuxConfig, mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

}
