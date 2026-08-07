package app

import (
	"context"
	"fmt"
	"runtime"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/frame"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/domdom82/udpmux/pkg/utils"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"k8s.io/component-base/version/verflag"
)

const Name = "udp-proxy"

var (
	listenAddr   string
	muxAddr      string
	endpointAddr string
	protocol     string
)

// NewCommand creates a new cobra.Command for running udp-proxy.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   Name,
		Short: "Launch the " + Name,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			log, err := utils.InitRun(cmd, Name)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg := &config.UdpProxyConfig{
				ListenAddr:   listenAddr,
				MuxAddr:      muxAddr,
				EndpointAddr: endpointAddr,
				Protocol:     protocol,
			}
			return run(ctx, log, cfg)
		},
	}

	flags := cmd.Flags()
	verflag.AddFlags(flags)
	flags.StringVarP(&listenAddr, "listenAddr", "l", ":7070", "Local address to listen on")
	flags.StringVarP(&muxAddr, "muxAddr", "m", "", "UDP Mux address in the format <host>:<port>")
	flags.StringVarP(&endpointAddr, "endpointAddr", "e", "", "Endpoint address in the format <host>:<port>")
	flags.StringVarP(&protocol, "protocol", "p", "v1", "UDPM Protocol version to use (v1 or v2)")
	return cmd
}

func run(ctx context.Context, log logr.Logger, cfg *config.UdpProxyConfig) error {
	log.Info("config parsed", "config", cfg)
	log.Info("runtime", "numCPU", runtime.NumCPU(), "GOMAXPROCS", runtime.GOMAXPROCS(0))

	err := cfg.Validate()
	if err != nil {
		return err
	}

	p := proxy.NewProxy(cfg.ListenAddr, cfg.MuxAddr, runtime.GOMAXPROCS(0))

	switch cfg.Protocol {
	case config.ProtocolV1:
		var wrapV1 = proxy.Hook(func(_ *proxy.ClientSession, data []byte) ([]byte, error) {
			header, err := frame.NewHeaderV1(cfg.EndpointAddr, data)
			if err != nil {
				return data, err
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
