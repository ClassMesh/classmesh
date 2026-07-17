package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ClassMesh/classmesh/internal/mockstage"
	"gopkg.in/yaml.v3"
)

type mockFile struct {
	Matchers []mockMatcher `yaml:"matchers"`
	Default  *mockOutcome  `yaml:"default"`
}

type mockMatcher struct {
	Contains   []string `yaml:"contains"`
	Category   string   `yaml:"category"`
	Confidence float64  `yaml:"confidence"`
}

type mockOutcome struct {
	Category   string  `yaml:"category"`
	Confidence float64 `yaml:"confidence"`
}

func (f mockFile) stageConfig() mockstage.Config {
	matchers := make([]mockstage.Matcher, len(f.Matchers))
	for i, matcher := range f.Matchers {
		matchers[i] = mockstage.Matcher{
			Contains: matcher.Contains, Category: matcher.Category, Confidence: matcher.Confidence,
		}
	}
	var fallback *mockstage.Outcome
	if f.Default != nil {
		fallback = &mockstage.Outcome{Category: f.Default.Category, Confidence: f.Default.Confidence}
	}
	return mockstage.Config{Matchers: matchers, Default: fallback}
}

// ParseMock builds a mock stage from a YAML document.
func ParseMock(data []byte) (*mockstage.Stage, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg mockFile
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return mockstage.New(cfg.stageConfig())
		}
		return nil, fmt.Errorf("mock: parse yaml: %w", err)
	}
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, errors.New("mock: expected a single YAML document")
	}
	return mockstage.New(cfg.stageConfig())
}

// LoadMock builds a mock stage from a YAML file.
func LoadMock(path string) (*mockstage.Stage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mock: %w", err)
	}
	return ParseMock(data)
}
