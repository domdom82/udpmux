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

const Name = "udp-mux"

var (
	listenAddr    string
	apiListenAddr string
	protocol      string
)

// NewCommand creates a new cobra.Command for running udp-mux.
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
			cfg := config.NewUdpMuxConfig(listenAddr, apiListenAddr, protocol)
			return run(ctx, log, cfg)
		},
	}

	flags := cmd.Flags()
	verflag.AddFlags(flags)
	flags.StringVarP(&listenAddr, "listenAddr", "l", ":8080", "Local address to listen on for UDP traffic")
	flags.StringVarP(&apiListenAddr, "apiListenAddr", "a", ":8081", "Local address to listen on for API traffic")
	flags.StringVarP(&protocol, "protocol", "p", "v1", "UDPM Protocol version to use (v1 or v2)")
	return cmd
}

func run(ctx context.Context, log logr.Logger, cfg *config.UdpMuxConfig) error {
	log.Info("config parsed", "config", cfg)
	log.Info("runtime", "numCPU", runtime.NumCPU(), "GOMAXPROCS", runtime.GOMAXPROCS(0))

	err := cfg.Validate()
	if err != nil {
		return err
	}

	wg := errgroup.Group{}
	wg.Go(func() error { return runProxy(ctx, log, cfg) })
	wg.Go(func() error { return runHTTPServer(ctx, log, cfg) })

	return wg.Wait()
}
