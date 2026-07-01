package schema

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the YAML shape of a schema stage: a quarantine category and the
// field constraints valid records must satisfy.
type Config struct {
	Category string      `yaml:"category"`
	Fields   []FieldSpec `yaml:"fields"`
}

// FieldSpec is one field constraint as written in YAML. Type is a name (any,
// string, number, bool) rather than the internal enum.
type FieldSpec struct {
	Path     string `yaml:"path"`
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
}

// Parse builds a Stage from a YAML document. Unknown fields are rejected so a
// misspelled key is a clear error rather than a constraint that silently does
// nothing.
func Parse(data []byte) (*Stage, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return New(cfg.Category, nil)
		}
		return nil, fmt.Errorf("schema: parse yaml: %w", err)
	}
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, errors.New("schema: expected a single YAML document")
	}
	fields := make([]Field, 0, len(cfg.Fields))
	for _, f := range cfg.Fields {
		typ, terr := typeFromString(f.Type)
		if terr != nil {
			return nil, terr
		}
		fields = append(fields, Field{Path: f.Path, Required: f.Required, Type: typ})
	}
	return New(cfg.Category, fields)
}

// Load builds a Stage from a YAML file.
func Load(path string) (*Stage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return Parse(data)
}

func typeFromString(name string) (Type, error) {
	switch name {
	case "", "any":
		return Any, nil
	case "string":
		return String, nil
	case "number":
		return Number, nil
	case "bool":
		return Bool, nil
	default:
		return Any, fmt.Errorf("schema: unknown field type %q (want any, string, number, or bool)", name)
	}
}
