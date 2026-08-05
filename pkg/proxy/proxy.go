package proxy

import (
	"context"
	"fmt"
	"hash/fnv"
	"net"
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/net/ipv4"
)

const (
	packetBatchSize    = 32           // this many packets can be read at once from socket buffer
	packetBufferSize   = 2 << 15      // 64k max udp length
	socketBufferSize   = 16 * 2 << 19 // 16M max default socket buffers. limited by net.core.wmem / rmem
	workerChanCapacity = 1000         // this many packets can be stored in a worker packet channel
)

type Packet struct {
	Addr net.Addr
	Data []byte
}

type Proxy struct {
	localAddr   string
	backendAddr string
	workers     int
}

func NewProxy(localAddr string, backendAddr string, workers int) *Proxy {
	p := &Proxy{
		localAddr:   localAddr,
		backendAddr: backendAddr,
		workers:     workers,
	}

	return p
}

func (p *Proxy) ListenAndServe(ctx context.Context, log logr.Logger) error {
	// Resolve frontend endpoint address
	listenAddr, err := net.ResolveUDPAddr("udp", p.localAddr)
	if err != nil {
		return fmt.Errorf("invalid local endpoint '%s' (%w)", p.localAddr, err)
	}

	// Bind frontend UDP socket
	frontendConn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP address '%s' (%w)", p.localAddr, err)
	}
	defer frontendConn.Close()

	// Resolve backend endpoint address
	backendAddr, err := net.ResolveUDPAddr("udp", p.backendAddr)
	if err != nil {
		return fmt.Errorf("invalid backend endpoint '%s' (%w)", p.backendAddr, err)
	}

	// Maximize frontend receive/write buffers (limited by sysctl net.core.wmem_max / net.core.rmem_max)
	if err = frontendConn.SetReadBuffer(socketBufferSize); err != nil {
		log.Error(err, "failed to set socket read buffer size")
	}
	if err = frontendConn.SetWriteBuffer(socketBufferSize); err != nil {
		log.Error(err, "failed to set socket write buffer size")
	}

	// Create one channel per worker. Packets from the same client always go to the same worker (address-hashed)
	workerChans := make([]chan Packet, p.workers)
	for i := range p.workers {
		workerChans[i] = make(chan Packet, workerChanCapacity)
	}
	sessionMgr := newSessionManager(log, backendAddr, frontendConn, p.writeHooks, p.readHooks)

	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go worker(log, workerChans[i], sessionMgr, &wg)
	}

	log.Info("UDP Proxy running", "frontend", p.localAddr, "backend", p.backendAddr, "workers", p.workers)

	// Handle OS signals for graceful shutdown
	go func() {
		<-ctx.Done()
		log.Info("Shutting down proxy...")
		frontendConn.Close()
	}()

	// Main multi-packet read loop (recvmmsg via ReadBatch)
	packetConn := ipv4.NewPacketConn(frontendConn)
	defer packetConn.Close()
	ms := make([]ipv4.Message, packetBatchSize)
	for i := range packetBatchSize {
		buf := make([]byte, packetBufferSize)
		ms[i] = ipv4.Message{Buffers: [][]byte{buf}}
	}

	for {
		// Read a batch of packets and push them into the packet channel
		n, err := packetConn.ReadBatch(ms, 0)
		if err != nil {
			break // Listener closed, context canceled etc.
		}

		for i := range n {
			msgLen := ms[i].N
			if msgLen == 0 {
				continue
			}

			// Copy frame payload to decouple from receiver batch buffers
			data := make([]byte, msgLen)
			copy(data, ms[i].Buffers[0][:msgLen])

			// Hash client address to a fixed worker to preserve per-client packet order.
			idx := getWorkerIndex(ms[i].Addr.String(), p.workers)
			workerChans[idx] <- Packet{
				Addr: ms[i].Addr,
				Data: data,
			}
		}
	}

	for _, ch := range workerChans {
		close(ch)
	}
	wg.Wait()
	log.Info("Proxy stopped.")
	return nil
}

// Worker dispatch logic: Routes packet from client channel to session forwarder
func worker(log logr.Logger, ch <-chan Packet, sm *SessionManager, wg *sync.WaitGroup) {
	defer wg.Done()

	for pkt := range ch {
		session := sm.getOrCreate(pkt.Addr)
		if session != nil {
			select {
			case session.sendChan <- pkt.Data:
			default:
				// Buffer full fallback to prevent blocking worker pool
				log.Info("Session queue full for %s, dropping packet", pkt.Addr)
			}
		}
	}
}

// Map client address to a worker
func getWorkerIndex(addr string, numWorkers int) int {
	h := fnv.New32a()
	h.Write([]byte(addr))
	return int(h.Sum32()) % numWorkers
}
