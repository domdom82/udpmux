package app

import (
	"context"
	"runtime"
	"time"

	"github.com/domdom82/udpmux/pkg/config"
	"github.com/domdom82/udpmux/pkg/proxy"
	"github.com/domdom82/udpmux/pkg/utils"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"k8s.io/component-base/version/verflag"
)

const Name = "udp-mux"

var (
	listenAddr        string
	apiListenAddr     string
	sessionTimeout    time.Duration
	keepaliveIdle     time.Duration
	keepaliveInterval time.Duration
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
			cfg := config.NewUdpMuxConfig(listenAddr, apiListenAddr, sessionTimeout, keepaliveIdle, keepaliveInterval)
			return run(ctx, log, cfg)
		},
	}

	flags := cmd.Flags()
	verflag.AddFlags(flags)
	flags.StringVarP(&listenAddr, "listenAddr", "l", ":8080", "Local address to listen on for UDP traffic")
	flags.StringVarP(&apiListenAddr, "apiListenAddr", "a", ":8081", "Local address to listen on for API traffic")
	flags.DurationVar(&sessionTimeout, "sessionTimeout", 30*time.Second, "Idle duration before a session is garbage-collected")
	flags.DurationVar(&keepaliveIdle, "keepaliveIdle", 20*time.Second, "Idle duration before sending KEEPALIVE toward the proxy")
	flags.DurationVar(&keepaliveInterval, "keepaliveInterval", 10*time.Second, "Interval between subsequent KEEPALIVE packets while idle")
	return cmd
}

func run(ctx context.Context, log logr.Logger, cfg *config.UdpMuxConfig) error {
	log.Info("config parsed", "config", cfg)
	log.Info("runtime", "numCPU", runtime.NumCPU(), "GOMAXPROCS", runtime.GOMAXPROCS(0))

	err := cfg.Validate()
	if err != nil {
		return err
	}

	p := proxy.NewProxy(cfg.ListenAddr, "", runtime.GOMAXPROCS(0), cfg.SessionTimeout)

	wg := errgroup.Group{}
	wg.Go(func() error { return runProxy(ctx, log, cfg, p) })
	wg.Go(func() error { return runHTTPServer(ctx, log, cfg, p) })

	return wg.Wait()
}
