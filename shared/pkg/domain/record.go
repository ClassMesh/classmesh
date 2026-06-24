// Package domain holds the core value types that flow through a ClassMesh
// pipeline. It has no dependencies and every other package depends on it.
package domain

// Kind identifies the shape of a Record's payload. The zero value is
// KindUnknown.
type Kind string

const (
	KindUnknown Kind = ""
	KindText    Kind = "text"
	KindLog     Kind = "log"
	KindEvent   Kind = "event"
	KindRecord  Kind = "record"
	KindJSON    Kind = "json"
)

// Record is a single unit of data moving through the pipeline. Sources
// produce Records, stages classify them, sinks consume them.
type Record struct {
	// ID uniquely identifies the record within a run. Sources assign it.
	ID string
	// Kind identifies the payload shape. Zero value is KindUnknown.
	Kind Kind
	// Data is the raw payload (a log line, a JSON document, ...).
	Data []byte
	// Fields holds structured attributes decoded from the payload, when a
	// source has them. Nil for unstructured payloads.
	Fields map[string]any
	// Meta carries source-specific context (file name, line number, ...).
	Meta map[string]string
}

// Reason explains why a stage picked a category. A stage may attach zero
// or more.
type Reason struct {
	// Code is a short tag for the evidence, like a rule ID.
	Code string `json:"code"`
	// Detail is a readable description.
	Detail string `json:"detail,omitempty"`
}

// Classification is the outcome of running a Record through a stage.
type Classification struct {
	// Category is the assigned label.
	Category string
	// Confidence is in [0, 1]. Deterministic stages emit 1.
	Confidence float64
	// Stage names the stage that produced this classification.
	Stage string
	// Reasons is optional evidence for the classification.
	Reasons []Reason
}

// IsValid reports whether the classification is well-formed: a non-empty
// category and a confidence within [0, 1].
func (c Classification) IsValid() bool {
	return c.Category != "" && c.Confidence >= 0 && c.Confidence <= 1
}
