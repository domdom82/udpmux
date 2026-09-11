package proxy

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

// ---- helpers ----------------------------------------------------------------

func mustBindUDP(b *testing.B) (*net.UDPConn, *net.UDPAddr) {
	b.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		b.Fatal(err)
	}
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

// startBenchProxy starts a Proxy in the background and returns the frontend
// address and a cancel func.  The proxy is torn down when cancel is called.
func startBenchProxy(b *testing.B, backendAddr string, writeHooks, readHooks []Hook) (*net.UDPAddr, context.CancelFunc) {
	b.Helper()

	// Grab a free port then release it so the proxy can bind it.
	tmp, addr := mustBindUDP(b)
	tmp.Close()

	p := NewProxy(addr.String(), backendAddr, 4)
	for _, h := range writeHooks {
		p.AddWriteHook(h)
	}
	for _, h := range readHooks {
		p.AddReadHook(h)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = p.ListenAndServe(ctx, logr.Discard()) }()
	time.Sleep(20 * time.Millisecond) // wait for bind
	return addr, cancel
}

// ---- getWorkerIndex ---------------------------------------------------------

func BenchmarkGetWorkerIndex_CacheHit(b *testing.B) {
	p := &Proxy{workers: 8, addrToWorker: make(map[string]int)}
	key := (&net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5000}).String()
	_ = p.getWorkerIndex(key) // warm cache
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = p.getWorkerIndex(key)
	}
}

func BenchmarkGetWorkerIndex_CacheMiss(b *testing.B) {
	p := &Proxy{workers: 8, addrToWorker: make(map[string]int)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		key := (&net.UDPAddr{IP: net.IPv4(10, 0, byte(i>>8), byte(i)), Port: 5000 + i%1000}).String()
		_ = p.getWorkerIndex(key)
	}
}

// ---- DialBackend ------------------------------------------------------------

func BenchmarkDialBackend(b *testing.B) {
	server, serverAddr := mustBindUDP(b)
	defer server.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		conn, err := DialBackend(serverAddr)
		if err != nil {
			b.Fatal(err)
		}
		conn.Close()
	}
}

// ---- session refresh (atomic) -----------------------------------------------

func BenchmarkSessionRefresh(b *testing.B) {
	s := &ClientSession{}
	s.lastActive.Store(0)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s.refresh()
	}
}

// ---- end-to-end throughput --------------------------------------------------

// BenchmarkProxyThroughput_NoHooks measures raw packet forwarding rate (packets/s)
// through a running Proxy with no hooks — pure dispatch overhead.
func BenchmarkProxyThroughput_NoHooks(b *testing.B) {
	backendConn, backendAddr := mustBindUDP(b)
	defer backendConn.Close()

	proxyAddr, cancel := startBenchProxy(b, backendAddr.String(), nil, nil)
	defer cancel()

	client, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	payload := make([]byte, 256)
	drain := make(chan struct{})
	go func() {
		buf := make([]byte, 65536)
		for {
			backendConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			_, err := backendConn.Read(buf)
			if err != nil {
				close(drain)
				return
			}
		}
	}()

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := client.Write(payload); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "pkt/s")
	<-drain
}

// BenchmarkProxyThroughput_WithHooks measures the same path but with a
// realistic framing hook (simulates the encode overhead of the mux write hook).
func BenchmarkProxyThroughput_WithHooks(b *testing.B) {
	backendConn, backendAddr := mustBindUDP(b)
	defer backendConn.Close()

	// Simulate the mux wrap hook: prepend a 17-byte V2 header on every reply.
	fakeHeader := make([]byte, 17)
	readHook := Hook(func(_ *ClientSession, data []byte) ([]byte, error) {
		return append(fakeHeader, data...), nil
	})

	proxyAddr, cancel := startBenchProxy(b, backendAddr.String(), nil, []Hook{readHook})
	defer cancel()

	client, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	payload := make([]byte, 256)
	drain := make(chan struct{})
	go func() {
		buf := make([]byte, 65536)
		for {
			backendConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			_, err := backendConn.Read(buf)
			if err != nil {
				close(drain)
				return
			}
		}
	}()

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := client.Write(payload); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "pkt/s")
	<-drain
}

// BenchmarkProxyRoundTrip measures the full round-trip latency
// (client→proxy→backend→proxy→client) for a single packet at a time.
func BenchmarkProxyRoundTrip(b *testing.B) {
	backendConn, backendAddr := mustBindUDP(b)
	defer backendConn.Close()

	// Echo server: reflect every packet back to its sender.
	go func() {
		buf := make([]byte, 65536)
		for {
			backendConn.SetReadDeadline(time.Now().Add(time.Second))
			n, addr, err := backendConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = backendConn.WriteToUDP(buf[:n], addr)
		}
	}()

	proxyAddr, cancel := startBenchProxy(b, backendAddr.String(), nil, nil)
	defer cancel()

	clientConn, _ := mustBindUDP(b)
	defer clientConn.Close()

	payload := make([]byte, 64)
	recvBuf := make([]byte, 65536)

	// Warm up the session so the first timed iteration doesn't pay session-creation cost.
	_, _ = clientConn.WriteTo(payload, proxyAddr)
	clientConn.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = clientConn.Read(recvBuf)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = clientConn.WriteTo(payload, proxyAddr)
		clientConn.SetReadDeadline(time.Now().Add(time.Second))
		_, err := clientConn.Read(recvBuf)
		if err != nil {
			b.Fatal(err)
		}
	}
}
