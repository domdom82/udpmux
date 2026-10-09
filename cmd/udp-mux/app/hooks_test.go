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
	h := frame.NewHeader(frame.FlagProxyHello, len(payload))
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

		cfg := config.NewUdpMuxConfig(muxAddr.String(), ":0", 30*time.Second, 25*time.Second, 5*time.Second)
		p := proxy.NewProxy(cfg.ListenAddr, "", 2, cfg.SessionTimeout)
		unwrap := buildMuxHooks(cfg, logr.Discard())
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
		cfg2 := config.NewUdpMuxConfig(muxAddr.String(), ":0", 30*time.Second, 25*time.Second, 5*time.Second)
		p2 := proxy.NewProxy(cfg2.ListenAddr, "", 2, cfg2.SessionTimeout)
		p2.AddWriteHook(buildMuxHooks(cfg2, logr.Discard()))
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

	It("refreshes session on KEEPALIVE (no backend forward)", func() {
		// Establish session.
		hello := buildProxyHello(backendAddr.String())
		_, err := proxyConn.WriteTo(hello, muxAddr)
		Expect(err).NotTo(HaveOccurred())
		reply, _, ok := readWithTimeout(proxyConn, time.Second)
		Expect(ok).To(BeTrue())
		Expect(isFlagSet(reply, frame.FlagMuxHello)).To(BeTrue())

		// Send KEEPALIVE.
		kah := frame.NewHeader(frame.FlagKeepAlive, 0)
		kaBytes, err := frame.Encode(kah)
		Expect(err).NotTo(HaveOccurred())
		_, err = proxyConn.WriteTo(kaBytes, muxAddr)
		Expect(err).NotTo(HaveOccurred())

		// Backend should NOT receive anything (keepalive is consumed by mux).
		_, _, gotData := readWithTimeout(backendConn, 200*time.Millisecond)
		Expect(gotData).To(BeFalse(), "KEEPALIVE must not be forwarded to backend")
	})
})
