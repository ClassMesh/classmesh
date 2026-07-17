package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/schema"
)

func schemaEvent(fields map[string]any) classmesh.Record {
	return classmesh.Record{Kind: classmesh.KindJSON, Fields: fields}
}

func TestParseQuarantinesMalformed(t *testing.T) {
	s, err := ParseSchema([]byte("category: bad\nfields:\n  - { path: user.id, required: true, type: string }\n  - { path: amount, type: number }\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	c, err := s.Classify(context.Background(), schemaEvent(map[string]any{"amount": "not-a-number"}))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if c.Category != "bad" {
		t.Fatalf("category = %q, want bad", c.Category)
	}
	if len(c.Reasons) != 2 {
		t.Fatalf("reasons = %+v, want one per violation (missing user.id, wrong type amount)", c.Reasons)
	}
}

func TestParseValidRecordEscalates(t *testing.T) {
	s, err := ParseSchema([]byte("category: bad\nfields:\n  - { path: level, required: true, type: string }\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, err := s.Classify(context.Background(), schemaEvent(map[string]any{"level": "error"})); !errors.Is(err, classmesh.ErrUnclassified) {
		t.Fatalf("valid record error = %v, want ErrUnclassified", err)
	}
}

func TestParseTypeNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		want schema.Type
	}{{"", schema.Any}, {"any", schema.Any}, {"string", schema.String}, {"number", schema.Number}, {"bool", schema.Bool}} {
		got, err := schemaType(tc.name)
		if err != nil {
			t.Fatalf("typeFromString(%q) error = %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("typeFromString(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseRejectsUnknownType(t *testing.T) {
	_, err := ParseSchema([]byte("category: bad\nfields:\n  - { path: x, type: integer }\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown field type") {
		t.Fatalf("Parse() error = %v, want an unknown-type error", err)
	}
}

func TestParseRejectsUnknownKey(t *testing.T) {
	_, err := ParseSchema([]byte("category: bad\nfields:\n  - { path: x, requird: true }\n"))
	if err == nil {
		t.Fatalf("Parse() error = nil, want a rejection of the misspelled key")
	}
}

func TestParseRejectsMissingCategory(t *testing.T) {
	if _, err := ParseSchema([]byte("fields:\n  - { path: x }\n")); err == nil {
		t.Fatalf("Parse() error = nil, want category-required")
	}
}

func TestParseRejectsNoFields(t *testing.T) {
	if _, err := ParseSchema([]byte("category: bad\n")); err == nil {
		t.Fatalf("Parse() error = nil, want at-least-one-field")
	}
}

func TestParseRejectsEmptyFieldPath(t *testing.T) {
	if _, err := ParseSchema([]byte("category: bad\nfields:\n  - { required: true }\n")); err == nil {
		t.Fatalf("Parse() error = nil, want field-path-required")
	}
}

func TestParseRejectsTrailingDocument(t *testing.T) {
	_, err := ParseSchema([]byte("category: bad\nfields:\n  - { path: id }\n---\ncategory: other\n"))
	if err == nil || !strings.Contains(err.Error(), "single YAML document") {
		t.Fatalf("Parse() error = %v, want a trailing-document rejection", err)
	}
}

func TestLoadReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.yml")
	if err := os.WriteFile(path, []byte("category: bad\nfields:\n  - { path: id, required: true }\n"), 0o600); err != nil {
		t.Fatalf("write schema file: %v", err)
	}
	if _, err := LoadSchema(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, err := LoadSchema(filepath.Join(dir, "missing.yml")); err == nil {
		t.Fatalf("Load(missing) error = nil, want a read error")
	}
}
