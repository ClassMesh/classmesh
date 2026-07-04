package main

import (
	"os"
	"testing"
)

const (
	benchModel = "models/model.onnx"
	benchVocab = "models/vocab.txt"
	benchText  = "The classification cascade routes each record to the best matching category."
)

// newTestEmbedder builds an embedder or skips the test when the model is absent.
func newTestEmbedder(tb testing.TB) *Embedder {
	if _, err := os.Stat(benchModel); err != nil {
		tb.Skipf("model not present: %v", err)
	}
	emb, err := NewEmbedder(benchModel, benchVocab)
	if err != nil {
		tb.Fatalf("NewEmbedder: %v", err)
	}
	return emb
}

// TestSeparation checks that related sentences score higher than unrelated ones.
func TestSeparation(t *testing.T) {
	emb := newTestEmbedder(t)
	catA := emb.Embed("A kitten napped on the sunny windowsill.")
	catB := emb.Embed("The cat slept in the warm sunlight by the window.")
	finA := emb.Embed("Quarterly revenue beat analyst estimates.")
	finB := emb.Embed("The firm posted strong earnings for the quarter.")

	simRelated := Cosine(catA, catB)
	finRelated := Cosine(finA, finB)
	unrelated := Cosine(catA, finA)

	t.Logf("cat~cat=%.4f fin~fin=%.4f cat~fin=%.4f", simRelated, finRelated, unrelated)
	if simRelated <= unrelated || finRelated <= unrelated {
		t.Fatalf("related pairs did not separate from unrelated: %.4f/%.4f vs %.4f",
			simRelated, finRelated, unrelated)
	}
}

// BenchmarkEmbed measures single-record, single-thread embedding latency.
func BenchmarkEmbed(b *testing.B) {
	emb := newTestEmbedder(b)
	emb.Embed(benchText)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		emb.Embed(benchText)
	}
}
