package app

import (
	"fmt"
	"net/http"

	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
)

const msgNotAllowed = "method not allowed"

func addApi(log logr.Logger, p *proxy.Proxy, mux *http.ServeMux) {
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(msgNotAllowed))
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%d", p.NumSessions())
	})
}
