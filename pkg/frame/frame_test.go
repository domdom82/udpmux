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
	Describe("NewHeader", func() {
		It("creates a valid control header", func() {
			h := frame.NewHeader(frame.FlagProxyHello, 14)
			Expect(h.Magic).To(Equal(frame.Magic))
			Expect(h.Flags).To(Equal(frame.FlagProxyHello))
			Expect(h.Length).To(Equal(uint16(14)))
		})

		It("creates a zero-payload header for RESET", func() {
			h := frame.NewHeader(frame.FlagReset, 0)
			Expect(h.Length).To(BeZero())
		})
	})

	Describe("Encode / Decode round-trip", func() {
		It("round-trips FlagProxyHello", func() {
			h := frame.NewHeader(frame.FlagProxyHello, 13)
			buf, err := frame.Encode(h)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf).To(HaveLen(frame.HeaderLength))

			h2, err := frame.Decode(buf)
			Expect(err).NotTo(HaveOccurred())
			Expect(h2.Flags).To(Equal(frame.FlagProxyHello))
			Expect(h2.Length).To(Equal(uint16(13)))
		})

		It("round-trips FlagMuxHello with no payload", func() {
			h := frame.NewHeader(frame.FlagMuxHello, 0)
			buf, _ := frame.Encode(h)
			h2, err := frame.Decode(buf)
			Expect(err).NotTo(HaveOccurred())
			Expect(h2.Flags & frame.FlagMuxHello).To(Equal(frame.FlagMuxHello))
			Expect(h2.Length).To(BeZero())
		})

		It("round-trips FlagKeepAlive with no payload", func() {
			h := frame.NewHeader(frame.FlagKeepAlive, 0)
			buf, _ := frame.Encode(h)
			h2, err := frame.Decode(buf)
			Expect(err).NotTo(HaveOccurred())
			Expect(h2.Flags & frame.FlagKeepAlive).To(Equal(frame.FlagKeepAlive))
		})

		It("round-trips FlagReset with no payload", func() {
			h := frame.NewHeader(frame.FlagReset, 0)
			buf, _ := frame.Encode(h)
			h2, err := frame.Decode(buf)
			Expect(err).NotTo(HaveOccurred())
			Expect(h2.Flags & frame.FlagReset).To(Equal(frame.FlagReset))
		})

		It("round-trips FlagPing with no payload", func() {
			h := frame.NewHeader(frame.FlagPing, 0)
			buf, _ := frame.Encode(h)
			h2, err := frame.Decode(buf)
			Expect(err).NotTo(HaveOccurred())
			Expect(h2.Flags & frame.FlagPing).To(Equal(frame.FlagPing))
		})
	})

	Describe("Decode error cases", func() {
		It("rejects wrong magic", func() {
			h := frame.NewHeader(frame.FlagMuxHello, 0)
			buf, _ := frame.Encode(h)
			buf[0] ^= 0xFF
			_, err := frame.Decode(buf)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("magic"))
		})
	})

	Describe("Flag constants", func() {
		It("all flags are distinct", func() {
			flags := []uint16{frame.FlagPing, frame.FlagProxyHello, frame.FlagMuxHello, frame.FlagKeepAlive, frame.FlagReset}
			for i := range flags {
				for j := range flags {
					if i != j {
						Expect(flags[i]&flags[j]).To(BeZero(), "flags %d and %d overlap", i, j)
					}
				}
			}
		})
	})
})
