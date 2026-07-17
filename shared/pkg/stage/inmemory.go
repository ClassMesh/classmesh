package stage

import (
	"context"

	"github.com/ClassMesh/classmesh"
)

// Static is a Stage that classifies by exact-match lookup of the record's
// payload, for tests and wiring examples. Records whose payload is not in
// the table are unclassified.
type Static struct {
	name       string
	categories map[string]string
}

var _ classmesh.Stage = (*Static)(nil)

// NewStatic returns a Stage named name that maps an exact payload string to
// a category with full confidence.
func NewStatic(name string, categories map[string]string) *Static {
	return &Static{name: name, categories: categories}
}

// Name implements Stage.
func (s *Static) Name() string { return s.name }

// Classify implements Stage.
func (s *Static) Classify(ctx context.Context, r classmesh.Record) (classmesh.Classification, error) {
	if err := ctx.Err(); err != nil {
		return classmesh.Classification{}, err
	}
	category, ok := s.categories[string(r.Data)]
	if !ok {
		return classmesh.Classification{}, classmesh.ErrUnclassified
	}
	return classmesh.Classification{Category: category, Confidence: 1, Stage: s.name}, nil
}
