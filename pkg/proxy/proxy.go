package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/net/ipv4"
)

const (
	packetBatchSize    = 32           // this many packets can be read at once from socket buffer
	packetBufferSize   = 2 << 15      // 64k max udp length
	socketBufferSize   = 16 * 2 << 19 // 16M max default socket buffers. limited by net.core.wmem / rmem
	socketChanCapacity = 20000        // this many packets can be stored in the global packet channel
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

	// Maximize frontend receive/write buffers
	if err = frontendConn.SetReadBuffer(socketBufferSize); err != nil {
		log.Error(err, "failed to set socket read buffer size")
	}
	if err = frontendConn.SetWriteBuffer(socketBufferSize); err != nil {
		log.Error(err, "failed to set socket write buffer size")
	}

	// Create packet channel and session manager
	packetChan := make(chan Packet, socketChanCapacity)
	sessionMgr := newSessionManager(log, backendAddr, frontendConn)

	// Launch worker pool to process packets from packet channel
	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go worker(log, packetChan, sessionMgr, &wg)
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

			packetChan <- Packet{
				Addr: ms[i].Addr,
				Data: data,
			}
		}
	}

	close(packetChan)
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
