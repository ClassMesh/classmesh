// Package cascade keeps the old engine compiling during the module migration.
package cascade

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ClassMesh/classmesh"
)

// Result is the cascade-level outcome for a record.
type Result int

const (
	Exhausted Result = iota
	Classified
)

// Run delegates classification to the root Cascade.
func Run(ctx context.Context, stages []classmesh.Stage, gate float64, r classmesh.Record, logger *slog.Logger) (classmesh.Classification, Result, error) {
	cascade, err := classmesh.NewWithOptions(classmesh.Options{
		Stages:        stages,
		MinConfidence: gate,
		Logger:        logger,
	})
	if err != nil {
		return classmesh.Classification{}, Exhausted, err
	}
	classification, err := cascade.Classify(ctx, r)
	if errors.Is(err, classmesh.ErrUnclassified) {
		return classmesh.Classification{}, Exhausted, nil
	}
	if err != nil {
		return classmesh.Classification{}, Exhausted, err
	}
	return classification, Classified, nil
}
