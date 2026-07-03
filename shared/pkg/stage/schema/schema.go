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

// firstViolationCap sizes the reasons buffer on the first violation. Quarantined
// records typically breach a handful of constraints; 4 clears the common case
// without a regrowth and leaves the cited eight-violation case a single one,
// rather than the four an append from nil pays.
const firstViolationCap = 4

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

// constraint carries a compiled Field's two possible reason details, formatted
// once at New time so a violation allocates nothing and the hot loop's struct
// stays lean.
type constraint struct {
	index    int
	required bool
	typ      Type
	missing  string
	badType  string
}

// compiledField is what the flat classify path iterates per record: only the
// precompiled path and the check, so its stride stays lean. The reason details
// live in the Stage's parallel details slice, touched on violations only.
type compiledField struct {
	segments []string
	required bool
	typ      Type
}

// reasonDetail holds one field's precomputed reason texts, indexed like fields.
type reasonDetail struct {
	missing string
	badType string
}

func missingReason(detail string) domain.Reason {
	return domain.Reason{Code: "missing", Detail: detail}
}
func typeReason(detail string) domain.Reason { return domain.Reason{Code: "type", Detail: detail} }

// Stage validates records against a set of Field constraints. Schemas whose
// paths share prefixes are classified through a prefix trie that walks each
// shared prefix once per record; the rest use a flat per-field walk, which the
// trie cannot beat when there is no prefix to share.
type Stage struct {
	category string
	fields   []compiledField
	details  []reasonDetail
	root     *node
}

var _ stage.Stage = (*Stage)(nil)

// New compiles the field constraints. category names where violating records
// go; at least one field is required and every field needs a path. A trie is
// built only when it would save prefix descents over the flat walk.
func New(category string, fields []Field) (*Stage, error) {
	if category == "" {
		return nil, errors.New("schema: category is required")
	}
	if len(fields) == 0 {
		return nil, errors.New("schema: at least one field is required")
	}
	compiled := make([]compiledField, 0, len(fields))
	details := make([]reasonDetail, 0, len(fields))
	root := &node{}
	flatDescents := 0
	for i, f := range fields {
		if f.Path == "" {
			return nil, errors.New("schema: field path is required")
		}
		c := constraint{
			index:    i,
			required: f.Required,
			typ:      f.Type,
			missing:  fmt.Sprintf("required field %s is missing", f.Path),
			badType:  fmt.Sprintf("field %s is not a %s", f.Path, f.Type),
		}
		segments := fieldpath.Split(f.Path)
		compiled = append(compiled, compiledField{segments: segments, required: f.Required, typ: f.Type})
		details = append(details, reasonDetail{missing: c.missing, badType: c.badType})
		flatDescents += len(segments)
		cur := root
		for _, seg := range segments {
			cur = cur.child(seg)
		}
		cur.constraints = append(cur.constraints, c)
	}
	root.compress()
	s := &Stage{category: category, fields: compiled, details: details}
	if root.descents() < flatDescents {
		s.root = root
	}
	return s, nil
}

// Name implements stage.Stage.
func (s *Stage) Name() string { return Name }

// Classify implements stage.Stage: a record violating the schema is classified
// into the configured category with a reason per violation; a valid record
// returns ErrUnclassified so the cascade moves on. A valid record allocates
// nothing.
func (s *Stage) Classify(ctx context.Context, r domain.Record) (domain.Classification, error) {
	if err := ctx.Err(); err != nil {
		return domain.Classification{}, err
	}
	var out []domain.Reason
	if s.root != nil {
		out = s.classifyTrie(r.Fields)
	} else {
		for i, f := range s.fields {
			v, ok := fieldpath.Lookup(r.Fields, f.segments)
			if !ok {
				if f.required {
					out = appendReason(out, missingReason(s.details[i].missing))
				}
				continue
			}
			if f.typ != Any && !matchesType(v, f.typ) {
				out = appendReason(out, typeReason(s.details[i].badType))
			}
		}
	}
	if len(out) == 0 {
		return domain.Classification{}, stage.ErrUnclassified
	}
	return domain.Classification{Category: s.category, Confidence: 1, Stage: Name, Reasons: out}, nil
}

// classifyTrie walks the trie once, recording violations by declaration index,
// then restores declaration order.
func (s *Stage) classifyTrie(fields map[string]any) []domain.Reason {
	var acc reasons
	walk(s.root, fields, fields != nil, &acc)
	if len(acc.pending) == 0 {
		return nil
	}
	acc.sort()
	out := make([]domain.Reason, len(acc.pending))
	for i := range acc.pending {
		out[i] = acc.pending[i].reason
	}
	return out
}

func appendReason(out []domain.Reason, r domain.Reason) []domain.Reason {
	if out == nil {
		out = make([]domain.Reason, 0, firstViolationCap)
	}
	return append(out, r)
}

// node is a branch point in the prefix trie. Its constraints terminate at this
// path; its edges extend it. A node can hold both (a field on a prefix and a
// field on a longer path sharing it). Edges are a slice, not a map, so the
// per-record walk iterates them without map-iteration overhead.
type node struct {
	edges       []edge
	constraints []constraint
}

// edge is a run of one or more path segments to a child node. compress collapses
// unshared linear chains into a single multi-segment edge, so a shared prefix is
// the only thing that costs a branch.
type edge struct {
	segments []string
	child    *node
}

func (n *node) child(segment string) *node {
	for i := range n.edges {
		if len(n.edges[i].segments) == 1 && n.edges[i].segments[0] == segment {
			return n.edges[i].child
		}
	}
	c := &node{}
	n.edges = append(n.edges, edge{segments: []string{segment}, child: c})
	return c
}

// compress folds each chain of single-child, constraint-free nodes into the edge
// above it, then recurses. A node that carries a constraint or branches is a
// real stop and is kept.
func (n *node) compress() {
	for i := range n.edges {
		e := &n.edges[i]
		for len(e.child.constraints) == 0 && len(e.child.edges) == 1 {
			next := e.child.edges[0]
			e.segments = append(e.segments, next.segments...)
			e.child = next.child
		}
		e.child.compress()
	}
}

// descents counts the path segments the trie walks per record; compared against
// the flat walk's total, it reports whether any prefix is actually shared.
func (n *node) descents() int {
	total := 0
	for i := range n.edges {
		total += len(n.edges[i].segments) + n.edges[i].child.descents()
	}
	return total
}

// walk descends the trie once, evaluating every constraint against the record.
// value is the value reached at this node's path and found whether it was
// present; children are descended into only when value is an object. Leaf edges
// are evaluated inline so a shallow schema pays no recursive call per field.
func walk(n *node, value any, found bool, acc *reasons) {
	for i := range n.constraints {
		checkOne(&n.constraints[i], value, found, acc)
	}
	if len(n.edges) == 0 {
		return
	}
	obj, ok := value.(map[string]any)
	present := found && ok
	for i := range n.edges {
		e := &n.edges[i]
		var cv any
		var cfound bool
		if present {
			if len(e.segments) == 1 {
				cv, cfound = obj[e.segments[0]]
			} else {
				cv, cfound = descend(obj, e.segments)
			}
		}
		if len(e.child.edges) > 0 {
			walk(e.child, cv, cfound, acc)
			continue
		}
		for j := range e.child.constraints {
			checkOne(&e.child.constraints[j], cv, cfound, acc)
		}
	}
}

func checkOne(c *constraint, value any, found bool, acc *reasons) {
	if !found {
		if c.required {
			acc.add(c.index, missingReason(c.missing))
		}
		return
	}
	if c.typ != Any && !matchesType(value, c.typ) {
		acc.add(c.index, typeReason(c.badType))
	}
}

// descend resolves a compressed multi-segment edge from obj in one pass, the way
// fieldpath.Lookup walks precompiled segments.
func descend(obj map[string]any, segments []string) (any, bool) {
	var cur any = obj
	for _, s := range segments {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[s]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// pendingReason is a violation tagged with its declaration index so the trie
// walk's order can be restored to declaration order.
type pendingReason struct {
	index  int
	reason domain.Reason
}

// reasons collects violations lazily: the backing slice is allocated on the
// first add, so a valid record stays allocation-free.
type reasons struct {
	pending []pendingReason
}

func (a *reasons) add(index int, r domain.Reason) {
	if a.pending == nil {
		a.pending = make([]pendingReason, 0, firstViolationCap)
	}
	a.pending = append(a.pending, pendingReason{index: index, reason: r})
}

func (a *reasons) sort() {
	p := a.pending
	for i := 1; i < len(p); i++ {
		x := p[i]
		j := i - 1
		for j >= 0 && p[j].index > x.index {
			p[j+1] = p[j]
			j--
		}
		p[j+1] = x
	}
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
