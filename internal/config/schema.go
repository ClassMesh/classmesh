package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ClassMesh/classmesh/schema"
	"gopkg.in/yaml.v3"
)

type schemaFile struct {
	Category string      `yaml:"category"`
	Fields   []fieldSpec `yaml:"fields"`
}

type fieldSpec struct {
	Path     string `yaml:"path"`
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
}

// ParseSchema builds a schema stage from a YAML document.
func ParseSchema(data []byte) (*schema.Stage, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg schemaFile
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return schema.New(cfg.Category, nil)
		}
		return nil, fmt.Errorf("schema: parse yaml: %w", err)
	}
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, errors.New("schema: expected a single YAML document")
	}
	fields := make([]schema.Field, 0, len(cfg.Fields))
	for _, field := range cfg.Fields {
		typ, err := schemaType(field.Type)
		if err != nil {
			return nil, err
		}
		fields = append(fields, schema.Field{Path: field.Path, Required: field.Required, Type: typ})
	}
	return schema.New(cfg.Category, fields)
}

// LoadSchema builds a schema stage from a YAML file.
func LoadSchema(path string) (*schema.Stage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return ParseSchema(data)
}

func schemaType(name string) (schema.Type, error) {
	switch name {
	case "", "any":
		return schema.Any, nil
	case "string":
		return schema.String, nil
	case "number":
		return schema.Number, nil
	case "bool":
		return schema.Bool, nil
	default:
		return schema.Any, fmt.Errorf("schema: unknown field type %q (want any, string, number, or bool)", name)
	}
}
