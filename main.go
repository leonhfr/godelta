// Command godelta renders Go function changes as a scoped Mermaid call graph.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/leonhfr/godelta/internal/cli"
)

func main() {
	os.Exit(execute())
}

func execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dir, err := os.Getwd()
	if err == nil {
		err = cli.Run(ctx, dir, os.Args[1:], os.Stdout, os.Stderr)
	}

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "godelta: %v\n", err)
		return 1
	}

	return 0
}
