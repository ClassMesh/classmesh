// Package schema implements a stage.Stage that validates a structured record's
// decoded Fields against a declared shape. It classifies records that violate
// the schema into a quarantine category, with a reason per violation, and
// escalates valid records with ErrUnclassified, so it sits in front of the real
// classification stages and routes malformed input aside.
package schema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/fieldpath"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

// Name identifies this stage in classifications, stats, and logs.
const Name = "schema"

// Type is the JSON type a field is expected to hold. Number matches what JSON
// decoding yields (float64 or json.Number).
type Type int

const (
	Any Type = iota
	String
	Number
	Bool
)

func (t Type) String() string {
	switch t {
	case String:
		return "string"
	case Number:
		return "number"
	case Bool:
		return "bool"
	default:
		return "any"
	}
}

// Field constrains one value, addressed by a path the fieldpath package
// understands. A Required field must be present; unless Type is Any, a present
// value must hold that type.
type Field struct {
	Path     string
	Required bool
	Type     Type
}

type compiledField struct {
	path     string
	segments []string
	required bool
	typ      Type
}

// Stage validates records against a set of Field constraints.
type Stage struct {
	category string
	fields   []compiledField
}

var _ stage.Stage = (*Stage)(nil)

// New compiles the field constraints. category names where violating records
// go; at least one field is required and every field needs a path.
func New(category string, fields []Field) (*Stage, error) {
	if category == "" {
		return nil, errors.New("schema: category is required")
	}
	if len(fields) == 0 {
		return nil, errors.New("schema: at least one field is required")
	}
	compiled := make([]compiledField, 0, len(fields))
	for _, f := range fields {
		if f.Path == "" {
			return nil, errors.New("schema: field path is required")
		}
		compiled = append(compiled, compiledField{
			path:     f.Path,
			segments: fieldpath.Split(f.Path),
			required: f.Required,
			typ:      f.Type,
		})
	}
	return &Stage{category: category, fields: compiled}, nil
}

// Name implements stage.Stage.
func (s *Stage) Name() string { return Name }

// Classify implements stage.Stage: a record violating the schema is classified
// into the configured category with a reason per violation; a valid record
// returns ErrUnclassified so the cascade moves on.
func (s *Stage) Classify(ctx context.Context, r domain.Record) (domain.Classification, error) {
	if err := ctx.Err(); err != nil {
		return domain.Classification{}, err
	}
	var reasons []domain.Reason
	for _, f := range s.fields {
		v, ok := fieldpath.Lookup(r.Fields, f.segments)
		if !ok {
			if f.required {
				reasons = append(reasons, domain.Reason{Code: "missing", Detail: fmt.Sprintf("required field %s is missing", f.path)})
			}
			continue
		}
		if f.typ != Any && !matchesType(v, f.typ) {
			reasons = append(reasons, domain.Reason{Code: "type", Detail: fmt.Sprintf("field %s is not a %s", f.path, f.typ)})
		}
	}
	if len(reasons) == 0 {
		return domain.Classification{}, stage.ErrUnclassified
	}
	return domain.Classification{Category: s.category, Confidence: 1, Stage: Name, Reasons: reasons}, nil
}

func matchesType(v any, t Type) bool {
	switch t {
	case String:
		_, ok := v.(string)
		return ok
	case Bool:
		_, ok := v.(bool)
		return ok
	case Number:
		switch v.(type) {
		case float64, json.Number:
			return true
		default:
			return false
		}
	default:
		return true
	}
}
