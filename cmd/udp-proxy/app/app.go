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
	listenAddr string
	muxAddr    string
	endpoint   string
	endpointId uint32
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
				ListenAddr: listenAddr,
				MuxAddr:    muxAddr,
				Endpoint:   endpoint,
				EndpointID: endpointId,
			}
			return run(ctx, log, cfg)
		},
	}

	flags := cmd.Flags()
	verflag.AddFlags(flags)
	flags.StringVarP(&listenAddr, "listenAddr", "l", ":7070", "Local address to listen on")
	flags.StringVarP(&muxAddr, "muxaddr", "m", "", "UDP Mux address in the format <host>:<port>")
	flags.StringVarP(&endpoint, "endpoint", "e", "", "Endpoint address in the format <host>:<port>")
	flags.Uint32VarP(&endpointId, "endpointId", "i", 0, "Endpoint id in the format <id>")
	return cmd
}

func run(ctx context.Context, log logr.Logger, cfg *config.UdpProxyConfig) error {
	log.Info("config parsed", "config", cfg)
	log.Info("runtime", "numCPU", runtime.NumCPU(), "GOMAXPROCS", runtime.GOMAXPROCS(0))

	p := proxy.NewProxy(cfg.ListenAddr, cfg.MuxAddr, runtime.GOMAXPROCS(0))

	if cfg.Endpoint != "" {
		var wrapV1 = proxy.Hook(func(_ *proxy.ClientSession, data []byte) ([]byte, error) {
			header, err := frame.NewHeaderV1(cfg.Endpoint, data)
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
			if endpointStr != cfg.Endpoint {
				return data, fmt.Errorf("invalid endpoint: '%s' expected '%s'", endpointStr, cfg.Endpoint)
			}

			return data[frame.HeaderV1Length : frame.HeaderV1Length+int(header.Length)], nil
		})

		p.AddReadHook(unwrapV1)
	}

	return p.ListenAndServe(ctx, log)
}
