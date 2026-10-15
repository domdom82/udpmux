package app

import (
	"encoding/binary"
	"fmt"
	"net"
	"regexp"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
)

const (
	muxResetCountTag    = "mux.resetCount"
	maxResetsPerSession = 3 // sessions are expired after this many RESETs
)

// buildMuxHooks returns the write hook for the mux side.
// The read hook is nil — return traffic from the backend is forwarded raw without any wrapping.
func buildMuxHooks(log logr.Logger, cfg *config.UdpMuxConfig) proxy.Hook {
	// write: proxy→backend direction.
	//
	// Known session (backend conn exists): forward raw data.
	//   PING: echo back, suppress.
	// Unknown session: expect PROXY_HELLO.
	//   PING: echo back, suppress (no session needed).
	//   other: send RESET; expire session after maxResetsPerSession.
	write := proxy.Hook(func(s *proxy.ClientSession, data []byte) ([]byte, error) {
		if s.GetBackendConn() != nil {
			if isControlFrame(data, frame.FlagPing) {
				log.Info("PING received from proxy", "session", s, "len", len(data))
				if err := sendRaw(s, data); err != nil {
					log.Error(err, "Failed to echo PING")
				}
				return nil, nil // consume ping
			}
			// Raw data: forward to backend.
			return data, nil
		}

		// Unknown session.
		if isControlFrame(data, frame.FlagPing) {
			log.Info("PING received from proxy", "session", s, "len", len(data))
			if err := sendRaw(s, data); err != nil {
				log.Error(err, "Failed to echo PING on unknown session")
			}
			return nil, nil
		}

		if !isControlFrame(data, frame.FlagProxyHello) {
			sendReset(log, s)
			return nil, fmt.Errorf("unexpected packet on unknown session, RESET sent")
		}

		// Parse PROXY_HELLO.
		endpointStr, err := parseHelloEndpoint(data)
		if err != nil {
			sendReset(log, s)
			return nil, fmt.Errorf("malformed PROXY_HELLO: %w", err)
		}

		// Check endpoint against allow-list.
		if patterns := cfg.CompiledAllowList(); len(patterns) > 0 {
			if !matchesAny(endpointStr, patterns) {
				sendReset(log, s)
				return nil, fmt.Errorf("endpoint '%s' not in allow list", endpointStr)
			}
		}

		// Dial backend.
		backendAddr, err := net.ResolveUDPAddr("udp", endpointStr)
		if err != nil {
			sendReset(log, s)
			return nil, fmt.Errorf("invalid backend endpoint '%s': %w", endpointStr, err)
		}
		backendConn, err := proxy.DialBackend(backendAddr)
		if err != nil {
			sendReset(log, s)
			return nil, fmt.Errorf("failed to dial backend '%s': %w", endpointStr, err)
		}

		s.SetBackendConn(backendConn)

		if err := sendControl(s, frame.FlagMuxHello); err != nil {
			log.Error(err, "Failed to send MUX_HELLO")
		}
		log.Info("Session established", "session", s, "endpoint", endpointStr)

		return nil, nil // consume PROXY_HELLO
	})

	return write
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

// sendReset sends a RESET control frame to the proxy, increments the per-session
// reset counter, and expires the session once maxResetsPerSession is reached.
func sendReset(log logr.Logger, s *proxy.ClientSession) {
	if err := sendControl(s, frame.FlagReset); err != nil {
		log.Error(err, "Failed to send RESET")
	}
	v := s.GetTag(muxResetCountTag)
	count := 0
	if v != nil {
		count = v.(int)
	}
	count++
	s.SetTag(muxResetCountTag, count)
	log.Info("RESET sent", "session", s, "resetCount", count)
	if count >= maxResetsPerSession {
		log.Info("Session expired after repeated RESETs", "session", s)
		s.Expire()
	}
}

// matchesAny reports whether s matches any of the given compiled patterns.
func matchesAny(s string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
