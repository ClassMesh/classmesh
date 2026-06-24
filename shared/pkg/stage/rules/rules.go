// Package rules implements stage.Stage with deterministic, YAML-defined
// matching: ordered rules of payload and field matchers mapped to categories.
// First matching rule wins; no match means ErrUnclassified so the cascade
// escalates. As a deterministic stage it always emits confidence 1.
package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/fieldpath"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

// Name identifies this stage in classifications, stats, and logs.
const Name = "rules"

// Rule maps matchers to a category. A rule matches a record when ANY of its
// matchers hit: a Contains substring or Regex pattern against the raw payload,
// or a Fields matcher against the decoded fields. Substring matching is
// case-sensitive; use (?i) in a regex for case-insensitive matching.
type Rule struct {
	Category string         `yaml:"category"`
	Contains []string       `yaml:"contains"`
	Regex    []string       `yaml:"regex"`
	Fields   []FieldMatcher `yaml:"fields"`
}

// FieldMatcher tests one value in a record's decoded Fields, addressed by a
// dot-separated Path (see fieldpath). Exactly one condition must be set: Exact
// (equal), Contains (substring), Regex (pattern), or Exists (present when
// true, absent when false). The string conditions stringify a scalar value
// (string, number, bool); a value that is an object or array never matches a
// string condition. Numbers are matched by their text form, so quote them in
// YAML (exact: "500").
type FieldMatcher struct {
	Path     string  `yaml:"path"`
	Exact    *string `yaml:"exact"`
	Contains *string `yaml:"contains"`
	Regex    *string `yaml:"regex"`
	Exists   *bool   `yaml:"exists"`
}

// Config is the YAML document shape.
type Config struct {
	Rules []Rule `yaml:"rules"`
}

type fieldPredicate func(fields map[string]any) bool

type compiledRule struct {
	category string
	contains [][]byte
	regexes  []*regexp.Regexp
	fields   []fieldPredicate
}

// Stage is a deterministic rule-matching stage.
type Stage struct {
	rules []compiledRule
}

var _ stage.Stage = (*Stage)(nil)

// New compiles rules in order. Every rule needs a category and at least one
// matcher; every regex must compile and every field matcher must be well
// formed.
func New(rules []Rule) (*Stage, error) {
	if len(rules) == 0 {
		return nil, errors.New("rules: at least one rule is required")
	}
	compiled := make([]compiledRule, 0, len(rules))
	for i, r := range rules {
		if r.Category == "" {
			return nil, fmt.Errorf("rules: rule %d: category is required", i+1)
		}
		if len(r.Contains) == 0 && len(r.Regex) == 0 && len(r.Fields) == 0 {
			return nil, fmt.Errorf("rules: rule %d (%s): at least one matcher (contains/regex/fields) is required", i+1, r.Category)
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
		for _, fm := range r.Fields {
			pred, err := compileFieldMatcher(fm, i+1, r.Category)
			if err != nil {
				return nil, err
			}
			cr.fields = append(cr.fields, pred)
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
		if rule.matches(r) {
			return domain.Classification{Category: rule.category, Confidence: 1, Stage: Name}, nil
		}
	}
	return domain.Classification{}, stage.ErrUnclassified
}

func (cr compiledRule) matches(r domain.Record) bool {
	for _, sub := range cr.contains {
		if bytes.Contains(r.Data, sub) {
			return true
		}
	}
	for _, re := range cr.regexes {
		if re.Match(r.Data) {
			return true
		}
	}
	if r.Fields != nil {
		for _, pred := range cr.fields {
			if pred(r.Fields) {
				return true
			}
		}
	}
	return false
}

func compileFieldMatcher(fm FieldMatcher, ruleNum int, category string) (fieldPredicate, error) {
	if fm.Path == "" {
		return nil, fmt.Errorf("rules: rule %d (%s): field matcher needs a path", ruleNum, category)
	}
	set := 0
	for _, on := range []bool{fm.Exact != nil, fm.Contains != nil, fm.Regex != nil, fm.Exists != nil} {
		if on {
			set++
		}
	}
	if set != 1 {
		return nil, fmt.Errorf("rules: rule %d (%s): field %q needs exactly one of exact/contains/regex/exists", ruleNum, category, fm.Path)
	}

	path := fm.Path
	switch {
	case fm.Exact != nil:
		want := *fm.Exact
		return func(fields map[string]any) bool {
			s, ok := lookupString(fields, path)
			return ok && s == want
		}, nil
	case fm.Contains != nil:
		want := *fm.Contains
		if want == "" {
			return nil, fmt.Errorf("rules: rule %d (%s): field %q: empty contains matcher", ruleNum, category, fm.Path)
		}
		return func(fields map[string]any) bool {
			s, ok := lookupString(fields, path)
			return ok && strings.Contains(s, want)
		}, nil
	case fm.Regex != nil:
		re, err := regexp.Compile(*fm.Regex)
		if err != nil {
			return nil, fmt.Errorf("rules: rule %d (%s): field %q: %w", ruleNum, category, fm.Path, err)
		}
		return func(fields map[string]any) bool {
			s, ok := lookupString(fields, path)
			return ok && re.MatchString(s)
		}, nil
	default:
		want := *fm.Exists
		return func(fields map[string]any) bool {
			_, ok := fieldpath.Get(fields, path)
			return ok == want
		}, nil
	}
}

// lookupString renders the scalar value at path (string, number, bool) as
// text. A missing path or a non-scalar value (object, array, null) reports false.
func lookupString(fields map[string]any, path string) (string, bool) {
	v, ok := fieldpath.Get(fields, path)
	if !ok {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case json.Number:
		return t.String(), true
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10), true
	case reflect.Float32:
		return strconv.FormatFloat(rv.Float(), 'f', -1, 32), true
	case reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'f', -1, 64), true
	default:
		return "", false
	}
}
