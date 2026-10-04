// Command mocks runs the mock Auth0 and the stub GitHub of the example
// application for a local run, and prints the example's settings for them,
// a line NAME=value each, until an interrupt.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bilus/documentation-portal/examples/portalapp/mocks"
)

func main() {
	auth0 := flag.String("auth0", "127.0.0.1:9100", "address of the mock Auth0")
	github := flag.String("github", "127.0.0.1:9200", "address of the stub GitHub")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := mocks.Serve(ctx, *auth0, *github, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mocks:", err)
		os.Exit(1)
	}
}
