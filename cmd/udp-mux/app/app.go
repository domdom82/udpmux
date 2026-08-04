package app

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/utils"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"k8s.io/component-base/version/verflag"
)

const Name = "udp-mux"

var (
	buffersize     int
	listen         string
	readTimeout    time.Duration
	sessionTimeout time.Duration
)

// NewCommand creates a new cobra.Command for running udp-mux.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   Name,
		Short: "Launch the " + Name,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			log, err := utils.InitRun(cmd, Name)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg := &config.UdpMuxConfig{
				Listen:         listen,
				BufferSize:     buffersize,
				ReadTimeout:    readTimeout,
				SessionTimeout: sessionTimeout,
			}
			return run(ctx, log, cfg)
		},
	}

	flags := cmd.Flags()
	verflag.AddFlags(flags)
	flags.StringVarP(&listen, "listen", "l", ":8080", "Listen address in the format <host>:<port>")
	flags.IntVarP(&buffersize, "buffersize", "b", 2048, "Buffer size in bytes")
	flags.DurationVarP(&readTimeout, "readtimeout", "r", 5*time.Second, "Read timeout (kept for compatibility)")
	flags.DurationVarP(&sessionTimeout, "sessiontimeout", "s", 2*time.Minute, "Idle session eviction timeout")
	return cmd
}

// muxSession is a long-lived connection to a single endpoint on behalf of one proxy address.
type muxSession struct {
	conn      net.Conn
	idleTimer *time.Timer
	proxyAddr *net.UDPAddr
}

// sessionMap is a concurrency-safe map from proxyAddr string to muxSession.
type sessionMap struct {
	mu       sync.Mutex
	sessions map[string]*muxSession
}

func newSessionMap() *sessionMap {
	return &sessionMap{sessions: make(map[string]*muxSession)}
}

// getOrCreate returns the existing session for key, or calls createFn to make one.
// createFn is called under the lock.
func (sm *sessionMap) getOrCreate(key string, createFn func() (*muxSession, error)) (*muxSession, bool, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[key]; ok {
		return s, false, nil
	}
	s, err := createFn()
	if err != nil {
		return nil, false, err
	}
	sm.sessions[key] = s
	return s, true, nil
}

func (sm *sessionMap) delete(key string) {
	sm.mu.Lock()
	delete(sm.sessions, key)
	sm.mu.Unlock()
}

// resetTimer resets the idle timer for an existing session. No-op if session is gone.
func (sm *sessionMap) resetTimer(key string, d time.Duration) {
	sm.mu.Lock()
	if s, ok := sm.sessions[key]; ok {
		s.idleTimer.Reset(d)
	}
	sm.mu.Unlock()
}

// mux holds all shared state for the udp-mux server.
type mux struct {
	udpProxyConn *net.UDPConn
	writeMu      sync.Mutex // guards concurrent WriteToUDP from per-session readerGoroutines
	sessions     *sessionMap
	cfg          *config.UdpMuxConfig
	log          logr.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// sessionReader runs as a goroutine for each session. It reads raw responses from
// the endpoint and forwards them framed back to the proxy.
func (m *mux) sessionReader(sess *muxSession, endpointStr string) {
	defer m.wg.Done()
	proxyAddrStr := sess.proxyAddr.String()
	buf := make([]byte, m.cfg.BufferSize)
	for {
		n, err := sess.conn.Read(buf)
		if err != nil {
			if m.ctx.Err() != nil {
				return
			}
			m.log.Info("session reader exiting", "proxy", proxyAddrStr, "err", err)
			return
		}
		m.log.Info("read(endpoint)", "bytes", n, "proxy", proxyAddrStr)

		h, err := frame.NewHeaderV1(endpointStr)
		if err != nil {
			m.log.Error(err, "failed to create response header")
			continue
		}
		h.Length = uint16(n)

		headerBytes, err := frame.EncodeV1(h)
		if err != nil {
			m.log.Error(err, "failed to encode response header")
			continue
		}

		datagram := append(headerBytes, buf[:n]...)
		m.writeMu.Lock()
		_, err = m.udpProxyConn.WriteToUDP(datagram, sess.proxyAddr)
		m.writeMu.Unlock()
		if err != nil {
			if m.ctx.Err() != nil {
				return
			}
			m.log.Error(err, "failed to write response to proxy", "proxy", proxyAddrStr)
		}
		m.log.Info("wrote(proxy)", "bytes", len(datagram), "proxy", proxyAddrStr)
	}
}

func run(ctx context.Context, log logr.Logger, cfg *config.UdpMuxConfig) error {
	log.Info("config parsed", "config", cfg)

	addr, err := net.ResolveUDPAddr("udp", cfg.Listen)
	if err != nil {
		return err
	}
	udpProxyConn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	defer udpProxyConn.Close()

	log.Info(fmt.Sprintf("%s listening", Name), "addr", addr.String())

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	m := &mux{
		udpProxyConn: udpProxyConn,
		sessions:     newSessionMap(),
		cfg:          cfg,
		log:          log,
		ctx:          ctx,
		cancel:       cancel,
	}

	// dispatch loop: receives framed datagrams from proxies and forwards to endpoints
	requestBuf := make([]byte, frame.HeaderV1Length+cfg.BufferSize)
	for {
		if ctx.Err() != nil {
			break
		}

		n, proxyAddr, err := udpProxyConn.ReadFromUDP(requestBuf)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Error(err, "failed to read from proxy")
			continue
		}
		if n < frame.HeaderV1Length {
			log.Error(fmt.Errorf("datagram too short: %d bytes", n), "invalid datagram from proxy")
			continue
		}
		log.Info("read(proxy)", "bytes", n, "proxy", proxyAddr.String())

		h, err := frame.DecodeV1(requestBuf[:frame.HeaderV1Length])
		if err != nil {
			log.Error(err, "failed to decode header")
			continue
		}

		endpointStr := string(h.Endpoint[:h.EndpointLen])

		// Copy payload out of the reused requestBuf before any async use.
		payload := make([]byte, h.Length)
		copy(payload, requestBuf[frame.HeaderV1Length:frame.HeaderV1Length+int(h.Length)])

		proxyAddrStr := proxyAddr.String()
		sess, created, err := m.sessions.getOrCreate(proxyAddrStr, func() (*muxSession, error) {
			conn, err := net.Dial("udp", endpointStr)
			if err != nil {
				return nil, err
			}
			s := &muxSession{conn: conn, proxyAddr: proxyAddr}
			s.idleTimer = time.AfterFunc(cfg.SessionTimeout, func() {
				m.sessions.mu.Lock()
				delete(m.sessions.sessions, proxyAddrStr)
				m.sessions.mu.Unlock()
				conn.Close()
				log.Info("session evicted (idle timeout)", "proxy", proxyAddrStr)
			})
			return s, nil
		})
		if err != nil {
			log.Error(err, "failed to create session", "endpoint", endpointStr)
			continue
		}

		if created {
			m.wg.Add(1)
			go m.sessionReader(sess, endpointStr)
			log.Info("new session", "proxy", proxyAddrStr, "endpoint", endpointStr)
		}

		if _, err = sess.conn.Write(payload); err != nil {
			log.Error(err, "failed to write to endpoint", "endpoint", endpointStr)
			m.sessions.delete(proxyAddrStr)
			sess.conn.Close()
			continue
		}
		log.Info("wrote(endpoint)", "bytes", len(payload), "endpoint", endpointStr)

		m.sessions.resetTimer(proxyAddrStr, cfg.SessionTimeout)
	}

	// Shutdown: close all session connections to unblock readerGoroutines.
	m.sessions.mu.Lock()
	for _, s := range m.sessions.sessions {
		s.idleTimer.Stop()
		s.conn.Close()
	}
	m.sessions.mu.Unlock()
	m.wg.Wait()
	return nil
}
