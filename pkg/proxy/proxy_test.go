package proxy_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/domdom82/udpmux/pkg/proxy"
)

func TestProxy(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proxy Suite")
}

// bindUDP binds a random local UDP port and returns the conn and its address.
func bindUDP() (*net.UDPConn, *net.UDPAddr) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	Expect(err).NotTo(HaveOccurred())
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

// dialAndSend sends a datagram to dst from a new ephemeral socket.
func dialAndSend(dst *net.UDPAddr, data []byte) *net.UDPConn {
	conn, err := net.DialUDP("udp", nil, dst)
	Expect(err).NotTo(HaveOccurred())
	_, err = conn.Write(data)
	Expect(err).NotTo(HaveOccurred())
	return conn
}

// readOne reads exactly one datagram from conn within the given timeout.
func readOne(conn *net.UDPConn, timeout time.Duration) []byte {
	buf := make([]byte, 65536)
	Expect(conn.SetReadDeadline(time.Now().Add(timeout))).To(Succeed())
	n, err := conn.Read(buf)
	Expect(err).NotTo(HaveOccurred())
	return buf[:n]
}

var _ = Describe("Proxy (end-to-end)", func() {
	var (
		backendConn *net.UDPConn
		backendAddr *net.UDPAddr
		proxyAddr   *net.UDPAddr
		ctx         context.Context
		cancel      context.CancelFunc
		stopped     chan struct{}
	)

	BeforeEach(func() {
		// Bind backend
		backendConn, backendAddr = bindUDP()

		// Find a free port for the proxy frontend; proxy will bind it when started
		tmp, addr := bindUDP()
		tmp.Close()
		proxyAddr = addr

		ctx, cancel = context.WithCancel(context.Background())
		stopped = make(chan struct{})
	})

	AfterEach(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}
		backendConn.Close()
	})

	startProxy := func(writeHooks, readHooks []proxy.Hook) {
		p := proxy.NewProxy(proxyAddr.String(), backendAddr.String(), 2)
		for _, h := range writeHooks {
			p.AddWriteHook(h)
		}
		for _, h := range readHooks {
			p.AddReadHook(h)
		}
		go func() {
			defer close(stopped)
			_ = p.ListenAndServe(ctx, logr.Discard())
		}()
		// Allow the proxy goroutine to bind the socket.
		time.Sleep(30 * time.Millisecond)
	}

	Describe("no hooks (pass-through)", func() {
		It("forwards a client packet to the backend", func() {
			startProxy(nil, nil)

			client := dialAndSend(proxyAddr, []byte("hello backend"))
			defer client.Close()

			got := readOne(backendConn, time.Second)
			Expect(got).To(Equal([]byte("hello backend")))
		})

		It("forwards the backend reply back to the client", func() {
			startProxy(nil, nil)

			// A single bound socket acts as both sender and receiver.
			clientConn, _ := bindUDP()
			defer clientConn.Close()

			// Send the trigger packet to the proxy; the proxy learns our address from this datagram.
			_, err := clientConn.WriteTo([]byte("trigger"), proxyAddr)
			Expect(err).NotTo(HaveOccurred())

			// Consume the forwarded packet on the backend side to get the proxied source address.
			buf := make([]byte, 65536)
			Expect(backendConn.SetReadDeadline(time.Now().Add(time.Second))).To(Succeed())
			_, proxiedAddr, err := backendConn.ReadFromUDP(buf)
			Expect(err).NotTo(HaveOccurred())

			// Reply from backend; the proxy should relay it to the original client.
			_, err = backendConn.WriteToUDP([]byte("pong"), proxiedAddr)
			Expect(err).NotTo(HaveOccurred())

			reply := readOne(clientConn, time.Second)
			Expect(reply).To(Equal([]byte("pong")))
		})
	})

	Describe("write hook", func() {
		It("transforms outbound packet data", func() {
			doubleHook := proxy.Hook(func(_ *proxy.ClientSession, data []byte) ([]byte, error) {
				return append(data, data...), nil
			})
			startProxy([]proxy.Hook{doubleHook}, nil)

			client := dialAndSend(proxyAddr, []byte("AB"))
			defer client.Close()

			got := readOne(backendConn, time.Second)
			Expect(got).To(Equal([]byte("ABAB")))
		})
	})

	Describe("read hook", func() {
		It("transforms reply packet data", func() {
			startProxy(nil, []proxy.Hook{
				func(_ *proxy.ClientSession, data []byte) ([]byte, error) {
					return append([]byte(">>"), data...), nil
				},
			})

			clientConn, _ := bindUDP()
			defer clientConn.Close()

			_, err := clientConn.WriteTo([]byte("hi"), proxyAddr)
			Expect(err).NotTo(HaveOccurred())

			buf := make([]byte, 65536)
			Expect(backendConn.SetReadDeadline(time.Now().Add(time.Second))).To(Succeed())
			_, proxiedAddr, err := backendConn.ReadFromUDP(buf)
			Expect(err).NotTo(HaveOccurred())

			_, err = backendConn.WriteToUDP([]byte("world"), proxiedAddr)
			Expect(err).NotTo(HaveOccurred())

			reply := readOne(clientConn, time.Second)
			Expect(reply).To(Equal([]byte(">>world")))
		})
	})
})

var _ = Describe("Hook", func() {
	It("is invokable as a plain function", func() {
		invoked := false
		h := proxy.Hook(func(_ *proxy.ClientSession, data []byte) ([]byte, error) {
			invoked = true
			return data, nil
		})
		out, err := h(nil, []byte("test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(invoked).To(BeTrue())
		Expect(out).To(Equal([]byte("test")))
	})
})

var _ = Describe("DialBackend", func() {
	It("dials a reachable UDP address and returns a connection", func() {
		server, serverAddr := bindUDP()
		defer server.Close()

		conn, err := proxy.DialBackend(serverAddr)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn).NotTo(BeNil())
		defer conn.Close()
	})
})
