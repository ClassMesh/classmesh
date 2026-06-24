package stage

import "fmt"

// Error reports that a stage itself broke, as opposed to ErrUnclassified,
// which just asks the cascade to try the next stage. Stage names the stage
// that failed and Unwrap gives the cause, so callers can tell which stage
// failed and why with errors.As and errors.Is.
type Error struct {
	// Stage is the stage that failed.
	Stage string
	// Err is the underlying cause.
	Err error
}

// Error implements error.
func (e *Error) Error() string { return fmt.Sprintf("stage %s: %v", e.Stage, e.Err) }

// Unwrap returns the cause so errors.Is and errors.As reach through.
func (e *Error) Unwrap() error { return e.Err }
