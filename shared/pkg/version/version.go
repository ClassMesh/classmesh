// Package version exposes build metadata injected at link time via -ldflags.
package version

var (
	// Version is the semantic version or git describe output.
	Version = "dev"
	// Commit is the short git commit hash.
	Commit = "none"
	// Date is the UTC build timestamp.
	Date = "unknown"
)
