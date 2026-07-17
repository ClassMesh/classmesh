// Package rules implements deterministic matching as a classmesh.Stage.
// Rules are evaluated in order and the first match wins.
// Matches emit confidence 1 and misses return classmesh.ErrUnclassified.
package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"

	"github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/internal/fieldpath"
)

// Name identifies this stage in classifications, stats, and logs.
const Name = "rules"

// Rule maps matchers to a category. It carries up to three matcher blocks, all
// of which must be satisfied for the rule to match (an absent block is
// ignored):
//
//   - the top-level Contains/Regex/Fields matchers, satisfied when ANY of them
//     hits (the shorthand for a simple rule);
//   - All, satisfied when every listed matcher hits;
//   - Any, satisfied when at least one listed matcher hits.
//
// Substring matching is case-sensitive; use (?i) in a regex for
// case-insensitive matching. ID and Category label the rule in errors and in
// the reason attached to a match.
type Rule struct {
	ID       string
	Category string
	Contains []string
	Regex    []string
	Fields   []FieldMatcher
	Any      []Matcher
	All      []Matcher
}

// Matcher is one condition inside an Any or All group: exactly one of a payload
// Contains substring, a payload Regex, or a Field test.
type Matcher struct {
	Contains *string
	Regex    *string
	Field    *FieldMatcher
}

// FieldMatcher tests one value in a record's decoded Fields, addressed by a
// dot-separated Path (see fieldpath). Exactly one condition must be set:
//   - Exact (equal), Contains (substring), Regex (pattern) compare the value as
//     text; a value that is an object or array never matches. Numbers are
//     matched by their text form.
//   - Gt, Gte, Lt, Lte compare the value as a number; a value that is not
//     numeric (including a numeric-looking string) never matches.
//   - Exists is true when the path is present, false when absent.
type FieldMatcher struct {
	Path     string
	Exact    *string
	Contains *string
	Regex    *string
	Exists   *bool
	Gt       *float64
	Gte      *float64
	Lt       *float64
	Lte      *float64
}

type fieldPredicate func(fields map[string]any) bool

// check is a compiled matcher: a predicate over a record plus a short
// description used as match evidence in a reason.
type check struct {
	desc  string
	match func(r classmesh.Record) bool
}

type compiledRule struct {
	category string
	base     []check // top-level matchers, satisfied when any hits
	any      []check // satisfied when any hits
	all      []check // satisfied when every one hits
	// reasons is a precomputed, read-only single reason naming the rule and
	// what it matches on, so attaching it on a match costs no allocation.
	reasons []classmesh.Reason
}

// Stage is a deterministic rule-matching stage.
type Stage struct {
	rules []compiledRule
}

var _ classmesh.Stage = (*Stage)(nil)

// New compiles rules in order. Every rule needs a category and at least one
// matcher; every regex must compile and every matcher must be well formed.
func New(rules []Rule) (*Stage, error) {
	if len(rules) == 0 {
		return nil, errors.New("rules: at least one rule is required")
	}
	compiled := make([]compiledRule, 0, len(rules))
	for i, r := range rules {
		if r.Category == "" {
			return nil, fmt.Errorf("rules: rule %d: category is required", i+1)
		}
		label := ruleLabel(i, r.ID, r.Category)
		if len(r.Contains)+len(r.Regex)+len(r.Fields)+len(r.Any)+len(r.All) == 0 {
			return nil, fmt.Errorf("rules: %s: at least one matcher (contains/regex/fields/any/all) is required", label)
		}
		cr := compiledRule{category: r.Category}

		for _, sub := range r.Contains {
			c, err := containsCheck(sub, label)
			if err != nil {
				return nil, err
			}
			cr.base = append(cr.base, c)
		}
		for _, pattern := range r.Regex {
			c, err := regexCheck(pattern, label)
			if err != nil {
				return nil, err
			}
			cr.base = append(cr.base, c)
		}
		for _, fm := range r.Fields {
			c, err := fieldCheck(fm, label)
			if err != nil {
				return nil, err
			}
			cr.base = append(cr.base, c)
		}
		for _, m := range r.Any {
			c, err := groupCheck(m, label)
			if err != nil {
				return nil, err
			}
			cr.any = append(cr.any, c)
		}
		for _, m := range r.All {
			c, err := groupCheck(m, label)
			if err != nil {
				return nil, err
			}
			cr.all = append(cr.all, c)
		}

		code := r.ID
		if code == "" {
			code = r.Category
		}
		cr.reasons = []classmesh.Reason{{Code: code, Detail: cr.describe()}}
		compiled = append(compiled, cr)
	}
	return &Stage{rules: compiled}, nil
}

// Name implements classmesh.Stage.
func (s *Stage) Name() string { return Name }

// Classify implements classmesh.Stage: first matching rule wins, and the result
// carries the rule's precomputed reason (its id/category and what it matches on).
func (s *Stage) Classify(ctx context.Context, r classmesh.Record) (classmesh.Classification, error) {
	if err := ctx.Err(); err != nil {
		return classmesh.Classification{}, err
	}
	for _, rule := range s.rules {
		if rule.match(r) {
			return classmesh.Classification{
				Category:   rule.category,
				Confidence: 1,
				Stage:      Name,
				Reasons:    rule.reasons,
			}, nil
		}
	}
	return classmesh.Classification{}, classmesh.ErrUnclassified
}

func (cr compiledRule) match(r classmesh.Record) bool {
	if len(cr.base) > 0 && !anyHit(cr.base, r) {
		return false
	}
	if len(cr.any) > 0 && !anyHit(cr.any, r) {
		return false
	}
	for _, c := range cr.all {
		if !c.match(r) {
			return false
		}
	}
	return true
}

func anyHit(checks []check, r classmesh.Record) bool {
	for _, c := range checks {
		if c.match(r) {
			return true
		}
	}
	return false
}

// describe renders a static, human-readable summary of what the rule matches
// on, used as the Detail of its precomputed reason.
func (cr compiledRule) describe() string {
	var parts []string
	if len(cr.base) > 0 {
		parts = append(parts, joinDescs(cr.base, " or "))
	}
	if len(cr.any) > 0 {
		parts = append(parts, "any("+joinDescs(cr.any, " or ")+")")
	}
	if len(cr.all) > 0 {
		parts = append(parts, "all("+joinDescs(cr.all, " and ")+")")
	}
	return strings.Join(parts, " and ")
}

func joinDescs(checks []check, sep string) string {
	descs := make([]string, len(checks))
	for i, c := range checks {
		descs[i] = c.desc
	}
	return strings.Join(descs, sep)
}

func ruleLabel(i int, id, category string) string {
	name := category
	if id != "" {
		name = id
	}
	return fmt.Sprintf("rule %d (%s)", i+1, name)
}

func containsCheck(sub, label string) (check, error) {
	if sub == "" {
		return check{}, fmt.Errorf("rules: %s: empty contains matcher", label)
	}
	b := []byte(sub)
	return check{
		desc:  fmt.Sprintf("contains %q", sub),
		match: func(r classmesh.Record) bool { return bytes.Contains(r.Data, b) },
	}, nil
}

func regexCheck(pattern, label string) (check, error) {
	if pattern == "" {
		return check{}, fmt.Errorf("rules: %s: empty regex matcher (matches everything)", label)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return check{}, fmt.Errorf("rules: %s: %w", label, err)
	}
	desc := fmt.Sprintf("regex %q", pattern)
	lit, complete := re.LiteralPrefix()
	switch {
	case lit != "" && complete && !hasZeroWidthAssertion(pattern):
		b := []byte(lit)
		return check{
			desc:  desc,
			match: func(r classmesh.Record) bool { return bytes.Contains(r.Data, b) },
		}, nil
	case lit != "":
		b := []byte(lit)
		return check{
			desc:  desc,
			match: func(r classmesh.Record) bool { return bytes.Contains(r.Data, b) && re.Match(r.Data) },
		}, nil
	default:
		return check{
			desc:  desc,
			match: func(r classmesh.Record) bool { return re.Match(r.Data) },
		}, nil
	}
}

// hasZeroWidthAssertion reports whether pattern contains ^ $ \A \z \b or \B.
// LiteralPrefix ignores these, so the substring-only fast path is unsound
// whenever one is present.
func hasZeroWidthAssertion(pattern string) bool {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return true
	}
	return containsAssertion(re)
}

func containsAssertion(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return true
	}
	for _, sub := range re.Sub {
		if containsAssertion(sub) {
			return true
		}
	}
	return false
}

func groupCheck(m Matcher, label string) (check, error) {
	set := 0
	for _, on := range []bool{m.Contains != nil, m.Regex != nil, m.Field != nil} {
		if on {
			set++
		}
	}
	if set != 1 {
		return check{}, fmt.Errorf("rules: %s: each any/all matcher needs exactly one of contains/regex/field", label)
	}
	switch {
	case m.Contains != nil:
		return containsCheck(*m.Contains, label)
	case m.Regex != nil:
		return regexCheck(*m.Regex, label)
	default:
		return fieldCheck(*m.Field, label)
	}
}

func fieldCheck(fm FieldMatcher, label string) (check, error) {
	if fm.Path == "" {
		return check{}, fmt.Errorf("rules: %s: field matcher needs a path", label)
	}
	set := 0
	for _, on := range []bool{
		fm.Exact != nil, fm.Contains != nil, fm.Regex != nil, fm.Exists != nil,
		fm.Gt != nil, fm.Gte != nil, fm.Lt != nil, fm.Lte != nil,
	} {
		if on {
			set++
		}
	}
	if set != 1 {
		return check{}, fmt.Errorf("rules: %s: field %q needs exactly one of exact/contains/regex/exists/gt/gte/lt/lte", label, fm.Path)
	}

	path := fm.Path
	segments := fieldpath.Split(path)
	var desc string
	var pred fieldPredicate
	switch {
	case fm.Exact != nil:
		want := *fm.Exact
		desc = fmt.Sprintf("field %s == %q", path, want)
		pred = func(fields map[string]any) bool {
			s, ok := lookupString(fields, segments)
			return ok && s == want
		}
	case fm.Contains != nil:
		want := *fm.Contains
		if want == "" {
			return check{}, fmt.Errorf("rules: %s: field %q: empty contains matcher", label, fm.Path)
		}
		desc = fmt.Sprintf("field %s contains %q", path, want)
		pred = func(fields map[string]any) bool {
			s, ok := lookupString(fields, segments)
			return ok && strings.Contains(s, want)
		}
	case fm.Regex != nil:
		re, err := regexp.Compile(*fm.Regex)
		if err != nil {
			return check{}, fmt.Errorf("rules: %s: field %q: %w", label, fm.Path, err)
		}
		desc = fmt.Sprintf("field %s matches %q", path, re.String())
		lit, complete := re.LiteralPrefix()
		switch {
		case lit != "" && complete && !hasZeroWidthAssertion(*fm.Regex):
			pred = func(fields map[string]any) bool {
				s, ok := lookupString(fields, segments)
				return ok && strings.Contains(s, lit)
			}
		case lit != "":
			pred = func(fields map[string]any) bool {
				s, ok := lookupString(fields, segments)
				return ok && strings.Contains(s, lit) && re.MatchString(s)
			}
		default:
			pred = func(fields map[string]any) bool {
				s, ok := lookupString(fields, segments)
				return ok && re.MatchString(s)
			}
		}
	case fm.Gt != nil:
		want := *fm.Gt
		desc = fmt.Sprintf("field %s > %v", path, want)
		pred = func(fields map[string]any) bool {
			n, ok := lookupNumber(fields, segments)
			return ok && n > want
		}
	case fm.Gte != nil:
		want := *fm.Gte
		desc = fmt.Sprintf("field %s >= %v", path, want)
		pred = func(fields map[string]any) bool {
			n, ok := lookupNumber(fields, segments)
			return ok && n >= want
		}
	case fm.Lt != nil:
		want := *fm.Lt
		desc = fmt.Sprintf("field %s < %v", path, want)
		pred = func(fields map[string]any) bool {
			n, ok := lookupNumber(fields, segments)
			return ok && n < want
		}
	case fm.Lte != nil:
		want := *fm.Lte
		desc = fmt.Sprintf("field %s <= %v", path, want)
		pred = func(fields map[string]any) bool {
			n, ok := lookupNumber(fields, segments)
			return ok && n <= want
		}
	default:
		want := *fm.Exists
		if want {
			desc = fmt.Sprintf("field %s exists", path)
		} else {
			desc = fmt.Sprintf("field %s absent", path)
		}
		pred = func(fields map[string]any) bool {
			_, ok := fieldpath.Lookup(fields, segments)
			return ok == want
		}
	}

	return check{
		desc: desc,
		match: func(r classmesh.Record) bool {
			if r.Fields == nil {
				return false
			}
			return pred(r.Fields)
		},
	}, nil
}

// lookupString renders the scalar value at the path segments (string, number,
// bool) as text. A missing path or a non-scalar value (object, array, null)
// reports false.
func lookupString(fields map[string]any, segments []string) (string, bool) {
	v, ok := fieldpath.Lookup(fields, segments)
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

// lookupNumber reads the value at the path segments as a float64. A missing
// path or a value that is not numeric (including a numeric-looking string)
// reports false.
func lookupNumber(fields map[string]any, segments []string) (float64, bool) {
	v, ok := fieldpath.Lookup(fields, segments)
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	default:
		return 0, false
	}
}
