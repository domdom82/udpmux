package proxy

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
)

const (
	sessionChanCapacity = 1000 // this many packets can be stored in a session packet channel

	DefaultSessionTimeout         = 30 * time.Second
	DefaultSessionCleanupInterval = 10 * time.Second // clean up idle sessions after this period
)

// ClientSession manages a 1:1 duplex connection between a client and the backend endpoint
type ClientSession struct {
	clientAddr   net.Addr
	backendConn  *net.UDPConn
	frontendConn *net.UDPConn
	sendChan     chan []byte
	lastActive   atomic.Int64 // UnixNano; updated without holding mu
	mu           sync.Mutex
	done         chan struct{}
	log          logr.Logger
	writeHooks   []Hook
	readHooks    []Hook

	keepaliveIdle     time.Duration
	keepaliveInterval time.Duration
	sendKeepalive     func() error // nil = keepalive not configured
	keepaliveDone     chan struct{}

	tags map[string]any // hook-owned state; opaque to the engine
}

func udpConnStr(clientAddr net.Addr, conn *net.UDPConn) string {
	connStr := "<nil>"
	if conn != nil {
		localStr := "<nil>"
		remoteStr := "<nil>"
		if conn.LocalAddr() != nil {
			if clientAddr != nil {
				localStr = clientAddr.String()
				remoteStr = conn.LocalAddr().String()
			} else {
				localStr = conn.LocalAddr().String()
			}
		}
		if conn.RemoteAddr() != nil {
			remoteStr = conn.RemoteAddr().String()
		}
		connStr = fmt.Sprintf("%s -> %s", localStr, remoteStr)
	}
	return connStr
}

func (s *ClientSession) String() string {
	age := time.Since(time.Unix(0, s.lastActive.Load()))
	return fmt.Sprintf("frontend %s backend %s age %.2fs", udpConnStr(s.clientAddr, s.frontendConn), udpConnStr(nil, s.backendConn), age.Seconds())
}

// SessionManager maps client addresses to active upstream sessions
type SessionManager struct {
	sessions        map[string]*ClientSession
	mu              sync.RWMutex
	backend         *net.UDPAddr
	frontend        *net.UDPConn
	log             logr.Logger
	writeHooks      []Hook
	readHooks       []Hook
	sessionTimeout  time.Duration
	cleanupInterval time.Duration
}

func newSessionManager(log logr.Logger, backend *net.UDPAddr, frontend *net.UDPConn, writeHooks []Hook, readHooks []Hook, sessionTimeout time.Duration, cleanupInterval time.Duration) *SessionManager {
	if sessionTimeout <= 0 {
		sessionTimeout = DefaultSessionTimeout
	}
	if cleanupInterval <= 0 {
		cleanupInterval = DefaultSessionCleanupInterval
	}
	sm := &SessionManager{
		sessions:        make(map[string]*ClientSession),
		backend:         backend,
		frontend:        frontend,
		log:             log,
		writeHooks:      writeHooks,
		readHooks:       readHooks,
		sessionTimeout:  sessionTimeout,
		cleanupInterval: cleanupInterval,
	}
	// Start session cleanup worker for idle sessions
	go sm.cleanupRoutine()
	return sm
}

func (sm *SessionManager) getOrCreate(key string, clientAddr net.Addr) *ClientSession {
	// Fast path: Return session if it exists.
	sm.mu.RLock()
	session, exists := sm.sessions[key]
	sm.mu.RUnlock()

	if exists {
		return session
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double check after acquiring write lock
	if session, exists = sm.sessions[key]; exists {
		return session
	}

	// Slow path: Create new session
	// Dial upstream backend endpoint for this specific client session if needed
	var backendConn *net.UDPConn
	var err error
	if sm.backend != nil {
		backendConn, err = DialBackend(sm.backend)
		if err != nil {
			sm.log.Error(err, "failed to dial backend", "client", key)
			return nil
		}
	}

	session = &ClientSession{
		clientAddr:   clientAddr,
		backendConn:  backendConn,
		frontendConn: sm.frontend,
		sendChan:     make(chan []byte, sessionChanCapacity),
		done:         make(chan struct{}),
		log:          sm.log,
		writeHooks:   sm.writeHooks,
		readHooks:    sm.readHooks,
	}
	session.lastActive.Store(time.Now().UnixNano())

	sm.sessions[key] = session

	// Start duplex loops:
	// 1. Forwarder: Client -> Proxy -> Backend
	// 2. Receiver: Backend -> Proxy -> Client
	go session.writeToBackendLoop()
	go session.readFromBackendLoop()

	sm.log.Info("Session created", "session", session)
	return session
}

func (sm *SessionManager) remove(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[key]; exists {
		sm.log.Info("Session expired", "session", session)
		close(session.done)
		if session.backendConn != nil {
			session.backendConn.Close()
		}
		delete(sm.sessions, key)
	}
}

func (sm *SessionManager) NumSessions() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

func (sm *SessionManager) cleanupRoutine() {
	ticker := time.NewTicker(sm.cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now().UnixNano()
		timeout := sm.sessionTimeout.Nanoseconds()
		sm.mu.RLock()
		var expiredKeys []string
		for key, session := range sm.sessions {
			if now-session.lastActive.Load() > timeout {
				expiredKeys = append(expiredKeys, key)
			}
		}
		sm.mu.RUnlock()

		for _, key := range expiredKeys {
			sm.remove(key)
		}
	}
}

func (s *ClientSession) refresh() {
	s.lastActive.Store(time.Now().UnixNano())
}

// Refresh is the exported equivalent of refresh, for use by protocol hooks.
func (s *ClientSession) Refresh() {
	s.refresh()
}

// Full Duplex Loop 1: Proxy -> Backend (Client outbound traffic)
func (s *ClientSession) writeToBackendLoop() {
	for {
		select {
		case <-s.done:
			return
		case data, ok := <-s.sendChan:
			if !ok {
				return
			}
			s.refresh()
			// Hand off to hooks if any
			var err error
			for _, hook := range s.writeHooks {
				data, err = hook(s, data)
				if err != nil {
					s.log.Error(err, "Failed to call write hook", "session", s)
				}
			}
			// No data means the hook consumed it, so don't forward to backend.
			if data == nil {
				continue
			}
			conn := s.GetBackendConn()
			if conn == nil {
				s.log.Error(err, "Missing backend connection", "session", s)
				continue
			}
			_, err = conn.Write(data)
			if err != nil {
				s.log.Error(err, "Failed sending packet to backend", "session", s)
				continue
			}
		}
	}
}

// Full Duplex Loop 2: Backend -> Proxy -> Client (Return traffic)
func (s *ClientSession) readFromBackendLoop() {
	buf := make([]byte, packetBufferSize)
	for {
		select {
		case <-s.done:
			return
		default:
			conn := s.GetBackendConn()
			if conn == nil {
				time.Sleep(100 * time.Millisecond) // No backend connection yet, wait before retrying
				continue
			}
			// Read return packet from backend
			n, err := conn.Read(buf)
			if err != nil {
				return // Closed socket or session expired
			}
			s.refresh()
			// Hand off to hooks if any
			data := buf[:n]
			for _, hook := range s.readHooks {
				data, err = hook(s, data)
				if err != nil {
					s.log.Error(err, "Failed to call read hook", "client", s.clientAddr.String(), "backend", conn.RemoteAddr())
				}
			}
			// No data means the hook consumed it, so don't forward to client.
			if data == nil {
				continue
			}
			// Forward return packet back to original client via frontend socket
			_, err = s.frontendConn.WriteTo(data, s.clientAddr)
			if err != nil {
				s.log.Error(err, "Failed returning packet to client", "client", s.clientAddr.String(), "backend", conn.RemoteAddr())
			}
		}
	}
}

func (s *ClientSession) GetClientAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientAddr
}

func (s *ClientSession) GetFrontendConn() *net.UDPConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frontendConn
}

func (s *ClientSession) GetBackendConn() *net.UDPConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backendConn
}

func (s *ClientSession) SetBackendConn(conn *net.UDPConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backendConn = conn
}

// SetTag stores hook-owned state on the session under the given key.
func (s *ClientSession) SetTag(key string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tags == nil {
		s.tags = make(map[string]any)
	}
	s.tags[key] = v
}

// GetTag returns the hook-owned value stored under key, or nil if not set.
func (s *ClientSession) GetTag(key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tags[key]
}

// StartKeepalive begins sending keepalive frames when the session is idle.
// Idempotent: calling it again while already running is a no-op.
func (s *ClientSession) StartKeepalive(idle, interval time.Duration, send func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keepaliveDone != nil {
		return // already running
	}
	s.keepaliveIdle = idle
	s.keepaliveInterval = interval
	s.sendKeepalive = send
	s.keepaliveDone = make(chan struct{})
	go s.keepaliveLoop(s.keepaliveDone)
}

// StopKeepalive stops the keepalive goroutine if it is running.
func (s *ClientSession) StopKeepalive() {
	s.mu.Lock()
	ch := s.keepaliveDone
	s.keepaliveDone = nil
	s.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
}

func (s *ClientSession) keepaliveLoop(done chan struct{}) {
	ticker := time.NewTicker(s.keepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-s.done:
			return
		case <-ticker.C:
			idle := time.Duration(time.Now().UnixNano() - s.lastActive.Load())
			if idle >= s.keepaliveIdle {
				if err := s.sendKeepalive(); err != nil {
					s.log.Error(err, "failed to send keepalive")
				}
				// Do NOT Refresh() here: only received traffic refreshes expiry.
			}
		}
	}
}

func DialBackend(backend *net.UDPAddr) (*net.UDPConn, error) {
	backendConn, err := net.DialUDP("udp", nil, backend)
	if err != nil {
		return nil, err
	}

	// Maximize socket write/read buffers on upstream (limited by sysctl net.core.wmem_max / net.core.rmem_max)
	if err = backendConn.SetReadBuffer(socketBufferSize); err != nil {
		return nil, err
	}
	if err = backendConn.SetWriteBuffer(socketBufferSize); err != nil {
		return nil, err
	}

	return backendConn, nil
}
