package app

import (
	"context"
	"runtime"

	"github.com/domdom82/udpmux/pkg/config"
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
			}
			return run(ctx, log, cfg)
		},
	}

	flags := cmd.Flags()
	verflag.AddFlags(flags)
	flags.StringVarP(&listenAddr, "listenAddr", "l", ":7070", "Local address to listen on")
	flags.StringVarP(&muxAddr, "muxaddr", "m", "", "UDP Mux address in the format <host>:<port>")
	flags.StringVarP(&endpoint, "endpoint", "e", "", "Endpoint address in the format <host>:<port>")
	return cmd
}

func run(ctx context.Context, log logr.Logger, cfg *config.UdpProxyConfig) error {
	log.Info("config parsed", "config", cfg)
	log.Info("runtime", "numCPU", runtime.NumCPU(), "GOMAXPROCS", runtime.GOMAXPROCS(0))

	p := proxy.NewProxy(cfg.ListenAddr, cfg.MuxAddr, runtime.GOMAXPROCS(0))

	//TODO: add hooks for wrapping / unwrapping of UDPM frames

	return p.ListenAndServe(ctx, log)
}
