// Package fieldpath reads nested values out of a decoded JSON object
// (map[string]any) by path. The default syntax is a dot-separated path, like
// "user.id". A path that begins with "/" is instead an RFC 6901 JSON Pointer
// ("/user/id"), which can address a key that itself contains a dot ("/http.status").
// It is the lookup the field-matching rules build on.
package fieldpath

import "strings"

// Split breaks a path into segments. A "/"-prefixed path is parsed as a JSON
// Pointer (see splitPointer); otherwise it is split on ".". An empty path
// returns nil so it round-trips through Lookup the same way Get treats "".
// Callers on a hot path should Split once and reuse the result with Lookup
// rather than calling Get per record.
func Split(path string) []string {
	if path == "" {
		return nil
	}
	if path[0] == '/' {
		return splitPointer(path)
	}
	return strings.Split(path, ".")
}

// splitPointer parses an RFC 6901 JSON Pointer into segments, unescaping ~1 as
// "/" and ~0 as "~".
func splitPointer(path string) []string {
	segs := strings.Split(path[1:], "/")
	for i, s := range segs {
		if strings.IndexByte(s, '~') >= 0 {
			s = strings.ReplaceAll(s, "~1", "/")
			s = strings.ReplaceAll(s, "~0", "~")
			segs[i] = s
		}
	}
	return segs
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
