package app

import (
	"context"
	"fmt"
	"runtime"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
)

func runProxy(ctx context.Context, log logr.Logger, cfg *config.UdpProxyConfig) error {
	p := proxy.NewProxy(cfg.ListenAddr, cfg.MuxAddr, runtime.GOMAXPROCS(0))

	switch cfg.Protocol {
	case config.ProtocolV1:
		var wrapV1 = proxy.Hook(func(_ *proxy.ClientSession, data []byte) ([]byte, error) {
			header, err := frame.NewHeaderV1(cfg.EndpointAddr, data)
			if err != nil {
				return data, err
			}
			if cfg.Ping {
				header.Flags = header.Flags | frame.FlagPing
			}
			headerBytes, err := frame.EncodeV1(header)
			if err != nil {
				return data, err
			}

			return append(headerBytes, data...), nil
		})

		p.AddWriteHook(wrapV1)

		var unwrapV1 = proxy.Hook(func(_ *proxy.ClientSession, data []byte) ([]byte, error) {
			header, err := frame.DecodeV1(data[:frame.HeaderV1Length])
			if err != nil {
				return data, err
			}
			endpointStr := string(header.Endpoint[:header.EndpointLen])
			if endpointStr != cfg.EndpointAddr {
				return data, fmt.Errorf("invalid endpoint: '%s' expected '%s'", endpointStr, cfg.EndpointAddr)
			}

			return data[frame.HeaderV1Length : frame.HeaderV1Length+int(header.Length)], nil
		})

		p.AddReadHook(unwrapV1)
	case config.ProtocolV2:
		endpointId := config.EndpointToId(cfg.EndpointAddr)
		var wrapV2 = proxy.Hook(func(_ *proxy.ClientSession, data []byte) ([]byte, error) {
			header := frame.NewHeaderV2(endpointId, data)
			if cfg.Ping {
				header.Flags = header.Flags | frame.FlagPing
			}
			headerBytes, err := frame.EncodeV2(header)
			if err != nil {
				return data, err
			}

			return append(headerBytes, data...), nil
		})

		p.AddWriteHook(wrapV2)

		var unwrapV2 = proxy.Hook(func(_ *proxy.ClientSession, data []byte) ([]byte, error) {
			header, err := frame.DecodeV2(data[:frame.HeaderV2Length])
			if err != nil {
				return data, err
			}
			if header.EndpointId != endpointId {
				return data, fmt.Errorf("invalid endpoint id: '%d' expected '%d'", header.EndpointId, endpointId)
			}

			return data[frame.HeaderV2Length : frame.HeaderV2Length+int(header.Length)], nil
		})

		p.AddReadHook(unwrapV2)
	}

	return p.ListenAndServe(ctx, log)
}
