//go:build ignore

// genprod writes n production-shaped log lines to stdout (deterministic by
// seed): access logs, probes, app chatter, payments, auth events, warns,
// errors, and a slice nothing matches. go run examples/genprod.go -n 1000000
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"
)

func main() {
	n := flag.Int("n", 1_000_000, "number of log lines to generate")
	seed := flag.Int64("seed", 42, "PRNG seed; same seed, same output")
	flag.Parse()

	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	r := rand.New(rand.NewSource(*seed))
	ts := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)

	paths := []string{"/api/v1/orders/%d", "/api/v1/users/%d", "/api/v1/search?q=q%d", "/api/v1/carts/%d/items", "/assets/app.js", "/login"}
	methods := []string{"GET", "GET", "GET", "POST", "PUT"}
	services := []string{"gateway", "orders", "users", "payments", "search", "carts"}
	infos := []string{
		"info service=%s cache miss key=user:%d refill_ms=%d",
		"info service=%s job completed name=reindex-%d duration_ms=%d",
		"info service=%s connection pool size=%d idle=%d",
	}
	odd := []string{
		"tick %d drift %dms",
		"peer gossip state=stable nodes=%d epoch=%d",
		"compaction window advanced segment=%d ratio=0.%d",
	}

	for i := 0; i < *n; i++ {
		ts = ts.Add(time.Duration(r.Intn(4)) * time.Millisecond)
		stamp := ts.Format(time.RFC3339Nano)
		svc := services[r.Intn(len(services))]
		draw := r.Float64()
		switch {
		case draw < 0.28: // http access, 2xx
			fmt.Fprintf(w, "%s info service=%s http request method=%s path=%s status=%d latency_ms=%d ip=10.%d.%d.%d\n",
				stamp, svc, methods[r.Intn(len(methods))], pick(r, paths), 200+[]int{0, 0, 0, 1, 4}[r.Intn(5)], 1+r.Intn(180), r.Intn(256), r.Intn(256), 1+r.Intn(254))
		case draw < 0.50: // probes: pure noise
			if r.Intn(3) == 0 {
				fmt.Fprintf(w, "GET /readiness 200\n")
			} else {
				fmt.Fprintf(w, "GET /healthz 200\n")
			}
		case draw < 0.62: // app info chatter
			f := infos[r.Intn(len(infos))]
			fmt.Fprintf(w, "%s "+f+"\n", stamp, svc, r.Intn(100000), r.Intn(900))
		case draw < 0.70: // payments
			verb := []string{"succeeded", "succeeded", "declined", "failed"}[r.Intn(4)]
			fmt.Fprintf(w, "%s info service=payments payment %s for order %d amount=%d.%02d currency=USD\n",
				stamp, verb, r.Intn(200000), 1+r.Intn(400), r.Intn(100))
		case draw < 0.76: // auth signals
			switch r.Intn(3) {
			case 0:
				fmt.Fprintf(w, "%s blocked: unauthorized token for user %d\n", stamp, r.Intn(9000))
			case 1:
				fmt.Fprintf(w, "%s login failed user=u%d attempts=%d\n", stamp, r.Intn(9000), 1+r.Intn(5))
			default:
				fmt.Fprintf(w, "%s rate limited ip=10.%d.%d.%d window=60s\n", stamp, r.Intn(256), r.Intn(256), 1+r.Intn(254))
			}
		case draw < 0.82: // http client/server errors
			fmt.Fprintf(w, "%s info service=%s http request method=%s path=%s status=%d latency_ms=%d ip=10.%d.%d.%d\n",
				stamp, svc, methods[r.Intn(len(methods))], pick(r, paths), []int{404, 404, 429, 500, 503}[r.Intn(5)], 1+r.Intn(900), r.Intn(256), r.Intn(256), 1+r.Intn(254))
		case draw < 0.88: // warns: the model tier's job
			switch r.Intn(3) {
			case 0:
				fmt.Fprintf(w, "%s warn service=%s slow query duration_ms=%d table=orders_%d\n", stamp, svc, 800+r.Intn(2400), 1+r.Intn(40))
			case 1:
				fmt.Fprintf(w, "%s warn service=%s queue depth=%d threshold=500\n", stamp, svc, 500+r.Intn(2000))
			default:
				fmt.Fprintf(w, "%s warn service=%s retrying request attempt=%d upstream=inventory\n", stamp, svc, 2+r.Intn(4))
			}
		case draw < 0.94: // severe errors
			switch r.Intn(4) {
			case 0:
				fmt.Fprintf(w, "%s error service=%s upstream timeout after %dms trace=%08x\n", stamp, svc, 1000+r.Intn(4000), r.Uint32())
			case 1:
				fmt.Fprintf(w, "%s error service=%s request failed status=503 trace=%08x attempt=%d\n", stamp, svc, r.Uint32(), 1+r.Intn(3))
			case 2:
				fmt.Fprintf(w, "%s error service=%s panic recovered in handler trace=%08x goroutine=%d\n", stamp, svc, r.Uint32(), r.Intn(90000))
			default:
				fmt.Fprintf(w, "%s OOMKilled container=%s-%d restart_count=%d\n", stamp, svc, r.Intn(40), 1+r.Intn(6))
			}
		case draw < 0.97: // business events: matched low-confidence by the model tier
			if r.Intn(2) == 0 {
				fmt.Fprintf(w, "%s shipment %d dispatched from tlv warehouse=%c\n", stamp, r.Intn(999999), 'A'+rune(r.Intn(4)))
			} else {
				fmt.Fprintf(w, "%s inventory sync started region=eu-%d items=%d\n", stamp, 1+r.Intn(3), r.Intn(50000))
			}
		default: // chatter no tier recognizes: exercises the review path
			f := odd[r.Intn(len(odd))]
			fmt.Fprintf(w, "%s "+f+"\n", stamp, r.Intn(100000), 1+r.Intn(500))
		}
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "genprod: %v\n", err)
		os.Exit(1)
	}
}

// pick returns a path template with its id filled in when it has one.
func pick(r *rand.Rand, paths []string) string {
	p := paths[r.Intn(len(paths))]
	if strings.Contains(p, "%d") {
		return fmt.Sprintf(p, r.Intn(90000))
	}
	return p
}
