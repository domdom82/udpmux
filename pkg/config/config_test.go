package config_test

import (
	"testing"

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
		cfg = config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV2)
	})

	Describe("RegisterEndpoint / GetEndpoint / GetEndpointId", func() {
		It("registers and retrieves an endpoint", func() {
			id := cfg.RegisterEndpoint("10.0.0.1:1194")
			Expect(id).NotTo(BeZero())

			addr, err := cfg.GetEndpoint(id)
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("10.0.0.1:1194"))
		})

		It("returns the same id for the same endpoint", func() {
			id1 := cfg.RegisterEndpoint("10.0.0.1:1194")
			id2 := cfg.RegisterEndpoint("10.0.0.1:1194")
			Expect(id1).To(Equal(id2))
		})

		It("returns different ids for different endpoints", func() {
			id1 := cfg.RegisterEndpoint("10.0.0.1:1194")
			id2 := cfg.RegisterEndpoint("10.0.0.2:1194")
			Expect(id1).NotTo(Equal(id2))
		})

		It("retrieves endpoint id by address", func() {
			id := cfg.RegisterEndpoint("host:9000")
			got, err := cfg.GetEndpointId("host:9000")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(id))
		})

		It("returns error for unknown id", func() {
			_, err := cfg.GetEndpoint(999)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown endpoint id"))
		})

		It("returns error for unknown address", func() {
			_, err := cfg.GetEndpointId("missing:1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown endpoint"))
		})
	})

	Describe("UnregisterEndpoint", func() {
		It("removes a registered endpoint", func() {
			id := cfg.RegisterEndpoint("10.0.0.1:1194")
			Expect(cfg.UnregisterEndpoint("10.0.0.1:1194")).To(Succeed())
			_, err := cfg.GetEndpoint(id)
			Expect(err).To(HaveOccurred())
			_, err = cfg.GetEndpointId("10.0.0.1:1194")
			Expect(err).To(HaveOccurred())
		})

		It("returns error when unregistering unknown endpoint", func() {
			err := cfg.UnregisterEndpoint("never:registered")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListEndpointMappings", func() {
		It("returns all registered endpoints", func() {
			cfg.RegisterEndpoint("a:1")
			cfg.RegisterEndpoint("b:2")
			m := cfg.ListEndpointMappings()
			Expect(m).To(HaveLen(2))
			var addrs []string
			for _, v := range m {
				addrs = append(addrs, v)
			}
			Expect(addrs).To(ConsistOf("a:1", "b:2"))
		})

		It("returns a copy — mutations don't affect the config", func() {
			cfg.RegisterEndpoint("x:1")
			m := cfg.ListEndpointMappings()
			for k := range m {
				delete(m, k)
			}
			Expect(cfg.NumEndpoints()).To(Equal(1))
		})
	})

	Describe("NumEndpoints", func() {
		It("starts at zero", func() {
			Expect(cfg.NumEndpoints()).To(BeZero())
		})

		It("increments on registration", func() {
			cfg.RegisterEndpoint("h:1")
			cfg.RegisterEndpoint("h:2")
			Expect(cfg.NumEndpoints()).To(Equal(2))
		})

		It("decrements on unregistration", func() {
			cfg.RegisterEndpoint("h:1")
			_ = cfg.UnregisterEndpoint("h:1")
			Expect(cfg.NumEndpoints()).To(BeZero())
		})
	})

	Describe("Validate", func() {
		It("passes with valid addresses", func() {
			Expect(cfg.Validate()).To(Succeed())
		})

		It("fails with empty listen address", func() {
			cfg = config.NewUdpMuxConfig("", ":8081", config.ProtocolV2)
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails with empty api listen address", func() {
			cfg = config.NewUdpMuxConfig(":8080", "", config.ProtocolV2)
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails with invalid listen address", func() {
			cfg = config.NewUdpMuxConfig("not-an-addr", ":8081", config.ProtocolV2)
			Expect(cfg.Validate()).To(HaveOccurred())
		})
	})
})

var _ = Describe("UdpProxyConfig", func() {
	var cfg *config.UdpProxyConfig

	BeforeEach(func() {
		cfg = &config.UdpProxyConfig{
			ListenAddr:   ":7070",
			MuxAddr:      "127.0.0.1:8080",
			EndpointAddr: "127.0.0.1:1194",
			Protocol:     config.ProtocolV1,
		}
	})

	Describe("Validate", func() {
		It("passes with valid v1 config", func() {
			Expect(cfg.Validate()).To(Succeed())
		})

		It("passes with valid v2 config", func() {
			cfg.Protocol = config.ProtocolV2
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

		It("fails with empty protocol", func() {
			cfg.Protocol = ""
			Expect(cfg.Validate()).To(HaveOccurred())
		})

		It("fails with unknown protocol", func() {
			cfg.Protocol = "v3"
			Expect(cfg.Validate()).To(HaveOccurred())
			Expect(cfg.Validate().Error()).To(ContainSubstring("invalid protocol"))
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

var _ = Describe("EndpointToId", func() {
	It("returns a non-zero id for a non-empty endpoint", func() {
		id := config.EndpointToId("10.0.0.1:1194")
		Expect(id).NotTo(BeZero())
	})

	It("returns the same id for the same input", func() {
		Expect(config.EndpointToId("a:1")).To(Equal(config.EndpointToId("a:1")))
	})

	It("returns different ids for different inputs", func() {
		Expect(config.EndpointToId("a:1")).NotTo(Equal(config.EndpointToId("a:2")))
	})
})
