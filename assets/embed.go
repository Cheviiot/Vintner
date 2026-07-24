// Package assets embeds vendored files that don't belong in Go source: the
// Wine compatibility patches applied after unpacking MSVC/WinSDK, and the
// toolrelay.cpp native launcher helper (see internal/install and
// internal/wrapper).
package assets

import "embed"

//go:embed patches
var Patches embed.FS

//go:embed vendor/toolrelay.cpp
var ToolRelaySource []byte
