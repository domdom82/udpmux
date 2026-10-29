package proxy

import (
	"net"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// bindInternal binds a random loopback UDP port for use in internal tests.
func bindInternal() (*net.UDPConn, *net.UDPAddr) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	Expect(err).NotTo(HaveOccurred())
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

// newSM creates a SessionManager wired to throwaway loopback sockets.
func newSM(sessionTimeout, cleanupInterval time.Duration, maxSessionsPerClient int) (*SessionManager, func()) {
	backend, backendAddr := bindInternal()
	frontend, _ := bindInternal()
	sm := newSessionManager(logr.Discard(), backendAddr, frontend, nil, nil, sessionTimeout, cleanupInterval, maxSessionsPerClient)
	return sm, func() {
		backend.Close()
		frontend.Close()
	}
}

// addr builds a throwaway *net.UDPAddr for a given port on 127.0.0.1.
func addr(port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
}

var _ = Describe("SessionManager", func() {

	Describe("getOrCreate", func() {
		It("creates a new session for an unknown address", func() {
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 0)
			defer cleanup()

			a := addr(9100)
			s := sm.getOrCreate(a.String(), a)
			Expect(s).NotTo(BeNil())
			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(1)))
		})

		It("returns the same session on repeated calls for the same address", func() {
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 0)
			defer cleanup()

			a := addr(9101)
			s1 := sm.getOrCreate(a.String(), a)
			s2 := sm.getOrCreate(a.String(), a)
			Expect(s1).To(BeIdenticalTo(s2))
			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(1)))
		})

		It("creates distinct sessions for different addresses", func() {
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 0)
			defer cleanup()

			a1, a2 := addr(9102), addr(9103)
			s1 := sm.getOrCreate(a1.String(), a1)
			s2 := sm.getOrCreate(a2.String(), a2)
			Expect(s1).NotTo(BeIdenticalTo(s2))
			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(2)))
		})

		Context("per-client session limit", func() {
			It("allows sessions up to the limit", func() {
				sm, cleanup := newSM(30*time.Second, 10*time.Second, 2)
				defer cleanup()

				Expect(sm.getOrCreate(addr(9110).String(), addr(9110))).NotTo(BeNil())
				Expect(sm.getOrCreate(addr(9111).String(), addr(9111))).NotTo(BeNil())
				Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(2)))
			})

			It("rejects sessions beyond the limit for the same IP", func() {
				sm, cleanup := newSM(30*time.Second, 10*time.Second, 2)
				defer cleanup()

				sm.getOrCreate(addr(9112).String(), addr(9112))
				sm.getOrCreate(addr(9113).String(), addr(9113))
				s := sm.getOrCreate(addr(9114).String(), addr(9114))
				Expect(s).To(BeNil())
				Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(2)))
			})

			It("allows unlimited sessions when limit is 0", func() {
				sm, cleanup := newSM(30*time.Second, 10*time.Second, 0)
				defer cleanup()

				for i := range 10 {
					s := sm.getOrCreate(addr(9120+i).String(), addr(9120+i))
					Expect(s).NotTo(BeNil())
				}
				Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(10)))
			})

			It("tracks clientSessions by IP, not by full address", func() {
				sm, cleanup := newSM(30*time.Second, 10*time.Second, 1)
				defer cleanup()

				// Two addresses, same IP (127.0.0.1) — only one session allowed.
				sm.getOrCreate(addr(9130).String(), addr(9130))
				s := sm.getOrCreate(addr(9131).String(), addr(9131))
				Expect(s).To(BeNil())
				Expect(len(sm.clientSessions)).To(Equal(1))
			})
		})
	})

	Describe("remove", func() {
		It("removes the session from sessions map", func() {
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 0)
			defer cleanup()

			a := addr(9200)
			sm.getOrCreate(a.String(), a)
			sm.remove(a.String())
			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(0)))
		})

		It("is a no-op for an unknown key", func() {
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 0)
			defer cleanup()

			Expect(func() { sm.remove("nonexistent") }).NotTo(Panic())
			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(0)))
		})

		It("prunes the clientSessions entry when the last session for an IP is removed", func() {
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 1)
			defer cleanup()

			a := addr(9201)
			sm.getOrCreate(a.String(), a)
			Expect(len(sm.clientSessions)).To(Equal(1))

			sm.remove(a.String())
			Expect(len(sm.clientSessions)).To(Equal(0))
		})

		It("keeps the clientSessions entry when other sessions for the same IP remain", func() {
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 3)
			defer cleanup()

			a1, a2 := addr(9202), addr(9203)
			sm.getOrCreate(a1.String(), a1)
			sm.getOrCreate(a2.String(), a2)
			sm.remove(a1.String())

			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(1)))
			Expect(len(sm.clientSessions["127.0.0.1"])).To(Equal(1))
		})

		It("allows a new session after the last session for an IP is removed", func() {
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 1)
			defer cleanup()

			a := addr(9204)
			sm.getOrCreate(a.String(), a)
			sm.remove(a.String())

			s := sm.getOrCreate(a.String(), a)
			Expect(s).NotTo(BeNil(), "per-client limit must not be phantom-blocked after remove")
		})
	})

	Describe("SessionsGauge / ClientsGauge metrics", func() {
		It("counts distinct client IPs, not sessions", func() {
			// limit=2 so both ports on the same IP are allowed
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 2)
			defer cleanup()

			// Two sessions from the same IP (different ports) = 1 client, 2 sessions.
			a1, a2 := addr(9400), addr(9401)
			Expect(sm.getOrCreate(a1.String(), a1)).NotTo(BeNil())
			Expect(sm.getOrCreate(a2.String(), a2)).NotTo(BeNil())

			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(2)))
			Expect(sm.GetMetrics().ClientsGauge.Load()).To(Equal(int64(1)))
		})

		It("counts each distinct client IP separately", func() {
			sm, cleanup := newSM(30*time.Second, 10*time.Second, 0)
			defer cleanup()

			// Use two different IPs to represent two distinct clients.
			c1 := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9410}
			c2 := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 9411}
			Expect(sm.getOrCreate(c1.String(), c1)).NotTo(BeNil())
			Expect(sm.getOrCreate(c2.String(), c2)).NotTo(BeNil())

			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(2)))
			Expect(sm.GetMetrics().ClientsGauge.Load()).To(Equal(int64(2)))
		})

		It("returns 0 for both counts after all sessions expire", func() {
			sm, cleanup := newSM(10*time.Millisecond, 20*time.Millisecond, 2)
			defer cleanup()

			a1, a2 := addr(9420), addr(9421)
			Expect(sm.getOrCreate(a1.String(), a1)).NotTo(BeNil())
			Expect(sm.getOrCreate(a2.String(), a2)).NotTo(BeNil())

			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(2)))
			Expect(sm.GetMetrics().ClientsGauge.Load()).To(Equal(int64(1)))

			Eventually(func() int64 { return sm.GetMetrics().SessionsGauge.Load() }, 300*time.Millisecond, 10*time.Millisecond).Should(Equal(int64(0)))
			Expect(sm.GetMetrics().ClientsGauge.Load()).To(Equal(int64(0)))
		})
	})

	Describe("cleanupRoutine", func() {
		It("reaps sessions that have exceeded the session timeout", func() {
			sm, cleanup := newSM(10*time.Millisecond, 20*time.Millisecond, 0)
			defer cleanup()

			a := addr(9300)
			sm.getOrCreate(a.String(), a)
			Expect(sm.GetMetrics().SessionsGauge.Load()).To(Equal(int64(1)))

			Eventually(func() int64 { return sm.GetMetrics().SessionsGauge.Load() }, 300*time.Millisecond, 10*time.Millisecond).Should(Equal(int64(0)))
		})

		It("reaps a session immediately after Expire() is called", func() {
			sm, cleanup := newSM(time.Hour, 20*time.Millisecond, 0)
			defer cleanup()

			a := addr(9301)
			s := sm.getOrCreate(a.String(), a)
			s.Expire()

			Eventually(func() int64 { return sm.GetMetrics().SessionsGauge.Load() }, 300*time.Millisecond, 10*time.Millisecond).Should(Equal(int64(0)))
		})

		It("does not reap active sessions within the timeout window", func() {
			sm, cleanup := newSM(time.Hour, 20*time.Millisecond, 0)
			defer cleanup()

			a := addr(9302)
			sm.getOrCreate(a.String(), a)

			Consistently(func() int64 { return sm.GetMetrics().SessionsGauge.Load() }, 100*time.Millisecond, 20*time.Millisecond).Should(Equal(int64(1)))
		})
	})

})

var _ = Describe("ClientSession", func() {

	Describe("Refresh / Expire", func() {
		It("Refresh updates lastActive to approximately now", func() {
			s := &ClientSession{}
			s.lastActive.Store(0)
			before := time.Now().UnixNano()
			s.Refresh()
			after := time.Now().UnixNano()
			Expect(s.lastActive.Load()).To(BeNumerically(">=", before))
			Expect(s.lastActive.Load()).To(BeNumerically("<=", after))
		})

		It("Expire sets lastActive to zero", func() {
			s := &ClientSession{}
			s.lastActive.Store(time.Now().UnixNano())
			s.Expire()
			Expect(s.lastActive.Load()).To(BeZero())
		})
	})

	Describe("SetTag / GetTag", func() {
		It("stores and retrieves a value by key", func() {
			s := &ClientSession{}
			s.SetTag("foo", 42)
			Expect(s.GetTag("foo")).To(Equal(42))
		})

		It("returns nil for an unset key", func() {
			s := &ClientSession{}
			Expect(s.GetTag("missing")).To(BeNil())
		})

		It("overwrites an existing value", func() {
			s := &ClientSession{}
			s.SetTag("k", "first")
			s.SetTag("k", "second")
			Expect(s.GetTag("k")).To(Equal("second"))
		})

		It("isolates keys from each other", func() {
			s := &ClientSession{}
			s.SetTag("a", 1)
			s.SetTag("b", 2)
			Expect(s.GetTag("a")).To(Equal(1))
			Expect(s.GetTag("b")).To(Equal(2))
		})
	})

	Describe("SetBackendConn / GetBackendConn", func() {
		It("returns nil before any conn is set", func() {
			s := &ClientSession{}
			Expect(s.GetBackendConn()).To(BeNil())
		})

		It("returns the conn after it is set", func() {
			server, serverAddr := bindInternal()
			defer server.Close()
			conn, err := net.DialUDP("udp", nil, serverAddr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			s := &ClientSession{}
			s.SetBackendConn(conn)
			Expect(s.GetBackendConn()).To(Equal(conn))
		})
	})

	Describe("GetClientAddr / GetFrontendConn", func() {
		It("returns the addresses stored at creation time", func() {
			backend, backendAddr := bindInternal()
			defer backend.Close()
			frontend, _ := bindInternal()
			defer frontend.Close()

			sm := newSessionManager(logr.Discard(), backendAddr, frontend, nil, nil, 30*time.Second, 10*time.Second, 0)
			a := addr(9500)
			s := sm.getOrCreate(a.String(), a)
			Expect(s).NotTo(BeNil())

			Expect(s.GetClientAddr().String()).To(Equal(a.String()))
			Expect(s.GetFrontendConn()).To(Equal(frontend))
		})
	})

})

var _ = Describe("extractIP", func() {
	It("strips the port from a host:port address", func() {
		a := &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5678}
		Expect(extractIP(a)).To(Equal("1.2.3.4"))
	})

	It("handles IPv6 addresses", func() {
		a := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 80}
		Expect(extractIP(a)).To(Equal("::1"))
	})
})
