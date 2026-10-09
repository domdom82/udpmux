package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/proxy"
)

func TestApp(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "App Suite")
}

// newTestServer wires up the full HTTP mux (health, readiness, metrics, api)
// and returns a test server along with the config it shares.
func newTestServer(cfg *config.UdpMuxConfig, p *proxy.Proxy) *httptest.Server {
	mux := http.NewServeMux()
	addHealth(logr.Discard(), cfg, mux)
	addReadiness(logr.Discard(), mux)
	addMetrics(logr.Discard(), cfg, mux)
	addApi(logr.Discard(), p, mux)
	return httptest.NewServer(mux)
}

var _ = Describe("/healthz", func() {
	var srv *httptest.Server

	BeforeEach(func() {
		srv = newTestServer(config.NewUdpMuxConfig(":8080", ":8081", 0, 20*time.Second, 10*time.Second), nil)
	})

	AfterEach(func() { srv.Close() })

	It("returns 200", func() {
		resp, err := http.Get(srv.URL + "/healthz")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})

var _ = Describe("/readyz", func() {
	var srv *httptest.Server

	BeforeEach(func() {
		srv = newTestServer(config.NewUdpMuxConfig(":8080", ":8081", 0, 20*time.Second, 10*time.Second), nil)
	})

	AfterEach(func() { srv.Close() })

	It("returns 200", func() {
		resp, err := http.Get(srv.URL + "/readyz")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})

var _ = Describe("/metrics", func() {
	var srv *httptest.Server

	BeforeEach(func() {
		srv = newTestServer(config.NewUdpMuxConfig(":8080", ":8081", 0, 20*time.Second, 10*time.Second), nil)
	})

	AfterEach(func() { srv.Close() })

	It("returns 501 Not Implemented", func() {
		resp, err := http.Get(srv.URL + "/metrics")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNotImplemented))
	})
})

var _ = Describe("/api/sessions", func() {
	var srv *httptest.Server

	BeforeEach(func() {
		srv = newTestServer(config.NewUdpMuxConfig(":8080", ":8081", 0, 20*time.Second, 10*time.Second), nil)
	})

	AfterEach(func() { srv.Close() })

	It("returns 200 with session count 0 when no proxy is active", func() {
		resp, err := http.Get(srv.URL + "/api/sessions")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		body, _ := io.ReadAll(resp.Body)
		Expect(string(body)).To(Equal("0"))
	})

	It("returns 405 for non-GET methods", func() {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/sessions", nil)
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
	})
})
