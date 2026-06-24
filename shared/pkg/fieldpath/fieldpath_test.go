package fieldpath

import (
	"reflect"
	"testing"
)

func TestGet(t *testing.T) {
	fields := map[string]any{
		"level": "error",
		"http":  map[string]any{"status": float64(500), "method": "GET"},
		"user":  map[string]any{"id": float64(7), "roles": []any{"admin", "ops"}},
	}

	cases := []struct {
		name string
		path string
		want any
		ok   bool
	}{
		{"top-level scalar", "level", "error", true},
		{"nested scalar", "http.status", float64(500), true},
		{"nested string", "http.method", "GET", true},
		{"path ends on an object", "http", map[string]any{"status": float64(500), "method": "GET"}, true},
		{"path ends on an array", "user.roles", []any{"admin", "ops"}, true},
		{"missing top-level key", "nope", nil, false},
		{"missing nested key", "http.code", nil, false},
		{"descend into a scalar", "level.sub", nil, false},
		{"descend into an array", "user.roles.0", nil, false},
		{"empty path", "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Get(fields, tc.path)
			if ok != tc.ok {
				t.Fatalf("Get(%q) ok = %v, want %v", tc.path, ok, tc.ok)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Get(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestGetNilFields(t *testing.T) {
	if _, ok := Get(nil, "a"); ok {
		t.Fatal("Get(nil, ...) ok = true, want false")
	}
}
