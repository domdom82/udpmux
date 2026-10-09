package app

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
)

const (
	handshakeRetries       = 5
	handshakeRetryInterval = 500 * time.Millisecond
	bufferSize             = 100
	proxySessionTag        = "proxy.session"
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

func sendKeepalive(s *proxy.ClientSession) error {
	h := frame.NewHeader(frame.FlagKeepAlive, 0)
	b, err := frame.Encode(h)
	if err != nil {
		return err
	}
	conn := s.GetBackendConn()
	if conn == nil {
		return fmt.Errorf("no backend connection for KEEPALIVE")
	}
	_, err = conn.Write(b)
	return err
}

// startHandshake transitions the session to HANDSHAKING and fires PROXY_HELLO retries.
func (ps *proxySession) startHandshake(s *proxy.ClientSession, log logr.Logger) {
	ps.setState(stateHandshaking)
	go func() {
		for i := range handshakeRetries {
			if ps.getState() == stateEstablished {
				return
			}
			if err := ps.sendProxyHello(s); err != nil {
				log.Error(err, "failed to send PROXY_HELLO", "attempt", i+1)
			}
			time.Sleep(handshakeRetryInterval)
		}
		if ps.getState() != stateEstablished {
			log.Info("handshake timed out, resetting session")
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
			s.StartKeepalive(cfg.KeepaliveIdle, cfg.KeepaliveInterval, func() error { return sendKeepalive(s) })
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
	// Slow path: control frame — handle MUX_HELLO, KEEPALIVE, RESET.
	read := proxy.Hook(func(s *proxy.ClientSession, data []byte) ([]byte, error) {
		if !isControlFrame(data) {
			return data, nil
		}
		h, err := frame.Decode(data[:frame.HeaderLength])
		if err != nil {
			// Not a valid control frame — treat as raw data.
			return data, nil
		}
		return handleControlFromMux(s, h, cfg, log, getOrCreate)
	})

	return write, read
}

func handleControlFromMux(s *proxy.ClientSession, h *frame.Header, cfg *config.UdpProxyConfig, log logr.Logger, getOrCreate func(*proxy.ClientSession) *proxySession) ([]byte, error) {
	switch {
	case h.Flags&frame.FlagMuxHello != 0:
		ps := getOrCreate(s)
		ps.setState(stateEstablished)
		log.Info("session established", "session", s)

		buffered := ps.drainBuffer()
		if len(buffered) > 0 {
			backendConn := s.GetBackendConn()
			if backendConn != nil {
				for _, pkt := range buffered {
					if _, err := backendConn.Write(pkt); err != nil {
						log.Error(err, "failed to flush buffered packet after handshake")
					}
				}
			} else {
				log.Info("MUX_HELLO received but no backend conn yet, buffered packets dropped", "count", len(buffered))
			}
		}
		return nil, nil // consume control frame

	case h.Flags&frame.FlagKeepAlive != 0:
		return nil, nil

	case h.Flags&frame.FlagReset != 0:
		log.Info("RESET received from mux, re-handshaking", "session", s)
		ps := getOrCreate(s)
		s.StopKeepalive()
		ps.setState(stateIdle)
		ps.startHandshake(s, log)
		s.StartKeepalive(cfg.KeepaliveIdle, cfg.KeepaliveInterval, func() error { return sendKeepalive(s) })
		return nil, nil // consume control frame
	}
	return nil, fmt.Errorf("unknown control flags from mux: 0x%04x", h.Flags)
}
