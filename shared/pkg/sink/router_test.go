package sink

import (
	"context"
	"errors"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

type failingSink struct{ err error }

func (f failingSink) Write(context.Context, domain.Record, domain.Classification) error { return nil }
func (f failingSink) Close() error                                                      { return f.err }

func TestRouterDispatchesByCategory(t *testing.T) {
	alerts := NewInMemory()
	billing := NewInMemory()
	fallback := NewInMemory()
	r := NewRouter(fallback, map[string]Sink{"alert": alerts, "billing": billing})

	ctx := context.Background()
	for _, cat := range []string{"alert", "billing", "misc"} {
		if err := r.Write(ctx, domain.Record{ID: cat}, domain.Classification{Category: cat}); err != nil {
			t.Fatalf("Write(%s) error = %v", cat, err)
		}
	}

	if e := alerts.Entries(); len(e) != 1 || e[0].Record.ID != "alert" {
		t.Fatalf("alerts = %+v, want one alert", e)
	}
	if e := billing.Entries(); len(e) != 1 || e[0].Record.ID != "billing" {
		t.Fatalf("billing = %+v, want one billing", e)
	}
	if e := fallback.Entries(); len(e) != 1 || e[0].Record.ID != "misc" {
		t.Fatalf("fallback = %+v, want the unrouted misc record", e)
	}
}

func TestRouterNilRouteDrops(t *testing.T) {
	fallback := NewInMemory()
	r := NewRouter(fallback, map[string]Sink{"noise": nil})
	if err := r.Write(context.Background(), domain.Record{ID: "x"}, domain.Classification{Category: "noise"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if e := fallback.Entries(); len(e) != 0 {
		t.Fatalf("fallback = %+v, want empty (a nil route drops its category)", e)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v (a nil route sink must not panic)", err)
	}
}

func TestRouterNilFallbackDrops(t *testing.T) {
	alerts := NewInMemory()
	r := NewRouter(nil, map[string]Sink{"alert": alerts})
	if err := r.Write(context.Background(), domain.Record{}, domain.Classification{Category: "unrouted"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n := len(alerts.Entries()); n != 0 {
		t.Fatalf("alerts = %d, want 0 (unrouted dropped)", n)
	}
}

func TestRouterCloseClosesAll(t *testing.T) {
	route := NewInMemory()
	fallback := NewInMemory()
	r := NewRouter(fallback, map[string]Sink{"x": route})
	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !route.closed || !fallback.closed {
		t.Fatalf("Close() left a sink open: route=%v fallback=%v", route.closed, fallback.closed)
	}
}

func TestRouterCloseReportsFirstError(t *testing.T) {
	boom := errors.New("boom")
	r := NewRouter(NewInMemory(), map[string]Sink{"x": failingSink{err: boom}})
	if err := r.Close(); !errors.Is(err, boom) {
		t.Fatalf("Close() error = %v, want boom", err)
	}
}
