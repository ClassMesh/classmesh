// Command genlogs writes a deterministic, weighted stream of example log
// lines to stdout, so the throughput numbers in the docs can be reproduced
// against a realistic volume instead of the four-line sample:
//
//	go run ./services/cli/cmd/genlogs -n 1000000 > logs-1m.txt
//	classmesh run --rules examples/rules.yml logs-1m.txt > /dev/null
//
// The category mix matches examples/rules.yml: mostly health-check noise,
// some billing and auth signal, and a slice no rule matches so the review
// path sees traffic too. Same seed, same output.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
)

func main() {
	n := flag.Int("n", 1_000_000, "number of log lines to generate")
	seed := flag.Int64("seed", 42, "PRNG seed; same seed, same output")
	flag.Parse()

	if err := gen(os.Stdout, *n, *seed); err != nil {
		fmt.Fprintf(os.Stderr, "genlogs: %v\n", err)
		os.Exit(1)
	}
}

// gen writes n weighted log lines to w using a PRNG seeded with seed.
func gen(w io.Writer, n int, seed int64) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	r := rand.New(rand.NewSource(seed))
	for i := 0; i < n; i++ {
		if _, err := bw.WriteString(line(r.Float64(), i)); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// line maps a draw in [0,1) to the i-th log line. The thresholds are the
// traffic mix: 70% noise, 15% billing, 10% auth, 5% unmatched.
func line(draw float64, i int) string {
	switch {
	case draw < 0.70:
		return "GET /healthz 200"
	case draw < 0.85:
		return fmt.Sprintf("payment declined for order %d", i%100000)
	case draw < 0.95:
		return fmt.Sprintf("blocked: unauthorized token for user %d", i%5000)
	default:
		return fmt.Sprintf("shipment %d dispatched from tlv", i)
	}
}
