package proxy

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

const (
	sessionTimeout           = 30 * time.Second // backend needs to respond before this to keep session alive
	sessionCleanupInterval   = 10 * time.Second // clean up idle sessions after this period
	sessionChanCapacity      = 1000             // this many packets can be stored in a session packet channel
	SessionMetaEndpointKey   = "endpoint"       // key for storing the endpoint address in session metadata
	SessionMetaTypeKey       = "type"           // key for storing the session type in session metadata
	SessionMetaTypeNoBackend = "no_backend"     // value for session type metadata indicating no backend connection is needed (e.g., for ping responses)
)

// ClientSession manages a 1:1 duplex connection between a client and the backend endpoint
type ClientSession struct {
	clientAddr   net.Addr
	backendConn  *net.UDPConn
	frontendConn *net.UDPConn
	sendChan     chan []byte
	lastActive   time.Time
	mu           sync.Mutex
	done         chan struct{}
	log          logr.Logger
	writeHooks   []Hook
	readHooks    []Hook
	metaData     map[string]string
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
	return fmt.Sprintf("frontend %s backend %s age %.2fs", udpConnStr(s.clientAddr, s.frontendConn), udpConnStr(nil, s.backendConn), time.Now().Sub(s.lastActive).Seconds())
}

// SessionManager maps client addresses to active upstream sessions
type SessionManager struct {
	sessions   map[string]*ClientSession
	mu         sync.RWMutex
	backend    *net.UDPAddr
	frontend   *net.UDPConn
	log        logr.Logger
	writeHooks []Hook
	readHooks  []Hook
}

func newSessionManager(log logr.Logger, backend *net.UDPAddr, frontend *net.UDPConn, writeHooks []Hook, readHooks []Hook) *SessionManager {
	sm := &SessionManager{
		sessions:   make(map[string]*ClientSession),
		backend:    backend,
		frontend:   frontend,
		log:        log,
		writeHooks: writeHooks,
		readHooks:  readHooks,
	}
	// Start session cleanup worker for idle sessions
	go sm.cleanupRoutine()
	return sm
}

func (sm *SessionManager) getOrCreate(clientAddr net.Addr) *ClientSession {
	key := clientAddr.String()

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
		lastActive:   time.Now(),
		done:         make(chan struct{}),
		log:          sm.log,
		writeHooks:   sm.writeHooks,
		readHooks:    sm.readHooks,
		metaData:     make(map[string]string),
	}

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

func (sm *SessionManager) cleanupRoutine() {
	ticker := time.NewTicker(sessionCleanupInterval)
	for range ticker.C {
		now := time.Now()
		sm.mu.RLock()
		var expiredKeys []string
		for key, session := range sm.sessions {
			session.mu.Lock()
			if now.Sub(session.lastActive) > sessionTimeout {
				expiredKeys = append(expiredKeys, key)
			}
			session.mu.Unlock()
		}
		sm.mu.RUnlock()

		for _, key := range expiredKeys {
			sm.remove(key)
		}
	}
}

func (s *ClientSession) refresh() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()
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
			if sessionType, _ := s.GetMetaData(SessionMetaTypeKey); sessionType == SessionMetaTypeNoBackend {
				// No backend connection needed, just drop the packet
				continue
			}
			if s.backendConn == nil {
				s.log.Error(err, "Missing backend connection", "session", s)
				continue
			}
			_, err = s.backendConn.Write(data)
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
			if s.backendConn == nil {
				time.Sleep(100 * time.Millisecond) // No backend connection yet, wait before retrying
				continue
			}
			// Read return packet from backend
			n, err := s.backendConn.Read(buf)
			if err != nil {
				return // Closed socket or session expired
			}
			s.refresh()
			// Hand off to hooks if any
			data := buf[:n]
			for _, hook := range s.readHooks {
				data, err = hook(s, data)
				if err != nil {
					s.log.Error(err, "Failed to call read hook", "client", s.clientAddr.String(), "backend", s.backendConn.RemoteAddr())
				}
			}
			// Forward return packet back to original client via frontend socket
			_, err = s.frontendConn.WriteTo(data, s.clientAddr)
			if err != nil {
				s.log.Error(err, "Failed returning packet to client", "client", s.clientAddr.String(), "backend", s.backendConn.RemoteAddr())
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

func (s *ClientSession) SetMetaData(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metaData[key] = value
}

func (s *ClientSession) GetMetaData(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, exists := s.metaData[key]
	if !exists {
		return "", fmt.Errorf("key not found: %s", key)
	}
	return value, nil
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
