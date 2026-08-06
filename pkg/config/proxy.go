package config

import "hash/fnv"

const (
	ProtocolV1 = "v1"
	ProtocolV2 = "v2"
)

type UdpProxyConfig struct {
	ListenAddr string // The ip:port the udp proxy will listen on.
	MuxAddr    string // The ip:port the udp proxy will forward to.
	Endpoint   string // The ip:port the udp mux will forward to.
	Protocol   string // The protocol version to use
}

func EndpointToId(endpoint string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(endpoint))
	return h.Sum32()
}
