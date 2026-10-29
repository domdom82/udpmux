package app

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
)

const (
	handshakeRetries       = 5
	handshakeRetryInterval = 500 * time.Millisecond
	bufferSize             = 100
	proxySessionTag        = "proxy.session"
	maxHandshakeResets     = 3 // give up after this many consecutive RESETs from the mux
)

type sessionState int

const (
	stateIdle        sessionState = iota
	stateHandshaking sessionState = iota
	stateEstablished sessionState = iota
)

// proxySession holds per-session handshake state on the proxy side.
type proxySession struct {
	mu           sync.Mutex
	state        sessionState
	buffer       [][]byte
	endpointAddr string
	resetCount   int // consecutive RESETs received from the mux
}

func newProxySession(endpointAddr string) *proxySession {
	return &proxySession{
		state:        stateIdle,
		endpointAddr: endpointAddr,
	}
}

func (ps *proxySession) setState(s sessionState) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.state = s
}

func (ps *proxySession) getState() sessionState {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.state
}

func (ps *proxySession) incResetCount() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.resetCount++
	return ps.resetCount
}

func (ps *proxySession) clearResetCount() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.resetCount = 0
}

func (ps *proxySession) bufferPacket(data []byte) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	pkt := make([]byte, len(data))
	copy(pkt, data)
	if len(ps.buffer) >= bufferSize {
		ps.buffer = ps.buffer[1:] // drop oldest
	}
	ps.buffer = append(ps.buffer, pkt)
}

func (ps *proxySession) drainBuffer() [][]byte {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	pkts := ps.buffer
	ps.buffer = nil
	return pkts
}

// sendProxyHello writes a PROXY_HELLO control frame to the mux via the session's backend conn.
func (ps *proxySession) sendProxyHello(s *proxy.ClientSession) error {
	ep := []byte(ps.endpointAddr)
	if len(ep) > 255 {
		return fmt.Errorf("endpoint address too long: %s", ps.endpointAddr)
	}
	payload := append([]byte{byte(len(ep))}, ep...)
	h := frame.NewHeader(frame.FlagProxyHello, len(payload))
	headerBytes, err := frame.Encode(h)
	if err != nil {
		return err
	}
	conn := s.GetBackendConn()
	if conn == nil {
		return fmt.Errorf("no backend connection available for PROXY_HELLO")
	}
	_, err = conn.Write(append(headerBytes, payload...))
	return err
}

// startHandshake transitions the session to HANDSHAKING and fires PROXY_HELLO retries.
// It is a no-op if the session is already handshaking.
func (ps *proxySession) startHandshake(s *proxy.ClientSession, log logr.Logger) {
	if ps.getState() == stateHandshaking {
		return
	}
	ps.setState(stateHandshaking)
	go func() {
		for i := range handshakeRetries {
			if ps.getState() == stateEstablished || s.IsExpired() {
				return
			}
			if err := ps.sendProxyHello(s); err != nil {
				log.Error(err, "Failed to send PROXY_HELLO", "attempt", i+1)
			}
			time.Sleep(handshakeRetryInterval)
		}
		if ps.getState() != stateEstablished {
			log.Info("Handshake timed out, resetting session")
			ps.setState(stateIdle)
			ps.drainBuffer()
		}
	}()
}

// isControlFrame returns true if data begins with the UDPM magic bytes.
// Used as a fast path to avoid calling Decode (which allocates) on raw data packets.
func isControlFrame(data []byte) bool {
	return len(data) >= frame.HeaderLength &&
		binary.BigEndian.Uint32(data[:4]) == frame.Magic
}

// buildHooks returns the write and read hooks for the proxy side.
func buildHooks(cfg *config.UdpProxyConfig, log logr.Logger) (proxy.Hook, proxy.Hook) {
	// getOrCreate retrieves the proxySession stored in the ClientSession.
	getOrCreate := func(s *proxy.ClientSession) *proxySession {
		if v := s.GetTag(proxySessionTag); v != nil {
			return v.(*proxySession)
		}
		ps := newProxySession(cfg.EndpointAddr)
		s.SetTag(proxySessionTag, ps)
		return ps
	}

	// write: client→mux direction.
	// IDLE/HANDSHAKING: buffer the packet, suppress forwarding (return nil).
	// ESTABLISHED: forward raw.
	write := proxy.Hook(func(s *proxy.ClientSession, data []byte) ([]byte, error) {
		ps := getOrCreate(s)

		switch ps.getState() {
		case stateIdle:
			ps.bufferPacket(data)
			ps.startHandshake(s, log)
			return nil, nil // suppress: no backend yet
		case stateHandshaking:
			ps.bufferPacket(data)
			return nil, nil // suppress: handshake in progress
		case stateEstablished:
			return data, nil
		}
		return data, nil
	})

	// read: mux→client direction.
	// Fast path: if data doesn't start with UDPM magic, it's raw backend data — pass through.
	// Slow path: control frame — handle MUX_HELLO, RESET.
	read := proxy.Hook(func(s *proxy.ClientSession, data []byte) ([]byte, error) {
		if !isControlFrame(data) {
			return data, nil
		}
		h, err := frame.Decode(data[:frame.HeaderLength])
		if err != nil {
			// Not a valid control frame — treat as raw data.
			return data, nil
		}
		return handleControlFromMux(s, h, log, getOrCreate(s))
	})

	return write, read
}

func handleControlFromMux(s *proxy.ClientSession, h *frame.Header, log logr.Logger, ps *proxySession) ([]byte, error) {
	switch {
	case h.Flags&frame.FlagMuxHello != 0:
		ps.setState(stateEstablished)
		ps.clearResetCount()
		log.Info("Session established", "session", s)

		buffered := ps.drainBuffer()
		if len(buffered) > 0 {
			backendConn := s.GetBackendConn()
			if backendConn != nil {
				for _, pkt := range buffered {
					if _, err := backendConn.Write(pkt); err != nil {
						log.Error(err, "Failed to flush buffered packet after handshake")
					}
				}
			} else {
				log.Info("MUX_HELLO received but no backend conn yet, buffered packets dropped", "count", len(buffered))
			}
		}
		return nil, nil // consume control frame

	case h.Flags&frame.FlagReset != 0:
		count := ps.incResetCount()
		if ps.getState() == stateHandshaking {
			if count >= maxHandshakeResets {
				log.Info("Mux rejected session repeatedly, giving up", "session", s, "resets", count)
				ps.drainBuffer()
				s.Expire()
			}
			// received RESET while handshaking — ignore, keep retrying handshake
			return nil, nil
		}

		log.Info("RESET received from mux, re-handshaking", "session", s, "resets", count)
		ps.setState(stateIdle)
		ps.startHandshake(s, log)
		return nil, nil // consume control frame
	}
	return nil, fmt.Errorf("unknown control flags from mux: 0x%04x", h.Flags)
}
