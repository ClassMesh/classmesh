package sink

import (
	"context"

	domain "github.com/ClassMesh/classmesh"
)

// Router is a Sink that dispatches each record to a per-category Sink, falling
// back to a default Sink for categories with no route. Because it is itself a
// Sink, the engine and the CLI use it unchanged.
type Router struct {
	routes   map[string]Sink
	fallback Sink
}

var _ Sink = (*Router)(nil)

// NewRouter returns a Router that sends a record to the Sink mapped to its
// classification Category, or to fallback when no category matches. A nil
// route Sink drops records of that category; a nil fallback drops records with
// an unrouted category.
func NewRouter(fallback Sink, routes map[string]Sink) *Router {
	owned := make(map[string]Sink, len(routes))
	for category, s := range routes {
		owned[category] = s
	}
	return &Router{routes: owned, fallback: fallback}
}

// Write implements Sink, dispatching on the classification's Category.
func (r *Router) Write(ctx context.Context, rec domain.Record, c domain.Classification) error {
	if s, ok := r.routes[c.Category]; ok {
		if s == nil {
			return nil
		}
		return s.Write(ctx, rec, c)
	}
	if r.fallback != nil {
		return r.fallback.Write(ctx, rec, c)
	}
	return nil
}

// Close closes every underlying Sink, returning the first error. A Sink shared
// by several categories is closed more than once, which the idempotent Close
// contract allows.
func (r *Router) Close() error {
	var first error
	for _, s := range r.routes {
		if s == nil {
			continue
		}
		if err := s.Close(); err != nil && first == nil {
			first = err
		}
	}
	if r.fallback != nil {
		if err := r.fallback.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
