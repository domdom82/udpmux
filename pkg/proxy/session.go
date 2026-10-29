package proxy

import (
	"fmt"
	"net"
	"slices"
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
	timeout      int64
	mu           sync.Mutex
	done         chan struct{}
	log          logr.Logger
	writeHooks   []Hook
	readHooks    []Hook

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
	return fmt.Sprintf("frontend %s backend %s age %s", udpConnStr(s.clientAddr, s.frontendConn), udpConnStr(nil, s.backendConn), age)
}

// SessionMetrics holds counters and gauges for session management
type SessionMetrics struct {
	SessionsGauge                            atomic.Int64
	ClientsGauge                             atomic.Int64
	SessionsExpiredCounter                   atomic.Int64
	SessionsResetCounter                     atomic.Int64
	BogusSessionsCounter                     atomic.Int64
	FailedHandshakesCounter                  atomic.Int64
	ClientsBlockedBySessionLimitCounter      atomic.Int64
	ClientsBlockedByEndpointAllowlistCounter atomic.Int64
	FrontendBytesReadCounter                 atomic.Int64
	FrontendBytesWrittenCounter              atomic.Int64
	BackendBytesReadCounter                  atomic.Int64
	BackendBytesWrittenCounter               atomic.Int64
}

// SessionManager maps client addresses to active upstream sessions
type SessionManager struct {
	sessions             map[string]*ClientSession
	clientSessions       map[string][]*ClientSession // client IP → sessions; kept in sync with sessions
	mu                   sync.RWMutex
	backend              *net.UDPAddr
	frontend             *net.UDPConn
	log                  logr.Logger
	writeHooks           []Hook
	readHooks            []Hook
	sessionTimeout       time.Duration
	cleanupInterval      time.Duration
	maxSessionsPerClient int // 0 = unlimited
	metrics              SessionMetrics
}

func newSessionManager(log logr.Logger, backend *net.UDPAddr, frontend *net.UDPConn, writeHooks []Hook, readHooks []Hook, sessionTimeout time.Duration, cleanupInterval time.Duration, maxSessionsPerClient int) *SessionManager {
	if sessionTimeout <= 0 {
		sessionTimeout = DefaultSessionTimeout
	}
	if cleanupInterval <= 0 {
		cleanupInterval = DefaultSessionCleanupInterval
	}
	sm := &SessionManager{
		sessions:             make(map[string]*ClientSession),
		clientSessions:       make(map[string][]*ClientSession),
		backend:              backend,
		frontend:             frontend,
		log:                  log,
		writeHooks:           writeHooks,
		readHooks:            readHooks,
		sessionTimeout:       sessionTimeout,
		cleanupInterval:      cleanupInterval,
		maxSessionsPerClient: maxSessionsPerClient,
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

	if exists && !session.IsExpired() {
		return session
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double check after acquiring write lock
	if session, exists = sm.sessions[key]; exists && !session.IsExpired() {
		return session
	}

	// Slow path: Create new session
	// Check per-client session limit before allocating.
	clientIP := extractIP(clientAddr)
	if sm.maxSessionsPerClient > 0 {
		if len(sm.clientSessions[clientIP]) >= sm.maxSessionsPerClient {
			sm.log.Info("Per-client session limit reached, rejecting new session", "client", clientIP, "limit", sm.maxSessionsPerClient)
			sm.metrics.ClientsBlockedBySessionLimitCounter.Add(1)
			return nil
		}
	}

	// Dial upstream backend endpoint for this specific client session if needed
	var backendConn *net.UDPConn
	var err error
	if sm.backend != nil {
		backendConn, err = DialBackend(sm.backend)
		if err != nil {
			sm.log.Error(err, "Failed to dial backend", "client", key)
			sm.metrics.FailedHandshakesCounter.Add(1)
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
		timeout:      sm.sessionTimeout.Nanoseconds(),
	}
	session.lastActive.Store(time.Now().UnixNano())

	sm.sessions[key] = session
	sm.clientSessions[clientIP] = append(sm.clientSessions[clientIP], session)

	sm.metrics.SessionsGauge.Store(int64(len(sm.sessions)))
	sm.metrics.ClientsGauge.Store(int64(len(sm.clientSessions)))

	// Start duplex loops:
	// 1. Forwarder: Client -> Proxy -> Backend
	// 2. Receiver: Backend -> Proxy -> Client
	go session.writeToBackendLoop(sm)
	go session.readFromBackendLoop(sm)

	sm.log.Info("Session created", "session", session)
	return session
}

func (sm *SessionManager) remove(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[key]
	if !exists {
		return
	}
	sm.log.Info("Session expired", "session", session)
	close(session.done)
	if conn := session.GetBackendConn(); conn != nil {
		_ = conn.Close()
	}
	delete(sm.sessions, key)

	clientIP := extractIP(session.clientAddr)
	list := sm.clientSessions[clientIP]
	for i, s := range list {
		if s == session {
			sm.clientSessions[clientIP] = slices.Delete(list, i, i+1)
			break
		}
	}
	if len(sm.clientSessions[clientIP]) == 0 {
		delete(sm.clientSessions, clientIP)
	}

	sm.metrics.SessionsGauge.Store(int64(len(sm.sessions)))
	sm.metrics.ClientsGauge.Store(int64(len(sm.clientSessions)))
}

func (sm *SessionManager) cleanupRoutine() {
	ticker := time.NewTicker(sm.cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		sm.mu.RLock()
		var expiredKeys []string
		for key, session := range sm.sessions {
			if session.IsExpired() {
				expiredKeys = append(expiredKeys, key)
			}
		}
		sm.mu.RUnlock()

		for _, key := range expiredKeys {
			sm.metrics.SessionsExpiredCounter.Add(1)
			sm.remove(key)
		}
	}
}

func (sm *SessionManager) GetMetrics() *SessionMetrics {
	return &sm.metrics
}

func (s *ClientSession) refresh() {
	s.lastActive.Store(time.Now().UnixNano())
}

// Refresh is the exported equivalent of refresh, for use by protocol hooks.
func (s *ClientSession) Refresh() {
	s.refresh()
}

// Expire marks the session as immediately eligible for cleanup by zeroing lastActive.
// The cleanup routine will remove it on the next tick.
func (s *ClientSession) Expire() {
	s.lastActive.Store(0)
}

// IsExpired reports whether the session has expired.
func (s *ClientSession) IsExpired() bool {
	now := time.Now().UnixNano()
	return now-s.lastActive.Load() > s.timeout
}

// extractIP returns the host portion of addr.String(), stripping the port.
func extractIP(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// Full Duplex Loop 1: Proxy -> Backend (Client outbound traffic)
func (s *ClientSession) writeToBackendLoop(sm *SessionManager) {
	for {
		select {
		case <-s.done:
			return
		case data, ok := <-s.sendChan:
			if !ok {
				return
			}
			sm.metrics.FrontendBytesReadCounter.Add(int64(len(data)))
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
			s.refresh()
			conn := s.GetBackendConn()
			if conn == nil {
				s.log.Error(err, "Missing backend connection", "session", s)
				continue
			}
			n, err := conn.Write(data)
			if err != nil {
				s.log.Error(err, "Failed sending packet to backend", "session", s)
				continue
			}
			sm.metrics.BackendBytesWrittenCounter.Add(int64(n))
		}
	}
}

// Full Duplex Loop 2: Backend -> Proxy -> Client (Return traffic)
func (s *ClientSession) readFromBackendLoop(sm *SessionManager) {
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
			sm.metrics.BackendBytesReadCounter.Add(int64(n))
			// Hand off to hooks if any
			data := buf[:n]
			for _, hook := range s.readHooks {
				data, err = hook(s, data)
				if err != nil {
					s.log.Error(err, "Failed to call read hook", "session", s)
				}
			}
			// No data means the hook consumed it, so don't forward to client.
			if data == nil {
				continue
			}
			s.refresh()
			// Forward return packet back to original client via frontend socket
			n, err = s.frontendConn.WriteTo(data, s.clientAddr)
			if err != nil {
				s.log.Error(err, "Failed returning packet to client", "session", s)
			}
			sm.metrics.FrontendBytesWrittenCounter.Add(int64(n))

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
