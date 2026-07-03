package schema

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/fieldpath"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

func mustNew(t *testing.T, fields ...Field) *Stage {
	t.Helper()
	s, err := New("invalid", fields)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return s
}

func event(fields map[string]any) domain.Record {
	return domain.Record{Kind: domain.KindJSON, Fields: fields}
}

func TestValidRecordEscalates(t *testing.T) {
	s := mustNew(t, Field{Path: "level", Required: true, Type: String}, Field{Path: "status", Type: Number})
	if _, err := s.Classify(context.Background(), event(map[string]any{"level": "error", "status": float64(500)})); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("valid record error = %v, want ErrUnclassified", err)
	}
	if _, err := s.Classify(context.Background(), event(map[string]any{"level": "info"})); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("optional field absent error = %v, want ErrUnclassified", err)
	}
}

func TestMissingRequiredFieldIsClassified(t *testing.T) {
	s := mustNew(t, Field{Path: "level", Required: true, Type: String})
	c, err := s.Classify(context.Background(), event(map[string]any{"other": "x"}))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if c.Category != "invalid" || c.Stage != Name || c.Confidence != 1 {
		t.Fatalf("Classify() = %+v, want invalid/schema/1", c)
	}
	if len(c.Reasons) != 1 || c.Reasons[0].Code != "missing" {
		t.Fatalf("Reasons = %+v, want one missing reason", c.Reasons)
	}
}

func TestWrongTypeIsClassified(t *testing.T) {
	s := mustNew(t, Field{Path: "status", Type: Number})
	c, err := s.Classify(context.Background(), event(map[string]any{"status": "500"}))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if c.Category != "invalid" || len(c.Reasons) != 1 || c.Reasons[0].Code != "type" {
		t.Fatalf("Classify() = %+v, want one type violation", c)
	}
}

func TestNumberAcceptsJSONNumber(t *testing.T) {
	s := mustNew(t, Field{Path: "status", Type: Number})
	if _, err := s.Classify(context.Background(), event(map[string]any{"status": json.Number("500")})); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("json.Number should satisfy Number; err = %v", err)
	}
}

func TestMultipleViolations(t *testing.T) {
	s := mustNew(t, Field{Path: "level", Required: true, Type: String}, Field{Path: "status", Type: Number})
	c, err := s.Classify(context.Background(), event(map[string]any{"status": "oops"}))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if len(c.Reasons) != 2 {
		t.Fatalf("Reasons = %+v, want 2 (missing level + wrong-typed status)", c.Reasons)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New("", []Field{{Path: "a"}}); err == nil {
		t.Fatal("empty category should error")
	}
	if _, err := New("invalid", nil); err == nil {
		t.Fatal("no fields should error")
	}
	if _, err := New("invalid", []Field{{Path: ""}}); err == nil {
		t.Fatal("empty field path should error")
	}
}

// trieStage builds a shared-prefix schema that New compiles to the trie path,
// declared so the trie visits fields out of declaration order (http.b after
// user.x in declaration, before it in the walk).
func trieStage(t *testing.T) *Stage {
	t.Helper()
	s := mustNew(t,
		Field{Path: "http.a", Required: true, Type: String},
		Field{Path: "user.x", Required: true, Type: String},
		Field{Path: "http.b", Required: true, Type: Number},
		Field{Path: "http.c", Type: Bool},
	)
	if s.root == nil {
		t.Fatal("shared-prefix schema should compile to the trie path")
	}
	return s
}

func TestTrieValidRecordEscalates(t *testing.T) {
	s := trieStage(t)
	fields := map[string]any{
		"http": map[string]any{"a": "GET", "b": float64(200), "c": true},
		"user": map[string]any{"x": "u1"},
	}
	if _, err := s.Classify(context.Background(), event(fields)); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("valid record error = %v, want ErrUnclassified", err)
	}
}

func TestTrieViolationsInDeclarationOrder(t *testing.T) {
	s := trieStage(t)
	c, err := s.Classify(context.Background(), event(map[string]any{
		"http": map[string]any{"b": "not-a-number", "c": "not-a-bool"},
	}))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	want := []domain.Reason{
		{Code: "missing", Detail: "required field http.a is missing"},
		{Code: "missing", Detail: "required field user.x is missing"},
		{Code: "type", Detail: "field http.b is not a number"},
		{Code: "type", Detail: "field http.c is not a bool"},
	}
	if !reflect.DeepEqual(c.Reasons, want) {
		t.Fatalf("Reasons = %+v, want declaration order %+v", c.Reasons, want)
	}
}

func TestTrieMatchesFlatSemantics(t *testing.T) {
	trie := trieStage(t)
	records := []map[string]any{
		nil,
		{"http": map[string]any{"a": "GET", "b": float64(200), "c": true}, "user": map[string]any{"x": "u1"}},
		{"http": map[string]any{"a": float64(1)}, "user": map[string]any{"x": "u1"}},
		{"http": "not-an-object", "user": map[string]any{"x": "u1"}},
		{"user": map[string]any{"x": true}},
	}
	for i, fields := range records {
		gotC, gotErr := trie.Classify(context.Background(), event(fields))
		wantReasons, wantErr := classifyNaive(trie, fields)
		if gotErr != wantErr && !errors.Is(gotErr, wantErr) {
			t.Fatalf("record %d: err = %v, want %v", i, gotErr, wantErr)
		}
		if !reflect.DeepEqual(gotC.Reasons, wantReasons) {
			t.Fatalf("record %d: Reasons = %+v, want %+v", i, gotC.Reasons, wantReasons)
		}
	}
}

// classifyNaive reimplements the documented flat semantics — one independent
// lookup per field, violations in declaration order — as the oracle the trie
// walk must match.
func classifyNaive(s *Stage, fieldsMap map[string]any) ([]domain.Reason, error) {
	var out []domain.Reason
	for i := range s.fields {
		f := &s.fields[i]
		v, ok := fieldpath.Lookup(fieldsMap, f.segments)
		if !ok {
			if f.required {
				out = append(out, domain.Reason{Code: "missing", Detail: s.details[i].missing})
			}
			continue
		}
		if f.typ != Any && !matchesType(v, f.typ) {
			out = append(out, domain.Reason{Code: "type", Detail: s.details[i].badType})
		}
	}
	if out == nil {
		return nil, stage.ErrUnclassified
	}
	return out, nil
}

func TestTrieNilFieldsReportsRequiredInDeclarationOrder(t *testing.T) {
	s := trieStage(t)
	c, err := s.Classify(context.Background(), event(nil))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	want := []domain.Reason{
		{Code: "missing", Detail: "required field http.a is missing"},
		{Code: "missing", Detail: "required field user.x is missing"},
		{Code: "missing", Detail: "required field http.b is missing"},
	}
	if !reflect.DeepEqual(c.Reasons, want) {
		t.Fatalf("Reasons = %+v, want %+v", c.Reasons, want)
	}
}

func TestHonorsCancellation(t *testing.T) {
	s := mustNew(t, Field{Path: "level", Required: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Classify(ctx, event(nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Classify() error = %v, want context.Canceled", err)
	}
}
