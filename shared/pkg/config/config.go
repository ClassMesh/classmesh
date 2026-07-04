// Package config parses and validates a declarative cascade configuration: a
// versioned YAML document that declares the input, the ordered stages and their
// gates, and where classified records are routed. It rejects a malformed
// pipeline up front, before any input is opened; building a runnable cascade
// from a Config belongs to the composition root (the CLI), not here.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/ClassMesh/classmesh/shared/pkg/stage"
	"gopkg.in/yaml.v3"
)

// Version is the only config schema version this build understands.
const Version = 1

// Config is a declared cascade.
type Config struct {
	Version int                 `yaml:"version"`
	Input   Input               `yaml:"input"`
	Stages  []StageSpec         `yaml:"stages"`
	Workers int                 `yaml:"workers,omitempty"` // concurrent classification workers; 0/1 = serial
	Routes  map[string]SinkSpec `yaml:"routes,omitempty"`
	Sink    SinkSpec            `yaml:"sink"`
	Review  *SinkSpec           `yaml:"review,omitempty"`
}

// Input names where records come from.
type Input struct {
	Type string `yaml:"type"` // text | jsonl
}

// StageSpec is one stage in the cascade.
type StageSpec struct {
	ID   string   `yaml:"id"`
	Type string   `yaml:"type"`           // rules | schema | mock
	Path string   `yaml:"path,omitempty"` // type-specific config file
	Gate *float64 `yaml:"gate,omitempty"` // per-stage confidence gate in [0, 1]
}

// SinkSpec is a place classified records are written.
type SinkSpec struct {
	Type   string `yaml:"type"`             // jsonl | drop
	Path   string `yaml:"path,omitempty"`   // file path (jsonl)
	Stream string `yaml:"stream,omitempty"` // stdout (jsonl); stderr is diagnostics-only
}

var (
	inputTypes = map[string]bool{"text": true, "jsonl": true}
	stageTypes = map[string]bool{"rules": true, "schema": true, "mock": true}
	sinkTypes  = map[string]bool{"jsonl": true, "drop": true}
	streams    = map[string]bool{"stdout": true}
)

const (
	inputList  = "text, jsonl"
	stageList  = "rules, schema, mock"
	sinkList   = "jsonl, drop"
	streamList = "stdout"
)

// Parse decodes a config from YAML, rejecting unknown keys, then validates it.
func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config: empty document")
		}
		return nil, fmt.Errorf("config: parse yaml: %w", firstYAMLError(err))
	}
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, errors.New("config: expected a single YAML document")
	}
	if err := requireIntegerVersion(data); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// requireIntegerVersion rejects a fractional or quoted version, which yaml would
// otherwise coerce into the int field (1.5 silently becomes 1).
func requireIntegerVersion(data []byte) error {
	var probe struct {
		Version yaml.Node `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil
	}
	v := &probe.Version
	for v.Kind == yaml.AliasNode && v.Alias != nil {
		v = v.Alias
	}
	if v.Kind == yaml.ScalarNode && v.Tag != "!!int" {
		return fmt.Errorf("config: version must be an integer, got %q", v.Value)
	}
	return nil
}

// firstYAMLError reduces yaml's aggregated type error to its first entry, so a
// parse failure reports one problem like the rest of config validation.
func firstYAMLError(err error) error {
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		return errors.New(te.Errors[0])
	}
	return err
}

// Validate reports the first structural problem in the config, or nil.
func (c *Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("config: unsupported version %d (want %d)", c.Version, Version)
	}
	if c.Workers < 0 {
		return fmt.Errorf("config: workers must not be negative, got %d", c.Workers)
	}
	if !inputTypes[c.Input.Type] {
		return fmt.Errorf("config: input.type %q is not one of %s", c.Input.Type, inputList)
	}
	if len(c.Stages) == 0 {
		return errors.New("config: at least one stage is required")
	}
	seen := make(map[string]struct{}, len(c.Stages))
	for i, s := range c.Stages {
		if s.ID == "" {
			return fmt.Errorf("config: stages[%d]: id is required", i)
		}
		if _, dup := seen[s.ID]; dup {
			return fmt.Errorf("config: duplicate stage id %q", s.ID)
		}
		seen[s.ID] = struct{}{}
		if !stageTypes[s.Type] {
			return fmt.Errorf("config: stage %q: type %q is not one of %s", s.ID, s.Type, stageList)
		}
		if s.Path == "" {
			return fmt.Errorf("config: stage %q: a %s stage needs a path", s.ID, s.Type)
		}
		if s.Gate != nil {
			if _, err := stage.NewGate(*s.Gate); err != nil {
				return fmt.Errorf("config: stage %q: gate %v is invalid: %w", s.ID, *s.Gate, err)
			}
		}
	}
	if err := c.Sink.validate("sink"); err != nil {
		return err
	}
	for category, sk := range c.Routes {
		if category == "" {
			return errors.New("config: a route category must not be empty")
		}
		if err := sk.validate("route " + category); err != nil {
			return err
		}
	}
	if c.Review != nil {
		if err := c.Review.validate("review"); err != nil {
			return err
		}
	}
	return nil
}

func (s SinkSpec) validate(where string) error {
	if !sinkTypes[s.Type] {
		return fmt.Errorf("config: %s: type %q is not one of %s", where, s.Type, sinkList)
	}
	if s.Type == "drop" {
		if s.Path != "" || s.Stream != "" {
			return fmt.Errorf("config: %s: a drop sink takes no path or stream", where)
		}
		return nil
	}
	if s.Path == "" && s.Stream == "" {
		return fmt.Errorf("config: %s: a jsonl sink needs a path or a stream", where)
	}
	if s.Path != "" && s.Stream != "" {
		return fmt.Errorf("config: %s: set either path or stream, not both", where)
	}
	if s.Stream != "" && !streams[s.Stream] {
		return fmt.Errorf("config: %s: stream %q is not one of %s", where, s.Stream, streamList)
	}
	return nil
}
