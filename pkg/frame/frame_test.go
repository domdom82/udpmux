package frame_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/domdom82/udpmux/pkg/frame"
)

func TestFrame(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Frame Suite")
}

var _ = Describe("Frame", func() {
	Describe("V1", func() {
		Describe("NewHeaderV1", func() {
			It("creates a valid header with normal endpoint", func() {
				h, err := frame.NewHeaderV1("localhost:1234", []byte("hello"))
				Expect(err).NotTo(HaveOccurred())
				Expect(h.Magic).To(Equal(frame.Magic))
				Expect(h.Version).To(Equal(frame.VersionV1))
				Expect(h.Flags).To(BeZero())
				Expect(h.Length).To(Equal(uint16(5)))
				Expect(h.EndpointLen).To(Equal(uint8(14)))
				Expect(string(h.Endpoint[:h.EndpointLen])).To(Equal("localhost:1234"))
			})

			It("accepts an empty payload", func() {
				h, err := frame.NewHeaderV1("127.0.0.1:80", []byte{})
				Expect(err).NotTo(HaveOccurred())
				Expect(h.Length).To(BeZero())
			})

			It("accepts an endpoint of exactly 256 bytes", func() {
				ep := string(make([]byte, 256))
				h, err := frame.NewHeaderV1(ep, []byte("x"))
				Expect(err).NotTo(HaveOccurred())
				Expect(h.EndpointLen).To(Equal(uint8(0))) // all zero bytes → len 256, stored as 0 due to uint8 overflow; but no error
				_ = h
			})

			It("rejects an endpoint longer than 256 bytes", func() {
				ep := string(make([]byte, 257))
				_, err := frame.NewHeaderV1(ep, []byte("x"))
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("EncodeV1 / DecodeV1 round-trip", func() {
			It("round-trips a normal header", func() {
				h, _ := frame.NewHeaderV1("10.0.0.1:443", []byte("payload"))
				buf, err := frame.EncodeV1(h)
				Expect(err).NotTo(HaveOccurred())
				Expect(buf).To(HaveLen(frame.HeaderV1Length))

				h2, err := frame.DecodeV1(buf)
				Expect(err).NotTo(HaveOccurred())
				Expect(h2.Magic).To(Equal(frame.Magic))
				Expect(h2.Version).To(Equal(frame.VersionV1))
				Expect(string(h2.Endpoint[:h2.EndpointLen])).To(Equal("10.0.0.1:443"))
				Expect(h2.Length).To(Equal(uint16(7)))
			})

			It("round-trips FlagPing", func() {
				h, _ := frame.NewHeaderV1("host:1", []byte{})
				h.Flags = frame.FlagPing
				buf, _ := frame.EncodeV1(h)
				h2, err := frame.DecodeV1(buf)
				Expect(err).NotTo(HaveOccurred())
				Expect(h2.Flags & frame.FlagPing).To(Equal(frame.FlagPing))
			})
		})

		Describe("DecodeV1 error cases", func() {
			It("rejects wrong magic", func() {
				h, _ := frame.NewHeaderV1("h:1", []byte{})
				buf, _ := frame.EncodeV1(h)
				buf[0] ^= 0xFF // corrupt magic
				_, err := frame.DecodeV1(buf)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("magic"))
			})

			It("rejects wrong version", func() {
				h, _ := frame.NewHeaderV1("h:1", []byte{})
				buf, _ := frame.EncodeV1(h)
				buf[4] = frame.VersionV2 // overwrite version byte
				_, err := frame.DecodeV1(buf)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("version"))
			})
		})
	})

	Describe("V2", func() {
		Describe("NewHeaderV2", func() {
			It("creates a valid header", func() {
				h := frame.NewHeaderV2(frame.EndpointId(42), []byte("hello"))
				Expect(h.Magic).To(Equal(frame.Magic))
				Expect(h.Version).To(Equal(frame.VersionV2))
				Expect(h.Flags).To(BeZero())
				Expect(h.Length).To(Equal(uint16(5)))
				Expect(h.EndpointId).To(Equal(frame.EndpointId(42)))
			})
		})

		Describe("EncodeV2 / DecodeV2 round-trip", func() {
			It("round-trips a normal header", func() {
				h := frame.NewHeaderV2(frame.EndpointId(0xdeadbeef), []byte("data"))
				buf, err := frame.EncodeV2(h)
				Expect(err).NotTo(HaveOccurred())
				Expect(buf).To(HaveLen(frame.HeaderV2Length))

				h2, err := frame.DecodeV2(buf)
				Expect(err).NotTo(HaveOccurred())
				Expect(*h).To(Equal(*h2))
			})

			It("round-trips FlagPing", func() {
				h := frame.NewHeaderV2(frame.EndpointId(1), []byte{})
				h.Flags = frame.FlagPing
				buf, _ := frame.EncodeV2(h)
				h2, err := frame.DecodeV2(buf)
				Expect(err).NotTo(HaveOccurred())
				Expect(h2.Flags & frame.FlagPing).To(Equal(frame.FlagPing))
			})
		})

		Describe("DecodeV2 error cases", func() {
			It("rejects wrong magic", func() {
				h := frame.NewHeaderV2(frame.EndpointId(1), []byte{})
				buf, _ := frame.EncodeV2(h)
				buf[0] ^= 0xFF
				_, err := frame.DecodeV2(buf)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("magic"))
			})

			It("rejects wrong version", func() {
				h := frame.NewHeaderV2(frame.EndpointId(1), []byte{})
				buf, _ := frame.EncodeV2(h)
				buf[4] = frame.VersionV1
				_, err := frame.DecodeV2(buf)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("version"))
			})
		})
	})

	Describe("Cross-version decode rejection", func() {
		It("rejects decoding a V1 frame as V2", func() {
			h, _ := frame.NewHeaderV1("h:1", []byte("x"))
			buf, _ := frame.EncodeV1(h)
			_, err := frame.DecodeV2(buf)
			Expect(err).To(HaveOccurred())
		})

		It("rejects decoding a V2 frame as V1", func() {
			h := frame.NewHeaderV2(frame.EndpointId(1), []byte("x"))
			buf, _ := frame.EncodeV2(h)
			_, err := frame.DecodeV1(buf)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("EndpointId", func() {
		It("formats as decimal string", func() {
			Expect(frame.EndpointId(42).String()).To(Equal("42"))
			Expect(frame.EndpointId(0).String()).To(Equal("0"))
		})
	})
})
