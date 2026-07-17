// Command classmesh is the ClassMesh CLI: a classification cascade pipeline
// in one binary. See `classmesh run --help`.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ClassMesh/classmesh/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err := cli.Run(ctx, os.Args[1:], cli.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	if err != nil {
		fmt.Fprintf(os.Stderr, "classmesh: %v\n", err)
		return 1
	}
	return 0
}
