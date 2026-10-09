package config_test

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/domdom82/udpmux/pkg/config"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = Describe("UdpMuxConfig", func() {
	var cfg *config.UdpMuxConfig

	BeforeEach(func() {
		cfg = config.NewUdpMuxConfig(":8080", ":8081", 30*time.Second, 20*time.Second, 10*time.Second)
		cfg.KeepaliveIdle = 25 * time.Second
		cfg.KeepaliveInterval = 5 * time.Second
	})

	Describe("Validate", func() {
		It("passes with valid config", func() {
			Expect(cfg.Validate()).To(Succeed())
		})

		It("fails with empty listen address", func() {
			cfg = config.NewUdpMuxConfig("", ":8081", 30*time.Second, 20*time.Second, 10*time.Second)
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails with empty api listen address", func() {
			cfg = config.NewUdpMuxConfig(":8080", "", 30*time.Second, 20*time.Second, 10*time.Second)
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails with invalid listen address", func() {
			cfg = config.NewUdpMuxConfig("not-an-addr", ":8081", 0, 20*time.Second, 10*time.Second)
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails when session_timeout is zero", func() {
			cfg = config.NewUdpMuxConfig(":8080", ":8081", 0, 20*time.Second, 10*time.Second)
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("session_timeout"))
		})

		It("fails when keepalive_idle is zero", func() {
			cfg = config.NewUdpMuxConfig(":8080", ":8081", 30*time.Second, 0, 10*time.Second)
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("keepalive_idle"))
		})

		It("fails when keepalive_interval is zero", func() {
			cfg = config.NewUdpMuxConfig(":8080", ":8081", 30*time.Second, 25*time.Second, 0)
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("keepalive_interval"))
		})

		It("fails when keepalive_idle >= session_timeout", func() {
			cfg = config.NewUdpMuxConfig(":8080", ":8081", 30*time.Second, 30*time.Second, 5*time.Second)
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("keepalive_idle must be less than"))
		})
	})
})

var _ = Describe("UdpProxyConfig", func() {
	var cfg *config.UdpProxyConfig

	BeforeEach(func() {
		cfg = &config.UdpProxyConfig{
			ListenAddr:        ":7070",
			MuxAddr:           "127.0.0.1:8080",
			EndpointAddr:      "127.0.0.1:1194",
			SessionTimeout:    30 * time.Second,
			KeepaliveIdle:     25 * time.Second,
			KeepaliveInterval: 5 * time.Second,
		}
	})

	Describe("Validate", func() {
		It("passes with valid config", func() {
			Expect(cfg.Validate()).To(Succeed())
		})

		It("fails with empty listen address", func() {
			cfg.ListenAddr = ""
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails with empty mux address", func() {
			cfg.MuxAddr = ""
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails with invalid mux address", func() {
			cfg.MuxAddr = "not-valid"
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails when session_timeout is zero", func() {
			cfg.SessionTimeout = 0
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("session_timeout"))
		})

		It("fails when keepalive_idle is zero", func() {
			cfg.KeepaliveIdle = 0
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("keepalive_idle"))
		})

		It("fails when keepalive_idle >= session_timeout", func() {
			cfg.KeepaliveIdle = cfg.SessionTimeout
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("keepalive_idle must be less than"))
		})

		It("fails with negative ping size", func() {
			cfg.Ping = true
			cfg.PingSize = -1
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("passes with zero ping size", func() {
			cfg.Ping = true
			cfg.PingSize = 0
			Expect(cfg.Validate()).To(Succeed())
		})

		It("passes with positive ping size", func() {
			cfg.Ping = true
			cfg.PingSize = 100
			Expect(cfg.Validate()).To(Succeed())
		})
	})
})
