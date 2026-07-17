package consumer_test

import (
	"context"
	"testing"

	"github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/rules"
	"github.com/ClassMesh/classmesh/stream"
	"github.com/ClassMesh/classmesh/stream/sink"
	"github.com/ClassMesh/classmesh/stream/source"
)

type fallbackStage struct{}

func (fallbackStage) Name() string { return "fallback" }

func (fallbackStage) Classify(context.Context, classmesh.Record) (classmesh.Classification, error) {
	return classmesh.Classification{Category: "statement", Confidence: 0.8}, nil
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func TestPublicConsumer(t *testing.T) {
	ruleStage := must(rules.New([]rules.Rule{{Category: "question", Contains: []string{"?"}}}))
	mesh := must(classmesh.New(ruleStage, fallbackStage{}))

	question := classmesh.Record{ID: "one", Kind: classmesh.KindText, Data: []byte("Ready?")}
	got, err := mesh.Classify(context.Background(), question)
	if err != nil || got.Category != "question" || got.Stage != rules.Name {
		t.Fatalf("Classify() = %+v, %v", got, err)
	}
	statement := classmesh.Record{ID: "two", Kind: classmesh.KindText, Data: []byte("Ready.")}
	out := sink.NewInMemory()
	engine := must(stream.New(stream.Options{
		Source:  source.NewInMemory([]classmesh.Record{question, statement}),
		Cascade: mesh,
		Sink:    out,
	}))
	stats := must(engine.Run(context.Background()))
	if stats.Classified != 2 || len(out.Entries()) != 2 || out.Entries()[1].Classification.Stage != "fallback" {
		t.Fatalf("Run() = %+v, entries=%d", stats, len(out.Entries()))
	}
}
