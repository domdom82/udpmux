package config

type UdpProxyConfig struct {
	ListenAddr string // The ip:port the udp proxy will listen on.
	MuxAddr    string // The ip:port the udp proxy will forward to.
	Endpoint   string // The ip:port the udp mux will forward to.
	EndpointID uint32 // The endpoint id on the udp mux to forward to.
}
