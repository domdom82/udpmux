package app

import (
	"context"
	"encoding/binary"
	"net"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
)

// bindTestUDP binds a random local UDP port and returns conn + address.
func bindTestUDP() (*net.UDPConn, *net.UDPAddr) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	Expect(err).NotTo(HaveOccurred())
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

// readWithTimeout reads one datagram from conn within the given duration.
func readWithTimeout(conn *net.UDPConn, timeout time.Duration) ([]byte, *net.UDPAddr, bool) {
	buf := make([]byte, 65536)
	Expect(conn.SetReadDeadline(time.Now().Add(timeout))).To(Succeed())
	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, nil, false
	}
	return buf[:n], addr, true
}

// buildProxyHello builds a raw PROXY_HELLO frame for the given endpoint string.
func buildProxyHello(endpoint string) []byte {
	ep := []byte(endpoint)
	payload := append([]byte{byte(len(ep))}, ep...)
	h, err := frame.NewHeader(frame.FlagProxyHello, len(payload))
	Expect(err).NotTo(HaveOccurred())
	hdr, err := frame.Encode(h)
	Expect(err).NotTo(HaveOccurred())
	return append(hdr, payload...)
}

// isFlagSet returns true if data is a control frame with the given flag set.
func isFlagSet(data []byte, flag uint16) bool {
	if len(data) < frame.HeaderLength {
		return false
	}
	if binary.BigEndian.Uint32(data[:4]) != frame.Magic {
		return false
	}
	return binary.BigEndian.Uint16(data[4:6])&flag != 0
}

// openCfg returns a validated UdpMuxConfig with no allow-list restrictions.
func openCfg() *config.UdpMuxConfig {
	cfg := &config.UdpMuxConfig{
		ListenAddr:      ":0",
		ApiListenAddr:   ":0",
		SessionTimeout:  30 * time.Second,
		CleanupInterval: 10 * time.Second,
	}
	Expect(cfg.Validate()).To(Succeed())
	return cfg
}

var _ = Describe("mux-side protocol", func() {
	var (
		backendConn *net.UDPConn
		backendAddr *net.UDPAddr
		muxAddr     *net.UDPAddr
		proxyConn   *net.UDPConn // simulates the udp-proxy sending to the mux
		ctx         context.Context
		cancel      context.CancelFunc
		stopped     chan struct{}
	)

	BeforeEach(func() {
		backendConn, backendAddr = bindTestUDP()

		// Reserve a port for the mux then release it so the mux can bind it.
		tmp, addr := bindTestUDP()
		tmp.Close()
		muxAddr = addr

		// The simulated proxy connects from a fixed ephemeral port.
		proxyConn, _ = bindTestUDP()

		ctx, cancel = context.WithCancel(context.Background())
		stopped = make(chan struct{})

		p := proxy.NewProxy(muxAddr.String(), "", 2, 30*time.Second, 10*time.Second, 2)
		unwrap := buildMuxHooks(logr.Discard(), openCfg(), p)
		p.AddWriteHook(unwrap)

		go func() {
			defer close(stopped)
			_ = p.ListenAndServe(ctx, logr.Discard())
		}()
		time.Sleep(30 * time.Millisecond) // let mux bind
	})

	AfterEach(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}
		backendConn.Close()
		proxyConn.Close()
	})

	It("responds with MUX_HELLO on a valid PROXY_HELLO", func() {
		hello := buildProxyHello(backendAddr.String())
		_, err := proxyConn.WriteTo(hello, muxAddr)
		Expect(err).NotTo(HaveOccurred())

		reply, _, ok := readWithTimeout(proxyConn, time.Second)
		Expect(ok).To(BeTrue(), "expected MUX_HELLO reply")
		Expect(isFlagSet(reply, frame.FlagMuxHello)).To(BeTrue(), "expected FlagMuxHello in reply")
	})

	It("forwards raw data to backend after handshake", func() {
		// Handshake
		hello := buildProxyHello(backendAddr.String())
		_, err := proxyConn.WriteTo(hello, muxAddr)
		Expect(err).NotTo(HaveOccurred())
		reply, _, ok := readWithTimeout(proxyConn, time.Second)
		Expect(ok).To(BeTrue())
		Expect(isFlagSet(reply, frame.FlagMuxHello)).To(BeTrue())

		// Send raw data
		payload := []byte("hello backend")
		_, err = proxyConn.WriteTo(payload, muxAddr)
		Expect(err).NotTo(HaveOccurred())

		// Backend should receive it verbatim
		got, _, ok := readWithTimeout(backendConn, time.Second)
		Expect(ok).To(BeTrue(), "backend should receive forwarded data")
		Expect(got).To(Equal(payload))
	})

	It("sends RESET when receiving raw data on an unknown session", func() {
		// New conn = unknown session to the mux
		unknownConn, _ := bindTestUDP()
		defer unknownConn.Close()

		// Send raw data without handshake
		_, err := unknownConn.WriteTo([]byte("unexpected raw"), muxAddr)
		Expect(err).NotTo(HaveOccurred())

		reply, _, ok := readWithTimeout(unknownConn, time.Second)
		Expect(ok).To(BeTrue(), "expected RESET reply")
		Expect(isFlagSet(reply, frame.FlagReset)).To(BeTrue(), "expected FlagReset in reply")
	})

	It("re-establishes session after receiving RESET (session recovery)", func() {
		// First, establish a session normally.
		hello := buildProxyHello(backendAddr.String())
		_, err := proxyConn.WriteTo(hello, muxAddr)
		Expect(err).NotTo(HaveOccurred())
		reply, _, ok := readWithTimeout(proxyConn, time.Second)
		Expect(ok).To(BeTrue())
		Expect(isFlagSet(reply, frame.FlagMuxHello)).To(BeTrue())

		// Simulate mux losing session state by starting a fresh mux with the same port.
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}

		// Start a new mux on the same address (simulates pod restart).
		ctx2, cancel2 := context.WithCancel(context.Background())
		stopped2 := make(chan struct{})
		defer cancel2()
		p2 := proxy.NewProxy(muxAddr.String(), "", 2, 30*time.Second, 10*time.Second, 2)
		p2.AddWriteHook(buildMuxHooks(logr.Discard(), openCfg(), p2))
		go func() {
			defer close(stopped2)
			_ = p2.ListenAndServe(ctx2, logr.Discard())
		}()
		time.Sleep(30 * time.Millisecond)

		// Send raw data to new mux — should get RESET.
		_, err = proxyConn.WriteTo([]byte("stale data"), muxAddr)
		Expect(err).NotTo(HaveOccurred())
		resetReply, _, ok := readWithTimeout(proxyConn, time.Second)
		Expect(ok).To(BeTrue(), "expected RESET from new mux")
		Expect(isFlagSet(resetReply, frame.FlagReset)).To(BeTrue())

		// Re-handshake.
		_, err = proxyConn.WriteTo(buildProxyHello(backendAddr.String()), muxAddr)
		Expect(err).NotTo(HaveOccurred())
		reply2, _, ok := readWithTimeout(proxyConn, time.Second)
		Expect(ok).To(BeTrue(), "expected MUX_HELLO after re-handshake")
		Expect(isFlagSet(reply2, frame.FlagMuxHello)).To(BeTrue())

		// Verify data flows after re-handshake.
		_, err = proxyConn.WriteTo([]byte("after recovery"), muxAddr)
		Expect(err).NotTo(HaveOccurred())
		got, _, ok := readWithTimeout(backendConn, time.Second)
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal([]byte("after recovery")))
	})

	It("expires bogus sessions after maxResetsPerSession non-UDPM packets", func() {
		// Send maxResetsPerSession non-UDPM packets from the established proxyConn.
		// The mux will send a RESET for each and then call s.Expire(), making the session
		// eligible for immediate cleanup on the next cleanup tick.
		for range maxResetsPerSession {
			_, err := proxyConn.WriteTo([]byte("not udpm"), muxAddr)
			Expect(err).NotTo(HaveOccurred())
			// Drain the RESET reply so the socket buffer doesn't fill up.
			readWithTimeout(proxyConn, 200*time.Millisecond)
		}

		// The cleanup interval in BeforeEach is 10s — too slow. Restart the mux with a
		// short cleanup interval for this sub-test.
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}

		ctx2, cancel2 := context.WithCancel(context.Background())
		stopped2 := make(chan struct{})
		defer cancel2()
		p2 := proxy.NewProxy(muxAddr.String(), "", 2, 30*time.Second, 50*time.Millisecond, 0)
		p2.AddWriteHook(buildMuxHooks(logr.Discard(), openCfg(), p2))
		go func() {
			defer close(stopped2)
			_ = p2.ListenAndServe(ctx2, logr.Discard())
		}()
		time.Sleep(30 * time.Millisecond)

		// Flood the new mux with non-UDPM packets from N distinct ports.
		const N = 20
		conns := make([]*net.UDPConn, N)
		for i := range N {
			conns[i], _ = bindTestUDP()
		}
		defer func() {
			for _, c := range conns {
				c.Close()
			}
		}()

		for _, c := range conns {
			for range maxResetsPerSession {
				_, _ = c.WriteTo([]byte("garbage"), muxAddr)
				readWithTimeout(c, 50*time.Millisecond)
			}
		}

		// Wait for several cleanup ticks to ensure all expired sessions are reaped.
		time.Sleep(500 * time.Millisecond)

		Expect(p2.GetMetrics().SessionsGauge.Load()).To(Equal(int64(0)), "all bogus sessions should have been expired and reaped")
	})

	It("rejects sessions beyond the per-client limit", func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}

		const limit = 3
		ctx2, cancel2 := context.WithCancel(context.Background())
		stopped2 := make(chan struct{})
		defer cancel2()
		p2 := proxy.NewProxy(muxAddr.String(), "", 2, 30*time.Second, 10*time.Second, limit)
		p2.AddWriteHook(buildMuxHooks(logr.Discard(), openCfg(), p2))
		go func() {
			defer close(stopped2)
			_ = p2.ListenAndServe(ctx2, logr.Discard())
		}()
		time.Sleep(30 * time.Millisecond)

		// Send PROXY_HELLO from limit+2 distinct ports on 127.0.0.1.
		const total = limit + 2
		conns := make([]*net.UDPConn, total)
		for i := range total {
			conns[i], _ = bindTestUDP()
		}
		defer func() {
			for _, c := range conns {
				c.Close()
			}
		}()

		accepted := 0
		for _, c := range conns {
			hello := buildProxyHello(backendAddr.String())
			_, err := c.WriteTo(hello, muxAddr)
			Expect(err).NotTo(HaveOccurred())
			_, _, ok := readWithTimeout(c, 300*time.Millisecond)
			if ok {
				accepted++
			}
		}

		Expect(accepted).To(Equal(limit), "only limit sessions should be accepted; excess must be silently dropped")
		Expect(p2.GetMetrics().SessionsGauge.Load()).To(BeNumerically("<=", int64(limit)))
	})

})

var _ = Describe("endpoint allow-list", func() {
	var (
		backendConn *net.UDPConn
		backendAddr *net.UDPAddr
		muxAddr     *net.UDPAddr
		proxyConn   *net.UDPConn
		ctx         context.Context
		cancel      context.CancelFunc
		stopped     chan struct{}
	)

	startMuxWithAllowList := func(patterns []string) {
		cfg := &config.UdpMuxConfig{
			ListenAddr:        muxAddr.String(),
			ApiListenAddr:     ":0",
			SessionTimeout:    30 * time.Second,
			CleanupInterval:   10 * time.Second,
			EndpointAllowList: patterns,
		}
		Expect(cfg.Validate()).To(Succeed())

		p := proxy.NewProxy(muxAddr.String(), "", 2, 30*time.Second, 10*time.Second, 0)
		p.AddWriteHook(buildMuxHooks(logr.Discard(), cfg, p))

		ctx, cancel = context.WithCancel(context.Background())
		stopped = make(chan struct{})
		go func() {
			defer close(stopped)
			_ = p.ListenAndServe(ctx, logr.Discard())
		}()
		time.Sleep(30 * time.Millisecond)
	}

	BeforeEach(func() {
		backendConn, backendAddr = bindTestUDP()
		tmp, addr := bindTestUDP()
		tmp.Close()
		muxAddr = addr
		proxyConn, _ = bindTestUDP()
	})

	AfterEach(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}
		backendConn.Close()
		proxyConn.Close()
	})

	It("allows an endpoint matching a pattern in the allow-list", func() {
		// Pattern matches 127.0.0.1:<any port>
		startMuxWithAllowList([]string{`^127\.0\.0\.1:\d+$`})

		hello := buildProxyHello(backendAddr.String())
		_, err := proxyConn.WriteTo(hello, muxAddr)
		Expect(err).NotTo(HaveOccurred())

		reply, _, ok := readWithTimeout(proxyConn, time.Second)
		Expect(ok).To(BeTrue(), "expected MUX_HELLO for allowed endpoint")
		Expect(isFlagSet(reply, frame.FlagMuxHello)).To(BeTrue())
	})

	It("sends RESET for an endpoint not matching any allow-list pattern", func() {
		// Pattern only matches 10.x.x.x addresses — backend is 127.0.0.1, so it must be rejected.
		startMuxWithAllowList([]string{`^10\.\d+\.\d+\.\d+:\d+$`})

		hello := buildProxyHello(backendAddr.String())
		_, err := proxyConn.WriteTo(hello, muxAddr)
		Expect(err).NotTo(HaveOccurred())

		reply, _, ok := readWithTimeout(proxyConn, time.Second)
		Expect(ok).To(BeTrue(), "expected RESET for denied endpoint")
		Expect(isFlagSet(reply, frame.FlagReset)).To(BeTrue())
	})
})
