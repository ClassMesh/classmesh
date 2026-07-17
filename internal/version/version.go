// Package version exposes build metadata injected at link time via -ldflags.
package version

import "runtime/debug"

var (
	// Version is the semantic version or git describe output.
	Version = "dev"
	// Commit is the short git commit hash.
	Commit = "none"
	// Date is the UTC build timestamp.
	Date = "unknown"
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if ok {
		applyBuildInfo(info)
	}
}

func applyBuildInfo(info *debug.BuildInfo) {
	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	for _, setting := range info.Settings {
		if setting.Value == "" {
			continue
		}
		switch setting.Key {
		case "vcs.revision":
			if Commit == "none" {
				Commit = setting.Value
			}
		case "vcs.time":
			if Date == "unknown" {
				Date = setting.Value
			}
		}
	}
}
