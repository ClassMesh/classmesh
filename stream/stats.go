package stream

// Stats summarizes one Run.
type Stats struct {
	// Processed counts records read from the source.
	Processed int
	// Classified counts records some stage decided.
	Classified int
	// Reviewed counts records no stage decided.
	Reviewed int
	// ByStage counts classifications per stage name.
	ByStage map[string]int
}
