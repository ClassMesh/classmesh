// Package fieldpath reads nested values out of a decoded JSON object
// (map[string]any) by dot-separated path, like "user.id". It is the lookup the
// field-matching rules build on.
package fieldpath

import "strings"

// Get walks fields by a dot-separated path and returns the value at the end
// and whether it was found. A segment that is missing, or that tries to
// descend into something that is not an object, returns (nil, false), as does
// an empty path. A path may end on any value, including a nested object or an
// array; descending into an array (numeric indexing) is not supported.
func Get(fields map[string]any, path string) (any, bool) {
	if path == "" || fields == nil {
		return nil, false
	}
	var cur any = fields
	for _, segment := range strings.Split(path, ".") {
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
