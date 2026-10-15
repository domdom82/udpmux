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
		cfg = &config.UdpMuxConfig{
			ListenAddr:      ":8080",
			ApiListenAddr:   ":8081",
			SessionTimeout:  30 * time.Second,
			CleanupInterval: 10 * time.Second,
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

		It("fails with empty api listen address", func() {
			cfg.ApiListenAddr = ""
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails with invalid listen address", func() {
			cfg.ListenAddr = "not-an-addr"
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails when sessionTimeout is zero", func() {
			cfg.SessionTimeout = 0
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("sessionTimeout"))
		})
		It("fails when cleanupInterval is zero", func() {
			cfg.CleanupInterval = 0
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("cleanupInterval"))
		})

		It("passes with a valid endpointAllowList", func() {
			cfg.EndpointAllowList = []string{`^10\.0\.0\.\d+:1194$`, `^127\.0\.0\.1:\d+$`}
			Expect(cfg.Validate()).To(Succeed())
		})

		It("fails with an invalid regexp in endpointAllowList", func() {
			cfg.EndpointAllowList = []string{`[invalid`}
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("endpointAllowList"))
		})

		It("exposes compiled patterns via CompiledAllowList after Validate", func() {
			cfg.EndpointAllowList = []string{`^127\.0\.0\.1:\d+$`}
			Expect(cfg.Validate()).To(Succeed())
			Expect(cfg.CompiledAllowList()).To(HaveLen(1))
		})
	})
})

var _ = Describe("UdpProxyConfig", func() {
	var cfg *config.UdpProxyConfig

	BeforeEach(func() {
		cfg = &config.UdpProxyConfig{
			ListenAddr:      ":7070",
			MuxAddr:         "127.0.0.1:8080",
			EndpointAddr:    "127.0.0.1:1194",
			SessionTimeout:  30 * time.Second,
			CleanupInterval: 10 * time.Second,
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

		It("fails with empty endpoint address", func() {
			cfg.EndpointAddr = ""
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("succeeds with empty endpoint address but ping is enabled", func() {
			cfg.EndpointAddr = ""
			cfg.Ping = true
			Expect(cfg.Validate()).To(Succeed())
		})

		It("fails with invalid mux address", func() {
			cfg.MuxAddr = "not-valid"
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails when sessionTimeout is zero", func() {
			cfg.SessionTimeout = 0
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("sessionTimeout"))
		})

		It("fails when cleanupInterval is zero", func() {
			cfg.CleanupInterval = 0
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("cleanupInterval"))
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
