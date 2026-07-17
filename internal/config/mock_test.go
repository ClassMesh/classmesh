package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh"
)

func TestParseAndLoadMock(t *testing.T) {
	s, err := ParseMock([]byte("matchers:\n  - { contains: [\"payment\"], category: billing, confidence: 0.93 }\ndefault: { category: unknown, confidence: 0.3 }\n"))
	if err != nil {
		t.Fatalf("ParseMock() error = %v", err)
	}
	c, err := s.Classify(context.Background(), classmesh.Record{ID: "r1", Data: []byte("payment declined")})
	if err != nil || c.Category != "billing" {
		t.Fatalf("Classify() = %+v, %v, want billing", c, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "mock.yml")
	if err := os.WriteFile(path, []byte("matchers:\n  - { contains: [\"x\"], category: c, confidence: 0.5 }\n"), 0o600); err != nil {
		t.Fatalf("write mock file: %v", err)
	}
	if _, err := LoadMock(path); err != nil {
		t.Fatalf("LoadMock() error = %v", err)
	}
	if _, err := LoadMock(filepath.Join(dir, "missing.yml")); err == nil {
		t.Fatalf("LoadMock(missing) error = nil, want a read error")
	}
}

func TestParseMockRejectsEmptyDocument(t *testing.T) {
	_, err := ParseMock([]byte(""))
	if err == nil || !strings.Contains(err.Error(), "at least one matcher or a default") {
		t.Fatalf("ParseMock(empty) error = %v, want the New validation error", err)
	}
}

func TestParseMockRejectsUnknownKey(t *testing.T) {
	if _, err := ParseMock([]byte("matchers:\n  - { containz: [\"x\"], category: c, confidence: 0.5 }\n")); err == nil {
		t.Fatalf("ParseMock() error = nil, want a rejection of the misspelled key")
	}
}

func TestParseMockRejectsTrailingDocument(t *testing.T) {
	_, err := ParseMock([]byte("default: { category: c, confidence: 0.5 }\n---\ndefault: { category: d, confidence: 0.5 }\n"))
	if err == nil || !strings.Contains(err.Error(), "single YAML document") {
		t.Fatalf("ParseMock() error = %v, want a trailing-document rejection", err)
	}
}
