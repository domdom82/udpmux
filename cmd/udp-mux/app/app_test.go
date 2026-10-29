package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
)

func TestApp(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "App Suite")
}

// newTestServer wires up the full HTTP mux (health, readiness, metrics)
// and returns a test server.
func newTestServer(cfg *config.UdpMuxConfig, p *proxy.Proxy) *httptest.Server {
	mux := http.NewServeMux()
	addHealth(logr.Discard(), cfg, mux)
	addReadiness(logr.Discard(), mux)
	_ = addMetrics(logr.Discard(), p, mux)
	return httptest.NewServer(mux)
}

// startMuxProxy starts a mux proxy on a free port and returns the proxy,
// its bound address, and a stop function. The caller must call stop() when done.
func startMuxProxy(sessionTimeout, cleanupInterval time.Duration, maxPerClient int) (*proxy.Proxy, *net.UDPAddr, func()) {
	tmp, muxAddr := bindTestUDP()
	tmp.Close()

	cfg := &config.UdpMuxConfig{
		ListenAddr:      muxAddr.String(),
		ApiListenAddr:   ":0",
		SessionTimeout:  sessionTimeout,
		CleanupInterval: cleanupInterval,
	}
	Expect(cfg.Validate()).To(Succeed())

	p := proxy.NewProxy(muxAddr.String(), "", 2, sessionTimeout, cleanupInterval, maxPerClient)
	p.AddWriteHook(buildMuxHooks(logr.Discard(), cfg, p))

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = p.ListenAndServe(ctx, logr.Discard())
	}()
	time.Sleep(30 * time.Millisecond)

	stop := func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}
	}
	return p, muxAddr, stop
}

// establishSession sends a PROXY_HELLO from srcConn to the mux and waits for MUX_HELLO.
func establishSession(srcConn *net.UDPConn, muxAddr *net.UDPAddr, backendAddr string) {
	hello := buildProxyHello(backendAddr)
	_, err := srcConn.WriteTo(hello, muxAddr)
	Expect(err).NotTo(HaveOccurred())

	// Wait for MUX_HELLO.
	buf := make([]byte, 65536)
	Expect(srcConn.SetReadDeadline(time.Now().Add(time.Second))).To(Succeed())
	n, _, err := srcConn.ReadFromUDP(buf)
	Expect(err).NotTo(HaveOccurred())
	Expect(isFlagSet(buf[:n], frame.FlagMuxHello)).To(BeTrue(), "expected MUX_HELLO")
}

// scrapeMetrics fetches /metrics from srv and returns the body as a string.
func scrapeMetrics(srv *httptest.Server) string {
	resp, err := http.Get(srv.URL + "/metrics")
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	body, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	return string(body)
}

var _ = Describe("/healthz", func() {
	var srv *httptest.Server

	BeforeEach(func() {
		srv = newTestServer(&config.UdpMuxConfig{
			ListenAddr:           ":8080",
			ApiListenAddr:        ":8081",
			SessionTimeout:       0,
			CleanupInterval:      0,
			MaxSessionsPerClient: 0,
		}, nil)
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
		srv = newTestServer(&config.UdpMuxConfig{
			ListenAddr:           ":8080",
			ApiListenAddr:        ":8081",
			SessionTimeout:       0,
			CleanupInterval:      0,
			MaxSessionsPerClient: 0,
		}, nil)
	})

	AfterEach(func() { srv.Close() })

	It("returns 200", func() {
		resp, err := http.Get(srv.URL + "/readyz")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})
