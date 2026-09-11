package frame

import (
	"testing"
)

var (
	benchEndpoint = "10.128.0.1:1194"
	benchPayload  = make([]byte, 1400) // typical VPN MTU payload
)

func BenchmarkNewHeaderV1(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = NewHeaderV1(benchEndpoint, benchPayload)
	}
}

func BenchmarkEncodeV1(b *testing.B) {
	h, _ := NewHeaderV1(benchEndpoint, benchPayload)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = EncodeV1(h)
	}
}

func BenchmarkDecodeV1(b *testing.B) {
	h, _ := NewHeaderV1(benchEndpoint, benchPayload)
	buf, _ := EncodeV1(h)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = DecodeV1(buf)
	}
}

func BenchmarkEncodeDecodeV1RoundTrip(b *testing.B) {
	h, _ := NewHeaderV1(benchEndpoint, benchPayload)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf, _ := EncodeV1(h)
		_, _ = DecodeV1(buf)
	}
}

func BenchmarkNewHeaderV2(b *testing.B) {
	id := EndpointId(0xdeadbeefcafe1234)
	b.ReportAllocs()
	for b.Loop() {
		_ = NewHeaderV2(id, benchPayload)
	}
}

func BenchmarkEncodeV2(b *testing.B) {
	h := NewHeaderV2(EndpointId(0xdeadbeefcafe1234), benchPayload)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = EncodeV2(h)
	}
}

func BenchmarkDecodeV2(b *testing.B) {
	h := NewHeaderV2(EndpointId(0xdeadbeefcafe1234), benchPayload)
	buf, _ := EncodeV2(h)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = DecodeV2(buf)
	}
}

func BenchmarkEncodeDecodeV2RoundTrip(b *testing.B) {
	h := NewHeaderV2(EndpointId(0xdeadbeefcafe1234), benchPayload)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf, _ := EncodeV2(h)
		_, _ = DecodeV2(buf)
	}
}
