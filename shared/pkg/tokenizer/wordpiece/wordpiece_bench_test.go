package wordpiece

import "testing"

// BenchmarkEncode measures a typical short log-line encode end to end:
// basic tokenization plus greedy subword segmentation plus special tokens.
func BenchmarkEncode(b *testing.B) {
	tok, err := New(vocabMap(bertVocab))
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	const line = "UNwantéd, running unwanted runn"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tok.Encode(line)
	}
}
