package mock

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse builds a Stage from a YAML document. Unknown fields are rejected so a
// misspelled key is a clear error rather than a matcher that silently does
// nothing.
func Parse(data []byte) (*Stage, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return New(cfg)
		}
		return nil, fmt.Errorf("mock: parse yaml: %w", err)
	}
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, errors.New("mock: expected a single YAML document")
	}
	return New(cfg)
}

// Load builds a Stage from a YAML file.
func Load(path string) (*Stage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mock: %w", err)
	}
	return Parse(data)
}
