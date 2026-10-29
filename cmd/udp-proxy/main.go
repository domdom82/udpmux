package main

import (
	"fmt"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/domdom82/udpmux/cmd/udp-proxy/app"
)

func main() {
	if err := app.NewCommand().ExecuteContext(signals.SetupSignalHandler()); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
