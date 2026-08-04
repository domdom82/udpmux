package config

type UdpProxyConfig struct {
	ListenAddr string // The ip:port the udp proxy will listen on.
	MuxAddr    string // The ip:port the udp proxy will forward to.
	Endpoint   string // The ip:port the udp mux will forward to.
}
