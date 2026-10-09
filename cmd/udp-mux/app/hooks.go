package app

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
)

// buildMuxHooks returns the write hook for the mux side.
// The read hook is nil — return traffic from the backend is forwarded raw without any wrapping.
func buildMuxHooks(cfg *config.UdpMuxConfig, log logr.Logger) proxy.Hook {
	// unwrap: proxy→backend direction.
	//
	// Known session (backend conn exists): forward raw data.
	//   KEEPALIVE: refresh idle timer, suppress.
	//   PING: echo back, suppress.
	// Unknown session: expect PROXY_HELLO.
	//   PING: echo back, suppress (no session needed).
	//   other: send RESET, suppress.
	unwrap := proxy.Hook(func(s *proxy.ClientSession, data []byte) ([]byte, error) {
		if s.GetBackendConn() != nil {
			if isControlFrame(data, frame.FlagKeepAlive) {
				return nil, nil // consume keepalive
			}
			if isControlFrame(data, frame.FlagPing) {
				if err := sendRaw(s, data); err != nil {
					log.Error(err, "failed to echo PING")
				}
				return nil, nil // consume ping
			}
			// Raw data: forward to backend.
			return data, nil
		}

		// Unknown session.
		if isControlFrame(data, frame.FlagPing) {
			if err := sendRaw(s, data); err != nil {
				log.Error(err, "failed to echo PING on unknown session")
			}
			return nil, nil
		}

		if !isControlFrame(data, frame.FlagProxyHello) {
			if err := sendControl(s, frame.FlagReset); err != nil {
				log.Error(err, "failed to send RESET")
			}
			log.Info("unknown session, non-HELLO packet received, sent RESET", "session", s)
			return nil, fmt.Errorf("unexpected packet on unknown session, RESET sent")
		}

		// Parse PROXY_HELLO.
		endpointStr, err := parseHelloEndpoint(data)
		if err != nil {
			if sendErr := sendControl(s, frame.FlagReset); sendErr != nil {
				log.Error(sendErr, "failed to send RESET after bad HELLO")
			}
			return nil, fmt.Errorf("malformed PROXY_HELLO: %w", err)
		}

		// Dial backend.
		backendAddr, err := net.ResolveUDPAddr("udp", endpointStr)
		if err != nil {
			if sendErr := sendControl(s, frame.FlagReset); sendErr != nil {
				log.Error(sendErr, "failed to send RESET after bad endpoint")
			}
			return nil, fmt.Errorf("invalid backend endpoint '%s': %w", endpointStr, err)
		}
		backendConn, err := proxy.DialBackend(backendAddr)
		if err != nil {
			if sendErr := sendControl(s, frame.FlagReset); sendErr != nil {
				log.Error(sendErr, "failed to send RESET after dial failure")
			}
			return nil, fmt.Errorf("failed to dial backend '%s': %w", endpointStr, err)
		}

		s.SetBackendConn(backendConn)

		if err := sendControl(s, frame.FlagMuxHello); err != nil {
			log.Error(err, "failed to send MUX_HELLO")
		}
		log.Info("session established", "session", s, "endpoint", endpointStr)

		idle := cfg.KeepaliveIdle
		if idle <= 0 {
			idle = 25 * time.Second
		}
		interval := cfg.KeepaliveInterval
		if interval <= 0 {
			interval = 5 * time.Second
		}
		s.StartKeepalive(idle, interval, func() error { return sendControl(s, frame.FlagKeepAlive) })

		return nil, nil // PROXY_HELLO was a control frame, not data
	})

	return unwrap
}

// isControlFrame returns true if data is a valid control frame with the given flag set.
func isControlFrame(data []byte, flag uint16) bool {
	if len(data) < frame.HeaderLength {
		return false
	}
	if binary.BigEndian.Uint32(data[:4]) != frame.Magic {
		return false
	}
	flags := binary.BigEndian.Uint16(data[4:6])
	return flags&flag != 0
}

// sendControl sends a control frame (no payload) back to the proxy.
func sendControl(s *proxy.ClientSession, flags uint16) error {
	h := frame.NewHeader(flags, 0)
	b, err := frame.Encode(h)
	if err != nil {
		return err
	}
	_, err = s.GetFrontendConn().WriteTo(b, s.GetClientAddr())
	return err
}

// sendRaw echoes the raw frame bytes back to the proxy (used for PING echo).
func sendRaw(s *proxy.ClientSession, data []byte) error {
	_, err := s.GetFrontendConn().WriteTo(data, s.GetClientAddr())
	return err
}

// parseHelloEndpoint extracts the endpoint string from a PROXY_HELLO frame.
func parseHelloEndpoint(data []byte) (string, error) {
	if len(data) < frame.HeaderLength+1 {
		return "", fmt.Errorf("frame too short")
	}
	epLen := int(data[frame.HeaderLength])
	payloadStart := frame.HeaderLength + 1
	if len(data) < payloadStart+epLen {
		return "", fmt.Errorf("endpoint truncated: need %d bytes, have %d", payloadStart+epLen, len(data))
	}
	return string(data[payloadStart : payloadStart+epLen]), nil
}
