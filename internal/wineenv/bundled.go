package wineenv

import (
	"os"
	"path/filepath"
)

// BundledWineVersion pins the exact vintner-built wine (plus matching Wine
// Mono, needed for MSBuild.exe - see internal/download/wine.go's doc
// comment) this vintner build expects, and names the directory it's
// installed under (home/wine/<BundledWineVersion>/) - bumping it is how a
// vintner upgrade signals "fetch a newer bundled wine", the same way
// wineenv.Config's MSVCVer/SDKVer key the MSVC/SDK trees.
const BundledWineVersion = "11.14-mono11.2.0"

// DefaultHome returns vintner's own root directory (~/.vintner) -
// independent of any particular toolchain install directory (a user can
// have several, e.g. side by side for different PlatformToolset
// generations - see toolchain_select.go), since the bundled wine and its
// prefix are shared across all of them, not tied to one.
func DefaultHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vintner"), nil
}

// FindBundledWine reports the path to home's bundled wine binary
// (home/wine/BundledWineVersion/bin/wine) if it's actually there, so a
// stale bundled wine from before a vintner upgrade bumped
// BundledWineVersion is correctly treated as absent rather than used.
func FindBundledWine(home string) (string, bool) {
	p := filepath.Join(home, "wine", BundledWineVersion, "bin", "wine")
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return "", false
	}
	return p, true
}

// WineSource identifies where ResolveWine's returned wine binary came from.
type WineSource int

const (
	WineSourceOverride WineSource = iota // VINTNER_WINE
	WineSourceBundled                    // home/wine/<BundledWineVersion>
	WineSourceSystem                     // wine64/wine on PATH
)

func (s WineSource) String() string {
	switch s {
	case WineSourceOverride:
		return "VINTNER_WINE"
	case WineSourceBundled:
		return "bundled"
	default:
		return "system"
	}
}

// ResolveWine finds the wine binary to use, in priority order: VINTNER_WINE
// (an explicit override), then home's bundled wine, then FindWine's plain
// PATH lookup (wine64/wine) - and reports which one won, so a caller can
// decide things a plain path can't answer alone, e.g. whether to isolate
// WINEPREFIX to home/wineprefix (only makes sense for the bundled wine -
// see buildEnv in internal/wrapper/run.go) or leave it inherited (system
// wine, or an explicit VINTNER_WINE override the caller presumably knows
// how to configure themselves).
func ResolveWine(home string) (path string, source WineSource, err error) {
	if p := os.Getenv("VINTNER_WINE"); p != "" {
		return p, WineSourceOverride, nil
	}
	if p, ok := FindBundledWine(home); ok {
		return p, WineSourceBundled, nil
	}
	p, err := FindWine()
	if err != nil {
		return "", 0, err
	}
	return p, WineSourceSystem, nil
}

// BundledWinePrefix returns home's isolated WINEPREFIX for wine resolved
// from home's bundled build. Kept separate from whatever prefix a system
// or VINTNER_WINE-overridden wine might already be using: a different wine
// version touching the same prefix can silently corrupt its format on
// either side (an upgrade-triggered auto-migration, or the reverse) -
// confirmed the hard way as a version-mismatch failure when testing the
// bundled build against a prefix a stray system-wine invocation had also
// touched.
func BundledWinePrefix(home string) string {
	return filepath.Join(home, "wineprefix")
}

// WinePrefixEnv returns the "WINEPREFIX=..." env entry to add for invoking
// wine resolved via ResolveWine(home, ...) - home's isolated prefix when
// source is WineSourceBundled and the caller hasn't already set WINEPREFIX
// themselves (an explicit WINEPREFIX always wins, matching how buildEnv in
// internal/wrapper/run.go treats every other WINE* override), nil
// otherwise - system/override wine keeps today's inherited-from-environment
// behavior unchanged.
func WinePrefixEnv(home string, source WineSource) []string {
	if source != WineSourceBundled {
		return nil
	}
	if _, set := os.LookupEnv("WINEPREFIX"); set {
		return nil
	}
	return []string{"WINEPREFIX=" + BundledWinePrefix(home)}
}
