package app

import (
	"math"
	"net/http"

	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func addMetrics(log logr.Logger, p *proxy.Proxy, mux *http.ServeMux) *prometheus.Registry {
	reg := prometheus.NewRegistry()

	sessionsGauge := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "udpmux_sessions",
			Help: "Current number of active client sessions.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.SessionsGauge.Load())
		},
	)
	if err := reg.Register(sessionsGauge); err != nil {
		log.Error(err, "failed to register sessions gauge")
	}

	clientsGauge := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "udpmux_clients",
			Help: "Current number of active clients.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.ClientsGauge.Load())
		},
	)
	if err := reg.Register(clientsGauge); err != nil {
		log.Error(err, "failed to register clients gauge")
	}

	avgSessionsPerClientGauge := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "udpmux_avg_sessions_per_client",
			Help: "Average rounded number of sessions per client.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			clients := m.ClientsGauge.Load()
			if clients == 0 {
				return 0
			}
			return math.Ceil(float64(m.SessionsGauge.Load()) / float64(clients))
		},
	)
	if err := reg.Register(avgSessionsPerClientGauge); err != nil {
		log.Error(err, "failed to register avg sessions per client gauge")
	}

	sessionsExpiredCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_sessions_expired_total",
			Help: "Total number of sessions expired due to inactivity.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.SessionsExpiredCounter.Load())
		},
	)
	if err := reg.Register(sessionsExpiredCounter); err != nil {
		log.Error(err, "failed to register sessions expired counter")
	}

	sessionsResetCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_sessions_reset_total",
			Help: "Total number of sessions reset due to unexpected packets.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.SessionsResetCounter.Load())
		},
	)
	if err := reg.Register(sessionsResetCounter); err != nil {
		log.Error(err, "failed to register sessions reset counter")
	}

	failedHandshakesCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_failed_handshakes_total",
			Help: "Total number of failed handshakes.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.FailedHandshakesCounter.Load())
		},
	)
	if err := reg.Register(failedHandshakesCounter); err != nil {
		log.Error(err, "failed to register failed handshakes counter")
	}

	clientsBlockedByEndpointAllowlistCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_clients_blocked_by_endpoint_allowlist_total",
			Help: "Total number of clients blocked by endpoint allow-list.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.ClientsBlockedByEndpointAllowlistCounter.Load())
		},
	)
	if err := reg.Register(clientsBlockedByEndpointAllowlistCounter); err != nil {
		log.Error(err, "failed to register clients blocked by endpoint allow-list counter")
	}

	clientsBlockedBySessionLimitCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_clients_blocked_by_session_limit_total",
			Help: "Total number of clients blocked by session limit.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.ClientsBlockedBySessionLimitCounter.Load())
		},
	)
	if err := reg.Register(clientsBlockedBySessionLimitCounter); err != nil {
		log.Error(err, "failed to register clients blocked by session limit counter")
	}

	bogusSessionsCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_bogus_sessions_total",
			Help: "Total number of bogus sessions.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.BogusSessionsCounter.Load())
		},
	)
	if err := reg.Register(bogusSessionsCounter); err != nil {
		log.Error(err, "failed to register bogus sessions counter")
	}

	frontendBytesReadCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_frontend_bytes_read_total",
			Help: "Total number of bytes read from the frontend.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.FrontendBytesReadCounter.Load())
		},
	)
	if err := reg.Register(frontendBytesReadCounter); err != nil {
		log.Error(err, "failed to register frontend bytes read counter")
	}

	frontendBytesWrittenCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_frontend_bytes_written_total",
			Help: "Total number of bytes written to the frontend.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.FrontendBytesWrittenCounter.Load())
		},
	)
	if err := reg.Register(frontendBytesWrittenCounter); err != nil {
		log.Error(err, "failed to register frontend bytes written counter")
	}

	backendBytesReadCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_backend_bytes_read_total",
			Help: "Total number of bytes read from the backend.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.BackendBytesReadCounter.Load())
		},
	)
	if err := reg.Register(backendBytesReadCounter); err != nil {
		log.Error(err, "failed to register backend bytes read counter")
	}

	backendBytesWrittenCounter := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "udpmux_backend_bytes_written_total",
			Help: "Total number of bytes written to the backend.",
		},
		func() float64 {
			m := p.GetMetrics()
			if m == nil {
				return 0
			}
			return float64(m.BackendBytesWrittenCounter.Load())
		},
	)
	if err := reg.Register(backendBytesWrittenCounter); err != nil {
		log.Error(err, "failed to register backend bytes written counter")
	}

	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	}))

	return reg
}
