package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLine(t *testing.T) {
	tests := []struct {
		name string
		draw float64
		i    int
		want string
	}{
		{name: "noise at zero", draw: 0.0, i: 1, want: "GET /healthz 200"},
		{name: "noise below threshold", draw: 0.699, i: 7, want: "GET /healthz 200"},
		{name: "billing at boundary", draw: 0.70, i: 7, want: "payment declined for order 7"},
		{name: "billing wraps order id", draw: 0.80, i: 100001, want: "payment declined for order 1"},
		{name: "auth at boundary", draw: 0.85, i: 42, want: "blocked: unauthorized token for user 42"},
		{name: "auth wraps user id", draw: 0.90, i: 5001, want: "blocked: unauthorized token for user 1"},
		{name: "unmatched at boundary", draw: 0.95, i: 9, want: "shipment 9 dispatched from tlv"},
		{name: "unmatched near one", draw: 0.999, i: 12, want: "shipment 12 dispatched from tlv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := line(tt.draw, tt.i); got != tt.want {
				t.Errorf("line(%v, %d) = %q, want %q", tt.draw, tt.i, got, tt.want)
			}
		})
	}
}

func TestGenDeterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := gen(&a, 10_000, 42); err != nil {
		t.Fatalf("gen: %v", err)
	}
	if err := gen(&b, 10_000, 42); err != nil {
		t.Fatalf("gen: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("same seed produced different output")
	}
}

func TestGenDistribution(t *testing.T) {
	const n = 100_000
	var buf bytes.Buffer
	if err := gen(&buf, n, 42); err != nil {
		t.Fatalf("gen: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("got %d lines, want %d", len(lines), n)
	}

	counts := map[string]int{}
	for _, l := range lines {
		switch {
		case l == "GET /healthz 200":
			counts["noise"]++
		case strings.HasPrefix(l, "payment declined for order "):
			counts["billing"]++
		case strings.HasPrefix(l, "blocked: unauthorized token for user "):
			counts["auth"]++
		case strings.HasPrefix(l, "shipment ") && strings.HasSuffix(l, " dispatched from tlv"):
			counts["unmatched"]++
		default:
			t.Fatalf("unrecognized line: %q", l)
		}
	}

	tests := []struct {
		category string
		want     float64
	}{
		{category: "noise", want: 0.70},
		{category: "billing", want: 0.15},
		{category: "auth", want: 0.10},
		{category: "unmatched", want: 0.05},
	}
	const tolerance = 0.01
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := float64(counts[tt.category]) / n
			if got < tt.want-tolerance || got > tt.want+tolerance {
				t.Errorf("%s fraction = %.4f, want %.2f +/- %.2f", tt.category, got, tt.want, tolerance)
			}
		})
	}
}
