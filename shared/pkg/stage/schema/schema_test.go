package schema

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
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

func TestHonorsCancellation(t *testing.T) {
	s := mustNew(t, Field{Path: "level", Required: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Classify(ctx, event(nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Classify() error = %v, want context.Canceled", err)
	}
}
