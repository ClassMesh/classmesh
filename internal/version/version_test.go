package version

import (
	"runtime/debug"
	"testing"
)

func setMetadata(t *testing.T, version, commit, date string) {
	t.Helper()
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	Version, Commit, Date = version, commit, date
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
	})
}

func TestApplyBuildInfoFillsDefaults(t *testing.T) {
	setMetadata(t, "dev", "none", "unknown")
	applyBuildInfo(&debug.BuildInfo{
		Main: debug.Module{Version: "v0.2.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-07-17T10:00:00Z"},
		},
	})
	if Version != "v0.2.0" || Commit != "0123456789abcdef" || Date != "2026-07-17T10:00:00Z" {
		t.Fatalf("metadata = %q, %q, %q", Version, Commit, Date)
	}
}

func TestApplyBuildInfoKeepsLinkerValues(t *testing.T) {
	setMetadata(t, "v9.0.0", "linked-commit", "linked-date")
	applyBuildInfo(&debug.BuildInfo{
		Main: debug.Module{Version: "v0.2.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "build-commit"},
			{Key: "vcs.time", Value: "build-date"},
		},
	})
	if Version != "v9.0.0" || Commit != "linked-commit" || Date != "linked-date" {
		t.Fatalf("metadata = %q, %q, %q", Version, Commit, Date)
	}
}

func TestApplyBuildInfoIgnoresUnavailableValues(t *testing.T) {
	setMetadata(t, "dev", "none", "unknown")
	applyBuildInfo(&debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision"},
			{Key: "vcs.time"},
		},
	})
	if Version != "dev" || Commit != "none" || Date != "unknown" {
		t.Fatalf("metadata = %q, %q, %q", Version, Commit, Date)
	}
}
