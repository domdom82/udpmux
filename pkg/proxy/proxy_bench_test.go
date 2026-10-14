package proxy

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

// ---- thresholds ----------------------------------------------------------------

const (
	// minThroughput is the minimum acceptable throughput for the proxy in packets/s.
	minThroughput = 70000
	payloadSize   = 1460 // typical MSS for 1500 MTU
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

	p := NewProxy(addr.String(), backendAddr, 4, 0, 0, 0)
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

// drainUDP reads from conn until it is closed, discarding all data.
// The caller must close conn to unblock the goroutine.
func drainUDP(conn *net.UDPConn) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	return done
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

// ---- getOrCreate fast path (hot path allocs) --------------------------------

// BenchmarkGetOrCreate_FastPath measures the common case: a packet arrives from
// a known client and we return its existing session under a read lock.
// This should be allocation-free.
func BenchmarkGetOrCreate_FastPath(b *testing.B) {
	backend, backendAddr := mustBindUDP(b)
	defer backend.Close()
	frontend, _ := mustBindUDP(b)
	defer frontend.Close()

	sm := newSessionManager(logr.Discard(), backendAddr, frontend, nil, nil, 30*time.Second, 10*time.Second, 0)

	// Pre-create a session so the fast path is always taken.
	clientAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9001}
	key := clientAddr.String()
	_ = sm.getOrCreate(key, clientAddr)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = sm.getOrCreate(key, clientAddr)
	}
}

// ---- end-to-end throughput (outbound only) ----------------------------------

// BenchmarkProxyThroughput_NoHooks measures raw packet forwarding rate (packets/s)
// through a running Proxy with no hooks — pure dispatch overhead.
func BenchmarkProxyThroughput_NoHooks(b *testing.B) {
	backendConn, backendAddr := mustBindUDP(b)

	proxyAddr, cancel := startBenchProxy(b, backendAddr.String(), nil, nil)
	defer cancel()

	client, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	payload := make([]byte, payloadSize)
	drain := drainUDP(backendConn)

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := client.Write(payload); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	pkts := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(pkts, "pkt/s")
	backendConn.Close()
	<-drain
	if pkts < minThroughput {
		b.Errorf("low throughput, only %.0f pkt/s, required at least %d pkt/s", pkts, minThroughput)
	}
}

// BenchmarkProxyThroughput_WithHooks measures the forwarding rate with a
// realistic hook that prepends the 8-byte UDPM control frame header — the
// actual per-packet overhead for framed data in this protocol.
func BenchmarkProxyThroughput_WithHooks(b *testing.B) {
	backendConn, backendAddr := mustBindUDP(b)

	// Simulate the real protocol overhead: prepend the 8-byte UDPM frame header.
	fakeHeader := make([]byte, 8)
	readHook := Hook(func(_ *ClientSession, data []byte) ([]byte, error) {
		out := make([]byte, 0, len(fakeHeader)+len(data))
		return append(append(out, fakeHeader...), data...), nil
	})

	proxyAddr, cancel := startBenchProxy(b, backendAddr.String(), nil, []Hook{readHook})
	defer cancel()

	client, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	payload := make([]byte, payloadSize)
	drain := drainUDP(backendConn)

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := client.Write(payload); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	pkts := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(pkts, "pkt/s")
	backendConn.Close()
	<-drain
	if pkts < minThroughput {
		b.Errorf("low throughput, only %.0f pkt/s, required at least %d pkt/s", pkts, minThroughput)
	}
}

// BenchmarkProxyRoundTrip measures the full round-trip latency and throughput
// (client→proxy→backend→proxy→client) one packet at a time.
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

	payload := make([]byte, payloadSize)
	recvBuf := make([]byte, 65536)

	// Warm up: establish the session before the timed loop.
	_, _ = clientConn.WriteTo(payload, proxyAddr)
	clientConn.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = clientConn.Read(recvBuf)

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = clientConn.WriteTo(payload, proxyAddr)
		clientConn.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := clientConn.Read(recvBuf); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	pkts := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(pkts, "roundtrips/s")
}
