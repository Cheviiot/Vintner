package wrapper

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

// toolchainsEnvVar lists extra <dest>/bin/<arch> directories (same
// granularity as VINTNER_BIN), separated the same way PATH is, that
// selectToolchainDir may switch an msbuild invocation onto when the project
// being built pins a PlatformToolset none of them match the primary
// toolchain's real one.
const toolchainsEnvVar = "VINTNER_TOOLCHAINS"

var reProjectArg = regexp.MustCompile(`(?i)\.(sln|vcxproj)$`)

// findProjectArg returns the first argument ending in .sln or .vcxproj,
// matching how msbuild itself treats its one project-file argument. Matched
// by suffix alone, not by ruling out a leading -/switch character first: an
// absolute Unix path (a realistic way to invoke `vintner msbuild` outside
// Windows-style relative paths) starts with "/" too, and no real MSBuild
// switch ends in ".sln"/".vcxproj". Returns "" if args names no project
// file (e.g. msbuild relying on its own cwd-search default, which this
// doesn't attempt to replicate).
func findProjectArg(args []string) string {
	for _, a := range args {
		if reProjectArg.MatchString(a) {
			return a
		}
	}
	return ""
}

var reSlnProject = regexp.MustCompile(`(?i)Project\("\{[^}]+\}"\)\s*=\s*"[^"]*",\s*"([^"]+\.vcxproj)"`)

// slnProjectPaths extracts every .vcxproj path a .sln file references,
// resolved relative to the .sln's own directory - a plain regex over the
// solution file's project-declaration lines, not a full .sln parser.
func slnProjectPaths(slnPath string) ([]string, error) {
	data, err := os.ReadFile(slnPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(slnPath)
	var paths []string
	for _, m := range reSlnProject.FindAllSubmatch(data, -1) {
		paths = append(paths, filepath.Join(dir, toOSPath(string(m[1]))))
	}
	return paths, nil
}

// toOSPath converts a Windows-style path (backslash separators, as commonly
// written in .sln/.vcxproj files and on msbuild command lines) into a real
// path for the current OS. Vintner only ever runs on a Unix host, so this
// is always a plain backslash-to-slash swap, never a no-op guarded by
// runtime.GOOS.
func toOSPath(p string) string {
	return strings.ReplaceAll(p, `\`, string(filepath.Separator))
}

var rePlatformToolset = regexp.MustCompile(`(?i)<PlatformToolset>\s*v(\d+)\s*</PlatformToolset>`)

// vcxprojPlatformToolsets returns every distinct PlatformToolset short name
// (e.g. "142") a .vcxproj's raw text declares.
//
// This deliberately ignores MSBuild Condition attributes tied to a specific
// Configuration|Platform: a plain regex over the file's text can't evaluate
// those, so a project pinning different toolsets per-configuration will
// surface as multiple distinct values here. requestedPlatformToolset treats
// that ambiguity as "don't know" rather than guessing.
func vcxprojPlatformToolsets(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range rePlatformToolset.FindAllSubmatch(data, -1) {
		v := string(m[1])
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, nil
}

// requestedPlatformToolset determines the single PlatformToolset short name
// projectArg (a .sln or .vcxproj path) pins, or "" if that isn't
// unambiguous: no PlatformToolset found anywhere, more than one distinct
// value found (across a solution's projects, or within one file's
// Condition-gated PropertyGroups - see vcxprojPlatformToolsets), or the file
// couldn't be read/parsed at all. Callers treat "" as "don't switch
// toolchains", never as an error to report.
func requestedPlatformToolset(projectArg string) string {
	// Args reach here exactly as the caller wrote them, commonly in
	// Windows path style (backslashes) even though vintner and this parsing
	// always run on a Unix host (it wraps Wine-hosted tools, never runs
	// under native Windows) - normalize before treating it as a real
	// filesystem path to read, the same as slnProjectPaths already does for
	// the relative paths it finds inside a .sln.
	projectArg = toOSPath(projectArg)

	var files []string
	if strings.EqualFold(filepath.Ext(projectArg), ".sln") {
		paths, err := slnProjectPaths(projectArg)
		if err != nil {
			return ""
		}
		files = paths
	} else {
		files = []string{projectArg}
	}

	seen := map[string]bool{}
	var distinct []string
	for _, f := range files {
		toolsets, err := vcxprojPlatformToolsets(f)
		if err != nil {
			continue // unreadable project referenced by the .sln - skip, not fatal
		}
		for _, v := range toolsets {
			if !seen[v] {
				seen[v] = true
				distinct = append(distinct, v)
			}
		}
		if len(distinct) > 1 {
			// Already ambiguous - the result is settled ("") no matter
			// what the rest of files contains, so stop reading/parsing
			// them. Matters for a solution with many projects: a real
			// build can reference dozens, and each one costs a full
			// os.ReadFile + regex pass (vcxprojPlatformToolsets).
			return ""
		}
	}
	if len(distinct) != 1 {
		return ""
	}
	return distinct[0]
}

// selectToolchainDir picks which <dest>/bin/<arch> directory an msbuild
// invocation should actually use: primaryBinDir (today's sole choice -
// VINTNER_BIN or the default toolchain dir) unless VINTNER_TOOLCHAINS names
// an alternate whose real PlatformToolset (recorded in env.json at install
// time, see internal/install's aliasPlatformToolsets) exactly matches what
// args' project file pins and primaryBinDir's own real toolset doesn't.
// Assigned as the "msbuild" spec's resolveScriptDir (tools.go), so Run()
// (run.go) calls it in place of its own default resolution.
//
// The returned cfg is whichever env.json this function already had to
// wineenv.Load to make its decision (primaryBinDir's own, or the matched
// candidate's) - non-nil in every path that actually loaded one, so Run()
// can skip re-loading the same file a second time. nil only on the
// zero-cost early-return paths below, where nothing was loaded at all.
//
// Returns primaryBinDir unchanged, with note == "", whenever:
// VINTNER_TOOLCHAINS is unset (the common case - zero cost, zero behavior
// change); args names no project file; the project's PlatformToolset isn't
// unambiguous; primaryBinDir already matches; or no candidate matches
// either (today's aliasing fallback still applies, exactly as before this
// existed).
func selectToolchainDir(primaryBinDir string, args []string) (binDir string, cfg *wineenv.Config, note string) {
	extra := os.Getenv(toolchainsEnvVar)
	if extra == "" {
		return primaryBinDir, nil, ""
	}

	projectArg := findProjectArg(args)
	if projectArg == "" {
		return primaryBinDir, nil, ""
	}

	wanted := requestedPlatformToolset(projectArg)
	if wanted == "" {
		return primaryBinDir, nil, ""
	}

	primaryCfg, err := wineenv.Load(primaryBinDir)
	if err == nil && primaryCfg.PlatformToolset == wanted {
		return primaryBinDir, primaryCfg, ""
	}

	for _, candidate := range filepath.SplitList(extra) {
		candCfg, err := wineenv.Load(candidate)
		if err != nil || candCfg.PlatformToolset != wanted {
			continue
		}
		primaryReal := "unknown"
		if primaryCfg != nil && primaryCfg.PlatformToolset != "" {
			primaryReal = "v" + primaryCfg.PlatformToolset + ", aliased"
		}
		return candidate, candCfg, "vintner: " + projectArg + " pins PlatformToolset v" + wanted +
			"; using toolchain at " + candidate + " (real v" + wanted +
			") instead of default (" + primaryReal + ")"
	}

	// primaryCfg may be nil here (wineenv.Load(primaryBinDir) failed) - the
	// caller's own fallback wineenv.Load(primaryBinDir) will surface that
	// same error properly instead of this function swallowing it silently.
	return primaryBinDir, primaryCfg, ""
}
