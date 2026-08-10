package app

import (
	"net/http"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/go-logr/logr"
)

const (
	msgError        = "internal server error"
	msgNotFound     = "endpoint not found"
	msgRegistered   = "endpoint registered"
	msgUnregistered = "endpoint unregistered"
	msgNotAllowed   = "method not allowed"
)

func addApi(log logr.Logger, cfg *config.UdpMuxConfig, mux *http.ServeMux) {
	mux.HandleFunc("/api/endpoints", func(w http.ResponseWriter, r *http.Request) {
		endpoint := r.FormValue("endpoint")
		response := msgError
		code := http.StatusInternalServerError
		switch r.Method {
		case http.MethodGet:
			id, err := cfg.GetEndpointId(endpoint)
			if err != nil {
				response = msgNotFound
				code = http.StatusNotFound
				break
			}
			response = id.String()
			code = http.StatusOK
		case http.MethodPut:
			log.Info("registering endpoint", "endpoint", endpoint)
			cfg.RegisterEndpoint(endpoint)
			response = msgRegistered
			code = http.StatusOK
		case http.MethodDelete:
			log.Info("unregistering endpoint", "endpoint", endpoint)
			if err := cfg.UnregisterEndpoint(endpoint); err != nil {
				response = msgNotFound
				code = http.StatusNotFound
				break
			}
			response = msgUnregistered
			code = http.StatusOK
		default:
			response = msgNotAllowed
			code = http.StatusMethodNotAllowed
		}
		w.WriteHeader(code)
		w.Write([]byte(response))
	})

}
