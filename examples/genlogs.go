//go:build ignore

// genlogs writes n weighted example log lines to stdout (deterministic by
// seed, mix matching rules.yml): go run examples/genlogs.go -n 1000000
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand"
	"os"
)

func main() {
	n := flag.Int("n", 1_000_000, "number of log lines to generate")
	seed := flag.Int64("seed", 42, "PRNG seed; same seed, same output")
	flag.Parse()

	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	r := rand.New(rand.NewSource(*seed))
	for i := 0; i < *n; i++ {
		draw := r.Float64()
		switch {
		case draw < 0.70: // noise
			fmt.Fprintln(w, "GET /healthz 200")
		case draw < 0.85: // billing
			fmt.Fprintf(w, "payment declined for order %d\n", i%100000)
		case draw < 0.95: // auth
			fmt.Fprintf(w, "blocked: unauthorized token for user %d\n", i%5000)
		default: // no rule matches; exercises the review path
			fmt.Fprintf(w, "shipment %d dispatched from tlv\n", i)
		}
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "genlogs: %v\n", err)
		os.Exit(1)
	}
}
