package app

import (
	"net/http"

	"github.com/go-logr/logr"
)

func addReadiness(log logr.Logger, mux *http.ServeMux) {
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
