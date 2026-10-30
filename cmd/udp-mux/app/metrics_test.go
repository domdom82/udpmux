package app

import (
	"context"
	"net"
	"net/http/httptest"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
)

// startMuxProxyWithCfg is like startMuxProxy but accepts a fully-formed config.
func startMuxProxyWithCfg(cfg *config.UdpMuxConfig, maxPerClient int) (*proxy.Proxy, func()) {
	p := proxy.NewProxy(cfg.ListenAddr, "", 2, cfg.SessionTimeout, cfg.CleanupInterval, maxPerClient)
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
	return p, stop
}

var _ = Describe("metrics counters", func() {
	var (
		p       *proxy.Proxy
		muxAddr *net.UDPAddr
		stop    func()
		backend *net.UDPConn
	)

	BeforeEach(func() {
		backend, _ = bindTestUDP()
		p, muxAddr, stop = startMuxProxy(30*time.Second, 10*time.Second, 0)
	})

	AfterEach(func() {
		stop()
		backend.Close()
	})

	It("increments SessionsExpiredCounter when a session times out", func() {
		p2, muxAddr2, stop2 := startMuxProxy(10*time.Millisecond, 20*time.Millisecond, 0)
		defer stop2()

		c, _ := bindTestUDP()
		defer c.Close()
		establishSession(c, muxAddr2, backend.LocalAddr().String())

		Eventually(func() int64 { return p2.GetMetrics().SessionsExpiredCounter.Load() },
			300*time.Millisecond, 10*time.Millisecond,
		).Should(Equal(int64(1)))
	})

	It("increments SessionsResetCounter and BogusSessionsCounter once per bogus packet", func() {
		c, _ := bindTestUDP()
		defer c.Close()

		_, err := c.WriteTo([]byte("not udpm"), muxAddr)
		Expect(err).NotTo(HaveOccurred())

		// Drain the RESET reply.
		buf := make([]byte, 65536)
		c.SetReadDeadline(time.Now().Add(time.Second))
		c.ReadFromUDP(buf)

		Eventually(func() int64 { return p.GetMetrics().SessionsResetCounter.Load() },
			200*time.Millisecond, 5*time.Millisecond,
		).Should(Equal(int64(1)))
		Expect(p.GetMetrics().BogusSessionsCounter.Load()).To(Equal(int64(1)))
	})

	It("does not double-count reset and bogus on a single packet", func() {
		c, _ := bindTestUDP()
		defer c.Close()

		for range 3 {
			_, err := c.WriteTo([]byte("garbage"), muxAddr)
			Expect(err).NotTo(HaveOccurred())
			buf := make([]byte, 65536)
			c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			c.ReadFromUDP(buf)
		}

		Expect(p.GetMetrics().SessionsResetCounter.Load()).To(Equal(int64(3)))
		Expect(p.GetMetrics().BogusSessionsCounter.Load()).To(Equal(int64(3)))
	})

	It("increments FailedHandshakesCounter on malformed PROXY_HELLO", func() {
		c, _ := bindTestUDP()
		defer c.Close()

		// Build a PROXY_HELLO with truncated payload (length byte claims 10 bytes but payload is empty).
		h, err := frame.NewHeader(frame.FlagProxyHello, 1)
		Expect(err).NotTo(HaveOccurred())
		hdr, err := frame.Encode(h)
		Expect(err).NotTo(HaveOccurred())
		malformed := append(hdr, byte(10)) // claims 10-byte endpoint, payload missing
		_, err = c.WriteTo(malformed, muxAddr)
		Expect(err).NotTo(HaveOccurred())

		buf := make([]byte, 65536)
		c.SetReadDeadline(time.Now().Add(time.Second))
		c.ReadFromUDP(buf)

		Eventually(func() int64 { return p.GetMetrics().FailedHandshakesCounter.Load() },
			200*time.Millisecond, 5*time.Millisecond,
		).Should(Equal(int64(1)))
	})

	It("increments ClientsBlockedBySessionLimitCounter when per-client limit is reached", func() {
		p2, muxAddr2, stop2 := startMuxProxy(30*time.Second, 10*time.Second, 1)
		defer stop2()

		c1, _ := bindTestUDP()
		c2, _ := bindTestUDP()
		defer c1.Close()
		defer c2.Close()

		establishSession(c1, muxAddr2, backend.LocalAddr().String())

		// Second session from same IP must be rejected.
		hello := buildProxyHello(backend.LocalAddr().String())
		_, err := c2.WriteTo(hello, muxAddr2)
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(50 * time.Millisecond)

		Expect(p2.GetMetrics().ClientsBlockedBySessionLimitCounter.Load()).To(Equal(int64(1)))
	})

	It("increments ClientsBlockedByEndpointAllowlistCounter when endpoint is denied", func() {
		tmp, addr := bindTestUDP()
		tmp.Close()

		cfg := &config.UdpMuxConfig{
			ListenAddr:        addr.String(),
			ApiListenAddr:     ":0",
			SessionTimeout:    30 * time.Second,
			CleanupInterval:   10 * time.Second,
			EndpointAllowList: []string{`^10\.`}, // only 10.x.x.x allowed; backend is 127.0.0.1
		}
		Expect(cfg.Validate()).To(Succeed())
		p2, stop2 := startMuxProxyWithCfg(cfg, 0)
		defer stop2()

		c, _ := bindTestUDP()
		defer c.Close()

		hello := buildProxyHello(backend.LocalAddr().String())
		_, err := c.WriteTo(hello, addr)
		Expect(err).NotTo(HaveOccurred())
		buf := make([]byte, 65536)
		c.SetReadDeadline(time.Now().Add(time.Second))
		c.ReadFromUDP(buf)

		Eventually(func() int64 { return p2.GetMetrics().ClientsBlockedByEndpointAllowlistCounter.Load() },
			200*time.Millisecond, 5*time.Millisecond,
		).Should(Equal(int64(1)))
	})

	It("counts bytes exactly once per direction for a single packet", func() {
		c, _ := bindTestUDP()
		defer c.Close()
		establishSession(c, muxAddr, backend.LocalAddr().String())

		// Capture baseline after handshake — handshake bytes are already counted.
		baselineFrontendRead := p.GetMetrics().FrontendBytesReadCounter.Load()
		baselineBackendWritten := p.GetMetrics().BackendBytesWrittenCounter.Load()

		payload := []byte("hello")
		_, err := c.WriteTo(payload, muxAddr)
		Expect(err).NotTo(HaveOccurred())

		// Wait for the backend to receive the packet.
		buf := make([]byte, 65536)
		backend.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := backend.ReadFromUDP(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(buf[:n]).To(Equal(payload))

		Expect(p.GetMetrics().FrontendBytesReadCounter.Load() - baselineFrontendRead).To(Equal(int64(len(payload))))
		Expect(p.GetMetrics().BackendBytesWrittenCounter.Load() - baselineBackendWritten).To(Equal(int64(len(payload))))
	})

	It("counts return bytes exactly once per direction", func() {
		c, _ := bindTestUDP()
		defer c.Close()
		establishSession(c, muxAddr, backend.LocalAddr().String())

		// Send to backend first so it learns the mux-side return address.
		_, err := c.WriteTo([]byte("ping"), muxAddr)
		Expect(err).NotTo(HaveOccurred())

		// Read the forwarded packet at the backend and echo it back.
		buf := make([]byte, 65536)
		backend.SetReadDeadline(time.Now().Add(time.Second))
		_, backendReplyAddr, err := backend.ReadFromUDP(buf)
		Expect(err).NotTo(HaveOccurred())

		// Capture baseline before the reply arrives.
		baselineBackendRead := p.GetMetrics().BackendBytesReadCounter.Load()
		baselineFrontendWritten := p.GetMetrics().FrontendBytesWrittenCounter.Load()

		reply := []byte("pong")
		_, err = backend.WriteTo(reply, backendReplyAddr)
		Expect(err).NotTo(HaveOccurred())

		// Wait for the client to receive the reply.
		c.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := c.ReadFromUDP(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(buf[:n]).To(Equal(reply))

		Expect(p.GetMetrics().BackendBytesReadCounter.Load() - baselineBackendRead).To(Equal(int64(len(reply))))
		Expect(p.GetMetrics().FrontendBytesWrittenCounter.Load() - baselineFrontendWritten).To(Equal(int64(len(reply))))
	})
})

var _ = Describe("/metrics", func() {
	var srv *httptest.Server

	AfterEach(func() { srv.Close() })

	Context("with no active proxy (nil)", func() {
		BeforeEach(func() {
			srv = newTestServer(&config.UdpMuxConfig{
				ListenAddr:           ":8080",
				ApiListenAddr:        ":8081",
				SessionTimeout:       0,
				CleanupInterval:      0,
				MaxSessionsPerClient: 0,
			}, nil)
		})

		It("returns 200 and exposes metrics", func() {
			body := scrapeMetrics(srv)
			Expect(body).To(ContainSubstring("udpmux_sessions"))
			Expect(body).To(ContainSubstring("udpmux_clients"))
			Expect(body).To(ContainSubstring("udpmux_avg_sessions_per_client"))
		})

		It("reports zero for all gauges", func() {
			body := scrapeMetrics(srv)
			Expect(body).To(ContainSubstring("udpmux_sessions 0"))
			Expect(body).To(ContainSubstring("udpmux_clients 0"))
			Expect(body).To(ContainSubstring("udpmux_avg_sessions_per_client 0"))
		})
	})

	Context("with active sessions", func() {
		var (
			p       *proxy.Proxy
			muxAddr *net.UDPAddr
			stop    func()
			backend *net.UDPConn
		)

		BeforeEach(func() {
			// Start a real backend to accept the dialed connections from the mux.
			backend, _ = bindTestUDP()

			p, muxAddr, stop = startMuxProxy(30*time.Second, 10*time.Second, 0)
			srv = newTestServer(nil, p)
		})

		AfterEach(func() {
			stop()
			backend.Close()
		})

		It("counts two sessions from the same client IP as 1 client", func() {
			// Two connections from distinct ports on 127.0.0.1 → same client IP.
			c1, _ := bindTestUDP()
			c2, _ := bindTestUDP()
			defer c1.Close()
			defer c2.Close()

			establishSession(c1, muxAddr, backend.LocalAddr().String())
			establishSession(c2, muxAddr, backend.LocalAddr().String())

			body := scrapeMetrics(srv)
			Expect(body).To(ContainSubstring("udpmux_sessions 2"))
			Expect(body).To(ContainSubstring("udpmux_clients 1"))
			// ceil(2 sessions / 1 client) = 2
			Expect(body).To(ContainSubstring("udpmux_avg_sessions_per_client 2"))
		})

		It("counts sessions from distinct client IPs separately", func() {
			c1, _ := bindTestUDP()
			defer c1.Close()
			establishSession(c1, muxAddr, backend.LocalAddr().String())

			body := scrapeMetrics(srv)
			Expect(body).To(ContainSubstring("udpmux_sessions 1"))
			Expect(body).To(ContainSubstring("udpmux_clients 1"))
			// ceil(1 / 1) = 1
			Expect(body).To(ContainSubstring("udpmux_avg_sessions_per_client 1"))
		})

		It("returns all zeros after sessions expire", func() {
			p2, muxAddr2, stop2 := startMuxProxy(10*time.Millisecond, 20*time.Millisecond, 0)
			defer stop2()
			srv2 := newTestServer(nil, p2)
			defer srv2.Close()

			c, _ := bindTestUDP()
			defer c.Close()
			establishSession(c, muxAddr2, backend.LocalAddr().String())

			Expect(scrapeMetrics(srv2)).To(ContainSubstring("udpmux_sessions 1"))

			Eventually(func() string { return scrapeMetrics(srv2) },
				300*time.Millisecond, 10*time.Millisecond,
			).Should(SatisfyAll(
				ContainSubstring("udpmux_sessions 0"),
				ContainSubstring("udpmux_clients 0"),
				ContainSubstring("udpmux_avg_sessions_per_client 0"),
			))
		})
	})
})
