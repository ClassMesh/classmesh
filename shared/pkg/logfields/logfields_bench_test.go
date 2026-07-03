package logfields

import "testing"

func BenchmarkParseMiss(b *testing.B) {
	line := []byte("just some random unstructured text with no fields here")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if Parse(line) != nil {
			b.Fatal("expected nil")
		}
	}
}

func BenchmarkParseEnriched(b *testing.B) {
	line := []byte(`2026-06-25T07:53:07Z INFO service=api user=42 latency=12ms msg="request done"`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if Parse(line) == nil {
			b.Fatal("expected fields")
		}
	}
}
