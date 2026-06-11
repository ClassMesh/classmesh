// Package domain holds the core value types that flow through a ClassMesh
// pipeline. It has no dependencies and every other package depends on it.
package domain

// Record is a single unit of data moving through the pipeline. Sources
// produce Records, stages classify them, sinks consume them.
type Record struct {
	// ID uniquely identifies the record within a run. Sources assign it.
	ID string
	// Data is the raw payload (a log line, a JSON document, ...).
	Data []byte
	// Meta carries source-specific context (file name, line number, ...).
	Meta map[string]string
}

// Classification is the outcome of running a Record through a stage.
type Classification struct {
	// Category is the assigned label.
	Category string
	// Confidence is in [0, 1]. Deterministic stages emit 1.
	Confidence float64
	// Stage names the stage that produced this classification.
	Stage string
}

// IsValid reports whether the classification is well-formed: a non-empty
// category and a confidence within [0, 1].
func (c Classification) IsValid() bool {
	return c.Category != "" && c.Confidence >= 0 && c.Confidence <= 1
}
