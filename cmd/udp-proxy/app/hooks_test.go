package app

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
)

func TestProxyApp(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ProxyApp Suite")
}

func bindUDP() (*net.UDPConn, *net.UDPAddr) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	Expect(err).NotTo(HaveOccurred())
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

func readWithTimeout(conn *net.UDPConn, timeout time.Duration) ([]byte, bool) {
	buf := make([]byte, 65536)
	Expect(conn.SetReadDeadline(time.Now().Add(timeout))).To(Succeed())
	n, err := conn.Read(buf)
	if err != nil {
		return nil, false
	}
	return buf[:n], true
}

func buildResetFrame() []byte {
	h := frame.NewHeader(frame.FlagReset, 0)
	b, err := frame.Encode(h)
	Expect(err).NotTo(HaveOccurred())
	return b
}

func buildMuxHelloFrame() []byte {
	h := frame.NewHeader(frame.FlagMuxHello, 0)
	b, err := frame.Encode(h)
	Expect(err).NotTo(HaveOccurred())
	return b
}

func hasFlag(data []byte, flag uint16) bool {
	if len(data) < frame.HeaderLength {
		return false
	}
	if binary.BigEndian.Uint32(data[:4]) != frame.Magic {
		return false
	}
	return binary.BigEndian.Uint16(data[4:6])&flag != 0
}

// startTestProxy starts a proxy pointing at muxAddr with the given endpointAddr.
// Returns the proxy's frontend address and a cancel func.
func startTestProxy(muxAddr *net.UDPAddr, endpointAddr string) (*net.UDPAddr, context.CancelFunc, chan struct{}) {
	tmp, proxyAddr := bindUDP()
	tmp.Close()

	cfg := &config.UdpProxyConfig{
		ListenAddr:      proxyAddr.String(),
		MuxAddr:         muxAddr.String(),
		EndpointAddr:    endpointAddr,
		SessionTimeout:  30 * time.Second,
		CleanupInterval: 10 * time.Second,
	}

	p := proxy.NewProxy(cfg.ListenAddr, cfg.MuxAddr, 2, cfg.SessionTimeout, cfg.CleanupInterval, 0)
	write, read := buildHooks(cfg, logr.Discard())
	p.AddWriteHook(write)
	p.AddReadHook(read)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = p.ListenAndServe(ctx, logr.Discard())
	}()
	time.Sleep(30 * time.Millisecond)
	return proxyAddr, cancel, stopped
}

var _ = Describe("proxy-side RESET handling", func() {
	var (
		fakeMux     *net.UDPConn
		fakeMuxAddr *net.UDPAddr
		client      *net.UDPConn
		proxyAddr   *net.UDPAddr
		cancel      context.CancelFunc
		stopped     chan struct{}
	)

	BeforeEach(func() {
		fakeMux, fakeMuxAddr = bindUDP()
		client, _ = bindUDP()
		proxyAddr, cancel, stopped = startTestProxy(fakeMuxAddr, "127.0.0.1:1194")
	})

	AfterEach(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}
		fakeMux.Close()
		client.Close()
	})

	// readHello drains one PROXY_HELLO from fakeMux and returns the source address.
	readHello := func() *net.UDPAddr {
		buf := make([]byte, 65536)
		Expect(fakeMux.SetReadDeadline(time.Now().Add(time.Second))).To(Succeed())
		n, src, err := fakeMux.ReadFromUDP(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasFlag(buf[:n], frame.FlagProxyHello)).To(BeTrue(), "expected PROXY_HELLO")
		return src
	}

	It("re-handshakes on the first RESET", func() {
		_, err := client.WriteTo([]byte("trigger"), proxyAddr)
		Expect(err).NotTo(HaveOccurred())

		src := readHello()

		// Reply with one RESET — proxy should send another PROXY_HELLO.
		_, err = fakeMux.WriteToUDP(buildResetFrame(), src)
		Expect(err).NotTo(HaveOccurred())

		src2 := readHello()
		Expect(src2.String()).To(Equal(src.String()), "re-handshake should come from same backend addr")
	})

	It("gives up after maxHandshakeResets consecutive RESETs", func() {
		_, err := client.WriteTo([]byte("trigger"), proxyAddr)
		Expect(err).NotTo(HaveOccurred())

		src := readHello()

		// Send maxHandshakeResets RESETs while the proxy is still HANDSHAKING.
		// The proxy counts each RESET but does not restart the handshake mid-flight;
		// it simply expires the session after the limit is reached.
		for range maxHandshakeResets {
			_, err = fakeMux.WriteToUDP(buildResetFrame(), src)
			Expect(err).NotTo(HaveOccurred())
		}

		// After the limit the session is expired — no further PROXY_HELLO should arrive.
		buf := make([]byte, 65536)
		fakeMux.SetReadDeadline(time.Now().Add(600 * time.Millisecond))
		n, _, readErr := fakeMux.ReadFromUDP(buf)
		Expect(readErr).To(HaveOccurred(), "proxy must not re-handshake after give-up; got %d bytes", n)
	})

	It("resets the RESET counter after a successful MUX_HELLO", func() {
		_, err := client.WriteTo([]byte("trigger"), proxyAddr)
		Expect(err).NotTo(HaveOccurred())

		src := readHello()

		// Send one RESET — proxy re-handshakes.
		_, err = fakeMux.WriteToUDP(buildResetFrame(), src)
		Expect(err).NotTo(HaveOccurred())
		src = readHello()

		// Acknowledge with MUX_HELLO — counter resets to 0.
		// The proxy will flush its buffer ("trigger" packet) to the mux after MUX_HELLO,
		// so drain that raw data packet before we start sending more RESETs.
		_, err = fakeMux.WriteToUDP(buildMuxHelloFrame(), src)
		Expect(err).NotTo(HaveOccurred())
		buf := make([]byte, 65536)
		fakeMux.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		fakeMux.ReadFromUDP(buf) // drain flushed buffer packet; ignore error (may or may not arrive)
		time.Sleep(30 * time.Millisecond)

		// Now send maxHandshakeResets-1 more RESETs — proxy should still re-handshake
		// each time (counter was reset on MUX_HELLO, so not at the limit yet).
		for range maxHandshakeResets - 1 {
			_, err = fakeMux.WriteToUDP(buildResetFrame(), src)
			Expect(err).NotTo(HaveOccurred())

			fakeMux.SetReadDeadline(time.Now().Add(time.Second))
			n, newSrc, readErr := fakeMux.ReadFromUDP(buf)
			Expect(readErr).NotTo(HaveOccurred(), "expected PROXY_HELLO after RESET (counter was reset)")
			Expect(hasFlag(buf[:n], frame.FlagProxyHello)).To(BeTrue())
			src = newSrc
		}
	})
})
