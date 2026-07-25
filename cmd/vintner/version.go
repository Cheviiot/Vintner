package main

import (
	"fmt"
	"runtime/debug"
)

// versionString renders "vintner <version>" plus, when available, the git
// commit and build time Go's toolchain embeds automatically (since Go
// 1.18, `go build` stamps vcs.revision/vcs.time/vcs.modified into the
// binary on its own - no -ldflags needed for this part, so it works the
// same whether the binary came from CI or a plain local `go build`).
// Knowing the exact commit a reported bug was built from, not just the
// X.Y.Z tag, is the point: two builds of the same tag could still differ
// if the tag was ever moved, or if someone built from an uncommitted tree.
//
// Note for anyone testing this: `go build` stamps vcs.* build settings,
// but `go test` binaries don't get them - there's no environment where a
// `go test` run can exercise the revision-formatting branch below, hence
// formatVersion is split out and tested directly instead.
func versionString() string {
	revision, buildTime, dirty := "", "", false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.time":
				buildTime = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
	}
	return formatVersion(version, revision, buildTime, dirty)
}

// formatVersion is the pure part of versionString: given a revision (full
// git SHA, may be empty), it's shortened to 12 chars and marked "-dirty" if
// the build tree had uncommitted changes.
func formatVersion(ver, revision, buildTime string, dirty bool) string {
	v := "vintner " + ver
	if revision == "" {
		return v
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if dirty {
		revision += "-dirty"
	}
	if buildTime != "" {
		return fmt.Sprintf("%s (%s, %s)", v, revision, buildTime)
	}
	return fmt.Sprintf("%s (%s)", v, revision)
}
