package config_test

import (
	"fmt"
	"testing"

	"github.com/domdom82/udpmux/pkg/config"
)

func BenchmarkRegisterEndpoints_10k(b *testing.B) {
	addrs := make([]string, 10000)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("10.%d.%d.%d:1194", (i/65536)%256, (i/256)%256, i%256)
	}
	b.ResetTimer()
	for b.Loop() {
		cfg := config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV2)
		cfg.RegisterEndpoints(addrs)
	}
	b.StopTimer()

	totalForOneBatch := b.Elapsed().Seconds() / float64(b.N)
	b.ReportMetric(totalForOneBatch, "s/10k_endpoints")

	if totalForOneBatch > 1.0 {
		b.Errorf("registering 10k endpoints took %.3fs, want <1s", totalForOneBatch)
	}
}
