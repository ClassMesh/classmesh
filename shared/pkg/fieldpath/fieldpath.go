// Package fieldpath reads nested values out of a decoded JSON object
// (map[string]any) by dot-separated path, like "user.id". It is the lookup the
// field-matching rules build on. A dot always separates segments, so a key
// that literally contains a dot cannot be addressed.
package fieldpath

import "strings"

// Split breaks a dot-separated path into segments, returning nil for an empty
// path so it round-trips through Lookup the same way Get treats "". Callers on
// a hot path should Split once and reuse the result with Lookup rather than
// calling Get per record.
func Split(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

// Lookup walks fields by precompiled segments and returns the value at the end
// and whether it was found. A segment that is missing, or that tries to
// descend into something that is not an object, returns (nil, false), as does
// an empty path. A path may end on any value, including a nested object or an
// array; descending into an array (numeric indexing) is not supported.
func Lookup(fields map[string]any, segments []string) (any, bool) {
	if len(segments) == 0 || fields == nil {
		return nil, false
	}
	var cur any = fields
	for _, segment := range segments {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = obj[segment]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// Get is Lookup for a path that has not been split yet. An empty path returns
// (nil, false).
func Get(fields map[string]any, path string) (any, bool) {
	return Lookup(fields, Split(path))
}
