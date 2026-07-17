package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ClassMesh/classmesh/rules"
	"gopkg.in/yaml.v3"
)

type rulesFile struct {
	Rules []ruleSpec `yaml:"rules"`
}

type ruleSpec rules.Rule

// ParseRules builds a rules stage from a YAML document.
func ParseRules(data []byte) (*rules.Stage, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg rulesFile
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return rules.New(nil)
		}
		return nil, fmt.Errorf("rules: parse yaml: %w", err)
	}
	declared := make([]rules.Rule, len(cfg.Rules))
	for i := range cfg.Rules {
		declared[i] = rules.Rule(cfg.Rules[i])
	}
	return rules.New(declared)
}

// LoadRules builds a rules stage from a YAML file.
func LoadRules(path string) (*rules.Stage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	return ParseRules(data)
}
