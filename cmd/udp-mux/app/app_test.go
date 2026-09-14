package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/domdom82/udpmux/pkg/config"
)

func TestApp(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "App Suite")
}

// newTestServer wires up the full HTTP mux (health, readiness, metrics, api)
// and returns a test server along with the config it shares.
func newTestServer(cfg *config.UdpMuxConfig) *httptest.Server {
	mux := http.NewServeMux()
	addHealth(logr.Discard(), cfg, mux)
	addReadiness(logr.Discard(), cfg, mux)
	addMetrics(logr.Discard(), cfg, mux)
	addApi(logr.Discard(), cfg, mux)
	return httptest.NewServer(mux)
}

var _ = Describe("/healthz", func() {
	var (
		cfg *config.UdpMuxConfig
		srv *httptest.Server
	)

	BeforeEach(func() {
		cfg = config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV1)
		srv = newTestServer(cfg)
	})

	AfterEach(func() { srv.Close() })

	It("returns 200 regardless of configuration", func() {
		resp, err := http.Get(srv.URL + "/healthz")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})

var _ = Describe("/readyz", func() {
	var (
		cfg *config.UdpMuxConfig
		srv *httptest.Server
	)

	AfterEach(func() { srv.Close() })

	Context("with protocol v1", func() {
		BeforeEach(func() {
			cfg = config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV1)
			srv = newTestServer(cfg)
		})

		It("returns 200 even without endpoints", func() {
			resp, err := http.Get(srv.URL + "/readyz")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Context("with protocol v2 and no endpoints", func() {
		BeforeEach(func() {
			cfg = config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV2)
			srv = newTestServer(cfg)
		})

		It("returns 503", func() {
			resp, err := http.Get(srv.URL + "/readyz")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})
	})

	Context("with protocol v2 and at least one endpoint", func() {
		BeforeEach(func() {
			cfg = config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV2)
			cfg.RegisterEndpoint("10.0.0.1:1194")
			srv = newTestServer(cfg)
		})

		It("returns 200", func() {
			resp, err := http.Get(srv.URL + "/readyz")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})
})

var _ = Describe("/metrics", func() {
	var srv *httptest.Server

	BeforeEach(func() {
		cfg := config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV1)
		srv = newTestServer(cfg)
	})

	AfterEach(func() { srv.Close() })

	It("returns 501 Not Implemented", func() {
		resp, err := http.Get(srv.URL + "/metrics")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNotImplemented))
	})
})

var _ = Describe("/api/endpoints", func() {
	var (
		cfg *config.UdpMuxConfig
		srv *httptest.Server
	)

	BeforeEach(func() {
		cfg = config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV2)
		srv = newTestServer(cfg)
	})

	AfterEach(func() { srv.Close() })

	Describe("PUT", func() {
		It("registers an endpoint and returns 200", func() {
			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints?endpoint=10.0.0.1:1194", nil)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(cfg.NumEndpoints()).To(Equal(1))
		})

		It("is idempotent — re-registering the same endpoint keeps count at 1", func() {
			for range 3 {
				req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints?endpoint=host:9000", nil)
				resp, _ := http.DefaultClient.Do(req)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			}
			Expect(cfg.NumEndpoints()).To(Equal(1))
		})
	})

	Describe("GET", func() {
		BeforeEach(func() {
			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints?endpoint=10.0.0.1:1194", nil)
			_, _ = http.DefaultClient.Do(req)
		})

		It("returns the endpoint id when queried by address", func() {
			resp, err := http.Get(srv.URL + "/api/endpoints?endpoint=10.0.0.1:1194")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("returns 404 for an unknown endpoint", func() {
			resp, err := http.Get(srv.URL + "/api/endpoints?endpoint=unknown:1")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("lists all endpoints when no query param is given", func() {
			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints?endpoint=a:1", nil)
			_, _ = http.DefaultClient.Do(req)
			req, _ = http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints?endpoint=b:2", nil)
			_, _ = http.DefaultClient.Do(req)

			resp, err := http.Get(srv.URL + "/api/endpoints")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("DELETE", func() {
		BeforeEach(func() {
			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints?endpoint=10.0.0.1:1194", nil)
			_, _ = http.DefaultClient.Do(req)
		})

		It("removes a registered endpoint and returns 200", func() {
			req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/endpoints?endpoint=10.0.0.1:1194", nil)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(cfg.NumEndpoints()).To(BeZero())
		})

		It("returns 404 when deleting an unknown endpoint", func() {
			req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/endpoints?endpoint=nope:1", nil)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("unsupported methods", func() {
		It("returns 405 for PATCH", func() {
			req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/endpoints?endpoint=x:1", nil)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})

		It("returns 405 for POST", func() {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/endpoints?endpoint=x:1", nil)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})
	})
})

var _ = Describe("/api/endpoints/bulk", func() {
	var (
		cfg *config.UdpMuxConfig
		srv *httptest.Server
	)

	BeforeEach(func() {
		cfg = config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV2)
		srv = newTestServer(cfg)
	})

	AfterEach(func() { srv.Close() })

	Describe("PUT", func() {
		It("registers all endpoints atomically and returns 200", func() {
			body := strings.NewReader("10.0.0.1:1194\n10.0.0.2:1194\n10.0.0.3:1194\n")
			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints/bulk", body)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(cfg.NumEndpoints()).To(Equal(3))
		})

		It("adds to existing endpoints — previous entries are kept", func() {
			cfg.RegisterEndpoint("old:1")
			cfg.RegisterEndpoint("old:2")

			body := strings.NewReader("new:1\nnew:2\n")
			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints/bulk", body)
			_, _ = http.DefaultClient.Do(req)

			Expect(cfg.NumEndpoints()).To(Equal(4))
			_, err := cfg.GetEndpointId("old:1")
			Expect(err).NotTo(HaveOccurred())
			_, err = cfg.GetEndpointId("new:1")
			Expect(err).NotTo(HaveOccurred())
		})

		It("accepts an empty body and leaves existing endpoints unchanged", func() {
			cfg.RegisterEndpoint("10.0.0.1:1194")

			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints/bulk", strings.NewReader(""))
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(cfg.NumEndpoints()).To(Equal(1))
		})

		It("ignores blank lines and whitespace", func() {
			body := strings.NewReader("\n  \n10.0.0.1:1194\n\n  10.0.0.2:1194  \n")
			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints/bulk", body)
			resp, _ := http.DefaultClient.Do(req)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(cfg.NumEndpoints()).To(Equal(2))
		})
	})

	Describe("unsupported methods", func() {
		It("returns 405 for POST", func() {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/endpoints/bulk", nil)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})

		It("returns 405 for GET", func() {
			resp, err := http.Get(srv.URL + "/api/endpoints/bulk")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})
	})
})
