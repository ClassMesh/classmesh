// Package rules implements stage.Stage with deterministic, YAML-defined
// matching: ordered rules of substring and regex matchers mapped to
// categories. First matching rule wins; no match means ErrUnclassified so
// the cascade escalates. As a deterministic stage it always emits
// confidence 1.
package rules

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

// Name identifies this stage in classifications, stats, and logs.
const Name = "rules"

// Rule maps matchers to a category. A rule matches a record when ANY of its
// Contains substrings or ANY of its Regex patterns match the payload.
// Substring matching is case-sensitive; use (?i) in a regex for
// case-insensitive matching.
type Rule struct {
	Category string   `yaml:"category"`
	Contains []string `yaml:"contains"`
	Regex    []string `yaml:"regex"`
}

// Config is the YAML document shape.
type Config struct {
	Rules []Rule `yaml:"rules"`
}

type compiledRule struct {
	category string
	contains [][]byte
	regexes  []*regexp.Regexp
}

// Stage is a deterministic rule-matching stage.
type Stage struct {
	rules []compiledRule
}

var _ stage.Stage = (*Stage)(nil)

// New compiles rules in order. Every rule needs a category and at least one
// matcher; every regex must compile.
func New(rules []Rule) (*Stage, error) {
	if len(rules) == 0 {
		return nil, errors.New("rules: at least one rule is required")
	}
	compiled := make([]compiledRule, 0, len(rules))
	for i, r := range rules {
		if r.Category == "" {
			return nil, fmt.Errorf("rules: rule %d: category is required", i+1)
		}
		if len(r.Contains) == 0 && len(r.Regex) == 0 {
			return nil, fmt.Errorf("rules: rule %d (%s): at least one matcher (contains/regex) is required", i+1, r.Category)
		}
		cr := compiledRule{category: r.Category}
		for _, sub := range r.Contains {
			if sub == "" {
				return nil, fmt.Errorf("rules: rule %d (%s): empty contains matcher", i+1, r.Category)
			}
			cr.contains = append(cr.contains, []byte(sub))
		}
		for _, pattern := range r.Regex {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("rules: rule %d (%s): %w", i+1, r.Category, err)
			}
			cr.regexes = append(cr.regexes, re)
		}
		compiled = append(compiled, cr)
	}
	return &Stage{rules: compiled}, nil
}

// Parse builds a Stage from a YAML document.
func Parse(data []byte) (*Stage, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("rules: parse yaml: %w", err)
	}
	return New(cfg.Rules)
}

// Load builds a Stage from a YAML file.
func Load(path string) (*Stage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	return Parse(data)
}

// Name implements stage.Stage.
func (s *Stage) Name() string { return Name }

// Classify implements stage.Stage: first matching rule wins.
func (s *Stage) Classify(ctx context.Context, r domain.Record) (domain.Classification, error) {
	if err := ctx.Err(); err != nil {
		return domain.Classification{}, err
	}
	for _, rule := range s.rules {
		if rule.matches(r.Data) {
			return domain.Classification{Category: rule.category, Confidence: 1, Stage: Name}, nil
		}
	}
	return domain.Classification{}, stage.ErrUnclassified
}

func (cr compiledRule) matches(data []byte) bool {
	for _, sub := range cr.contains {
		if bytes.Contains(data, sub) {
			return true
		}
	}
	for _, re := range cr.regexes {
		if re.Match(data) {
			return true
		}
	}
	return false
}
