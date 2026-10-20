// Command godelta renders Go function changes as a scoped Mermaid call graph.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/leonhfr/godelta/internal/cli"
)

// version is set to the release tag by GoReleaser.
var version = "dev"

func main() {
	os.Exit(execute())
}

func execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := cli.New(cli.WithVersion(version)).Run(ctx, os.Args)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "godelta: %v\n", err)
		return 1
	}

	return 0
}
