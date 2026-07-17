// Package mockstage implements the internal deterministic mock stage.
package mockstage

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ClassMesh/classmesh"
)

// Name identifies this stage in classifications, stats, and logs.
const Name = "mock"

// Outcome is a category scored with a fixed confidence.
type Outcome struct {
	Category   string
	Confidence float64
}

// Matcher scores records whose payload contains any of the substrings.
type Matcher struct {
	Contains   []string
	Category   string
	Confidence float64
}

// Config declares a mock stage: ordered matchers tried first-match-wins, and
// an optional default outcome for records no matcher hits. Without a default,
// unmatched records escalate with ErrUnclassified.
type Config struct {
	Matchers []Matcher
	Default  *Outcome
}

type compiledMatcher struct {
	needles [][]byte
	result  classmesh.Classification
}

// Stage scores records with declared confidences.
type Stage struct {
	matchers []compiledMatcher
	def      *classmesh.Classification
}

var _ classmesh.Stage = (*Stage)(nil)

// New validates and compiles the config. At least one matcher or a default is
// required; every outcome needs a category and a confidence in [0, 1].
func New(cfg Config) (*Stage, error) {
	if len(cfg.Matchers) == 0 && cfg.Default == nil {
		return nil, errors.New("mock: at least one matcher or a default is required")
	}
	s := &Stage{matchers: make([]compiledMatcher, 0, len(cfg.Matchers))}
	for i, m := range cfg.Matchers {
		if len(m.Contains) == 0 {
			return nil, fmt.Errorf("mock: matcher %d (%s): contains is required", i+1, m.Category)
		}
		needles := make([][]byte, 0, len(m.Contains))
		for _, c := range m.Contains {
			if c == "" {
				return nil, fmt.Errorf("mock: matcher %d (%s): contains must not hold an empty string", i+1, m.Category)
			}
			needles = append(needles, []byte(c))
		}
		result, err := outcome(fmt.Sprintf("matcher %d", i+1), Outcome{Category: m.Category, Confidence: m.Confidence})
		if err != nil {
			return nil, err
		}
		s.matchers = append(s.matchers, compiledMatcher{needles: needles, result: result})
	}
	if cfg.Default != nil {
		result, err := outcome("default", *cfg.Default)
		if err != nil {
			return nil, err
		}
		s.def = &result
	}
	return s, nil
}

func outcome(where string, o Outcome) (classmesh.Classification, error) {
	c := classmesh.Classification{
		Category:   o.Category,
		Confidence: o.Confidence,
		Stage:      Name,
		Reasons:    []classmesh.Reason{{Code: Name, Detail: fmt.Sprintf("mock score %v for %q", o.Confidence, o.Category)}},
	}
	if !c.IsValid() {
		return classmesh.Classification{}, fmt.Errorf("mock: %s: category %q with confidence %v is invalid", where, o.Category, o.Confidence)
	}
	return c, nil
}

// Name implements classmesh.Stage.
func (s *Stage) Name() string { return Name }

// Classify implements classmesh.Stage: the first matcher whose substring hits the
// payload returns its outcome; otherwise the default applies, or the record
// escalates with ErrUnclassified.
func (s *Stage) Classify(ctx context.Context, r classmesh.Record) (classmesh.Classification, error) {
	if err := ctx.Err(); err != nil {
		return classmesh.Classification{}, err
	}
	for _, m := range s.matchers {
		for _, needle := range m.needles {
			if bytes.Contains(r.Data, needle) {
				return m.result, nil
			}
		}
	}
	if s.def != nil {
		return *s.def, nil
	}
	return classmesh.Classification{}, classmesh.ErrUnclassified
}
