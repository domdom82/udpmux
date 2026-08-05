package proxy

import (
	"net"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

const (
	sessionTimeout         = 30 * time.Second // backend needs to respond before this to keep session alive
	sessionCleanupInterval = 10 * time.Second // clean up idle sessions after this period
	sessionChanCapacity    = 1000             // this many packets can be stored in a session packet channel
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
}

// SessionManager maps client addresses to active upstream sessions
type SessionManager struct {
	sessions map[string]*ClientSession
	mu       sync.RWMutex
	backend  *net.UDPAddr
	frontend *net.UDPConn
	log      logr.Logger
}

func newSessionManager(log logr.Logger, backend *net.UDPAddr, frontend *net.UDPConn) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*ClientSession),
		backend:  backend,
		frontend: frontend,
		log:      log,
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
	// Dial upstream backend endpoint for this specific client session
	upstreamConn, err := net.DialUDP("udp", nil, sm.backend)
	if err != nil {
		sm.log.Error(err, "Failed to dial backend for client", "client", key, "backend", sm.backend)
		return nil
	}

	// Maximize socket write/read buffers on upstream (limited by sysctl net.core.wmem_max / net.core.rmem_max)
	if err = upstreamConn.SetReadBuffer(socketBufferSize); err != nil {
		sm.log.Error(err, "Failed to set read socket buffer size", "client", key, "backend", sm.backend)
	}
	if err = upstreamConn.SetWriteBuffer(socketBufferSize); err != nil {
		sm.log.Error(err, "Failed to set write socket buffer size", "client", key, "backend", sm.backend)
	}

	session = &ClientSession{
		clientAddr:   clientAddr,
		backendConn:  upstreamConn,
		frontendConn: sm.frontend,
		sendChan:     make(chan []byte, sessionChanCapacity),
		lastActive:   time.Now(),
		done:         make(chan struct{}),
		log:          sm.log,
	}

	sm.sessions[key] = session

	// Start duplex loops:
	// 1. Forwarder: Client -> Proxy -> Backend
	// 2. Receiver: Backend -> Proxy -> Client
	go session.writeToBackendLoop()
	go session.readFromBackendLoop()

	return session
}

func (sm *SessionManager) remove(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[key]; exists {
		sm.log.Info("Session expired", "client", key, "backend", sm.backend)
		close(session.done)
		session.backendConn.Close()
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
			_, err := s.backendConn.Write(data)
			if err != nil {
				s.log.Error(err, "Failed sending packet to backend", "client", s.clientAddr.String(), "backend", s.backendConn.RemoteAddr())
			}
			s.refresh()
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
			// Read return packet from backend
			n, err := s.backendConn.Read(buf)
			if err != nil {
				return // Closed socket or session expired
			}
			// Hand off to hooks if any
			data := buf[:n]
			for _, hook := range s.readHooks {
				data = hook(data)
			}
			// Forward return packet back to original client via frontend socket
			_, err = s.frontendConn.WriteTo(buf[:n], s.clientAddr)
			if err != nil {
				s.log.Error(err, "Failed returning packet to client", "client", s.clientAddr.String(), "backend", s.backendConn.RemoteAddr())
			}
			s.refresh()
		}
	}
}
