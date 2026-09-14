package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/domdom82/udpmux/pkg/config"
)

func BenchmarkAPIRegisterEndpoints_1k(b *testing.B) {
	addrs := make([]string, 1000)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("10.%d.%d.%d:1194", (i/65536)%256, (i/256)%256, i%256)
	}
	body := strings.Join(addrs, "\n")

	cfg := config.NewUdpMuxConfig(":8080", ":8081", config.ProtocolV2)
	srv := newTestServer(cfg)
	defer srv.Close()

	b.ResetTimer()
	for b.Loop() {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/endpoints/bulk", strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
	b.StopTimer()

	totalForOneBatch := b.Elapsed().Seconds() / float64(b.N)
	b.ReportMetric(totalForOneBatch, "s/1k_endpoints")

	if totalForOneBatch > 1.0 {
		b.Errorf("bulk registering 1k endpoints took %.3fs, want <1s", totalForOneBatch)
	}
}
