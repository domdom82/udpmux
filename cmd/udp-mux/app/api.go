package app

import (
	"io"
	"net/http"
	"strings"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/go-logr/logr"
)

const (
	msgError          = "internal server error"
	msgNotFound       = "endpoint not found"
	msgRegistered     = "endpoint registered"
	msgUnregistered   = "endpoint unregistered"
	msgNotAllowed     = "method not allowed"
	msgBulkRegistered = "endpoints registered"
)

func addApi(log logr.Logger, cfg *config.UdpMuxConfig, mux *http.ServeMux) {
	mux.HandleFunc("/api/endpoints", func(w http.ResponseWriter, r *http.Request) {
		endpoint := r.FormValue("endpoint")
		response := msgError
		code := http.StatusInternalServerError
		switch r.Method {
		case http.MethodGet:
			if endpoint == "" {
				response = listEndpoints(cfg)
				code = http.StatusOK
				break
			}
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

	// PUT /api/endpoints/bulk atomically registers all endpoints in the body.
	// Body: newline-separated list of host:port strings.
	mux.HandleFunc("/api/endpoints/bulk", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(msgNotAllowed))
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(msgError))
			return
		}
		var addrs []string
		for line := range strings.SplitSeq(string(body), "\n") {
			if addr := strings.TrimSpace(line); addr != "" {
				addrs = append(addrs, addr)
			}
		}
		log.Info("registering endpoints in bulk", "count", len(addrs))
		cfg.RegisterEndpoints(addrs)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(msgBulkRegistered))
	})
}

func listEndpoints(cfg *config.UdpMuxConfig) string {
	endpointMappings := cfg.ListEndpointMappings()
	response := strings.Builder{}
	for id, endpoint := range endpointMappings {
		response.WriteString(id.String() + "\t" + endpoint + "\n")
	}
	return response.String()
}
