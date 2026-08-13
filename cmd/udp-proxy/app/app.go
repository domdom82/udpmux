package app

import (
	"context"
	"runtime"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/utils"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"k8s.io/component-base/version/verflag"
)

const Name = "udp-proxy"

var (
	listenAddr   string
	muxAddr      string
	endpointAddr string
	protocol     string
	ping         bool
	pingsize     int
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
				Ping:         ping,
				PingSize:     pingsize,
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
	flags.BoolVarP(&ping, "ping", "i", false, "If set, the udp proxy will only send pings to the mux.")
	flags.IntVarP(&pingsize, "size", "s", 0, "Size of the ping payload to send. Only used if ping is set.")

	return cmd
}

func run(ctx context.Context, log logr.Logger, cfg *config.UdpProxyConfig) error {
	log.Info("config parsed", "config", cfg)
	log.Info("runtime", "numCPU", runtime.NumCPU(), "GOMAXPROCS", runtime.GOMAXPROCS(0))

	err := cfg.Validate()
	if err != nil {
		return err
	}

	wg := errgroup.Group{}
	wg.Go(func() error { return runProxy(ctx, log, cfg) })
	if cfg.Ping {
		wg.Go(func() error { return runPing(ctx, log, cfg) })
	}

	return wg.Wait()
}
