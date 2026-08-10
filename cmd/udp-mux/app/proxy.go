package app

import (
	"context"
	"fmt"
	"net"
	"runtime"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/go-logr/logr"
)

func runProxy(ctx context.Context, log logr.Logger, cfg *config.UdpMuxConfig) error {
	p := proxy.NewProxy(cfg.ListenAddr, "", runtime.GOMAXPROCS(0))

	var unwrap = proxy.Hook(func(s *proxy.ClientSession, data []byte) ([]byte, error) {
		var (
			headerV2    *frame.HeaderV2
			headerV1    *frame.HeaderV1
			err         error
			endpointStr string
			endpointId  frame.EndpointId
		)

		// try V2 first
		headerV2, err = frame.DecodeV2(data[:frame.HeaderV2Length])
		if err != nil {
			// fallback to V1
			headerV1, err = frame.DecodeV1(data[:frame.HeaderV1Length])
			if err != nil {
				log.Error(err, "failed to decode header")
				return data, err
			}
			endpointStr = string(headerV1.Endpoint[:headerV1.EndpointLen])
		} else {
			endpointId = headerV2.EndpointId
			endpointStr, err = cfg.GetEndpoint(endpointId)
			if err != nil {
				return data, err
			}
		}

		// Check if we need to connect to the backend
		if s.GetBackendConn() == nil {
			backendAddr, err := net.ResolveUDPAddr("udp", endpointStr)
			if err != nil {
				return data, fmt.Errorf("invalid backend endpoint '%s': %w", endpointStr, err)
			}
			backendConn, err := proxy.DialBackend(backendAddr)
			if err != nil {
				return data, fmt.Errorf("failed to dial backend '%s': %w", endpointStr, err)
			}

			s.SetBackendConn(backendConn)
			s.SetMetaData("endpoint", endpointStr)
		}

		var innerFrame []byte
		switch {
		case headerV1 != nil:
			innerFrame = data[frame.HeaderV1Length : frame.HeaderV1Length+int(headerV1.Length)]
		case headerV2 != nil:
			innerFrame = data[frame.HeaderV2Length : frame.HeaderV2Length+int(headerV2.Length)]
		}
		return innerFrame, nil
	})

	p.AddWriteHook(unwrap)

	var wrap = proxy.Hook(func(s *proxy.ClientSession, data []byte) ([]byte, error) {
		var (
			headerV2    *frame.HeaderV2
			headerV1    *frame.HeaderV1
			headerBytes []byte
			endpointStr string
			endpointId  frame.EndpointId
			err         error
		)
		endpointStr, err = s.GetMetaData("endpoint")
		if err != nil {
			return data, err
		}
		endpointId, err = cfg.GetEndpointId(endpointStr)
		if err != nil {
			// V1 case
			headerV1, err = frame.NewHeaderV1(endpointStr, data)
			if err != nil {
				return data, err
			}
			headerBytes, err = frame.EncodeV1(headerV1)
			if err != nil {
				return data, err
			}
		} else {
			// V2 case
			headerV2 = frame.NewHeaderV2(endpointId, data)
			headerBytes, err = frame.EncodeV2(headerV2)
			if err != nil {
				return data, err
			}
		}
		wrappedFrame := append(headerBytes, data...)
		return wrappedFrame, nil
	})

	p.AddReadHook(wrap)

	return p.ListenAndServe(ctx, log)
}
