package schema

import (
	"context"
	"errors"
	"fmt"
	"testing"

	domain "github.com/ClassMesh/classmesh"
)

var benchPrefixes = []string{"http.request", "http.response", "user.profile", "db.query", "app.meta"}

// manyFieldSchema builds a 50-field schema of depth-3 paths sharing prefixes
// (http.request.*, http.response.*, ...), the shape PERF-011 targets.
func manyFieldSchema(b *testing.B) *Stage {
	b.Helper()
	fields := make([]Field, 0, 50)
	for _, p := range benchPrefixes {
		for i := 0; i < 10; i++ {
			fields = append(fields, Field{Path: fmt.Sprintf("%s.f%d", p, i), Required: true, Type: String})
		}
	}
	s, err := New("invalid", fields)
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	return s
}

func validManyFieldRecord() domain.Record {
	root := map[string]any{}
	for _, p := range benchPrefixes {
		leaf := map[string]any{}
		for i := 0; i < 10; i++ {
			leaf[fmt.Sprintf("f%d", i)] = "x"
		}
		nest(root, p, leaf)
	}
	return domain.Record{Kind: domain.KindJSON, Fields: root}
}

func violatingManyFieldRecord() domain.Record {
	root := map[string]any{}
	for _, p := range benchPrefixes {
		leaf := map[string]any{}
		for i := 0; i < 10; i++ {
			if p == "user.profile" && i < 4 {
				leaf[fmt.Sprintf("f%d", i)] = float64(i)
				continue
			}
			leaf[fmt.Sprintf("f%d", i)] = "x"
		}
		nest(root, p, leaf)
	}
	delete(root["http"].(map[string]any), "request")
	return domain.Record{Kind: domain.KindJSON, Fields: root}
}

func nest(root map[string]any, prefix string, leaf map[string]any) {
	a, b, _ := splitTwo(prefix)
	inner, ok := root[a].(map[string]any)
	if !ok {
		inner = map[string]any{}
		root[a] = inner
	}
	inner[b] = leaf
}

func splitTwo(prefix string) (a, b string, ok bool) {
	for i := 0; i < len(prefix); i++ {
		if prefix[i] == '.' {
			return prefix[:i], prefix[i+1:], true
		}
	}
	return prefix, "", false
}

func benchClassify(b *testing.B, s *Stage, r domain.Record) {
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Classify(ctx, r)
		if err != nil && !errors.Is(err, domain.ErrUnclassified) {
			b.Fatal(err)
		}
	}
}

func BenchmarkClassifyManyFieldsValid(b *testing.B) {
	benchClassify(b, manyFieldSchema(b), validManyFieldRecord())
}

func BenchmarkClassifyManyFieldsViolating(b *testing.B) {
	benchClassify(b, manyFieldSchema(b), violatingManyFieldRecord())
}

// smallSchema is the 1-3 field shape of today's real schemas; the trie must not
// regress it.
func smallSchema(b *testing.B) *Stage {
	b.Helper()
	s, err := New("invalid", []Field{
		{Path: "level", Required: true, Type: String},
		{Path: "status", Type: Number},
		{Path: "user.id", Required: true, Type: String},
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	return s
}

func BenchmarkClassifySmallValid(b *testing.B) {
	r := domain.Record{Kind: domain.KindJSON, Fields: map[string]any{
		"level":  "error",
		"status": float64(500),
		"user":   map[string]any{"id": "u1"},
	}}
	benchClassify(b, smallSchema(b), r)
}

func BenchmarkClassifySmallViolating(b *testing.B) {
	r := domain.Record{Kind: domain.KindJSON, Fields: map[string]any{
		"status": "oops",
	}}
	benchClassify(b, smallSchema(b), r)
}
