package wrapper

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

var reToolsetDir = regexp.MustCompile(`^v(\d+)$`)

// msbuildEnv returns the extra environment variables MSBuild's own
// SDK/toolset-detection property sheets need. The generic INCLUDE/LIB/
// WINEPATH set by buildEnv are enough for cl/link/lib invoked directly, but
// MSBuild resolves the compiler location and Windows SDK through a
// different, registry-oriented mechanism - DisableRegistryUse=true
// redirects that lookup to these variables instead of a (nonexistent)
// Windows Registry.
func msbuildEnv(cfg *wineenv.Config, paths *wineenv.Paths) map[string]string {
	env := map[string]string{
		// WDK driver builds stamp the INF's DriverVer with StampInf (which
		// uses the local wall-clock date) and then validate it with
		// Inf2Cat (which checks against UTC "now"). For any timezone east
		// of UTC, local-vs-UTC disagree on the calendar date for most of
		// the day, so Inf2Cat rejects the just-stamped date as "postdated"
		// (MSB6006, "DriverVer set to a date in the future"). Forcing both
		// tools onto the same UTC clock removes the mismatch.
		"TZ": "UTC",

		// VCToolsVersion must be a real version string, not left unset:
		// Microsoft.Cpp.VCTools.props itself falls back to the literal
		// placeholder "VCToolsVersion_is_not_defined" whenever it's empty,
		// and that placeholder then reaches unconditional (not gated behind
		// CheckMSVCComponents) version-string comparisons elsewhere in
		// Microsoft.CppBuild.targets, e.g. the SegmentHeap manifest check's
		// VersionGreaterThanOrEquals(), which throws MSB4184 on a
		// non-version string.
		//
		// CheckMSVCComponents=false is what actually makes an aliased
		// PlatformToolset safe to combine with that real version: without
		// it, CheckVCToolsetVersion (Microsoft.CppBuild.targets, MSB8052)
		// rejects the combination whenever VCToolsVersion's numeric
		// generation doesn't match PlatformToolset's - exactly the legacy-
		// project case toolsetSuffixes/aliasPlatformToolsets exist for (a
		// v142 project against the one real, newer compiler actually
		// installed). Everything else CheckMSVCComponents gates
		// (Microsoft.CppBuild.targets ~495-535) is diagnostic-only - MFC/ATL/
		// Spectre component presence warnings, none of it feeding into the
		// actual compile/link - so disabling it costs nothing here.
		"DisableRegistryUse":  "true",
		"CheckMSVCComponents": "false",
		"VCToolsVersion":      cfg.MSVCVer,
		"VsInstallRoot":       paths.BaseWin + `\`,
		"VSInstallDir":        paths.BaseWin + `\`,

		"SDKReferenceDirectoryRoot":                 paths.BaseWin + `\`,
		"SDKExtensionDirectoryRoot":                 paths.BaseWin + `\`,
		"MSBUILDSDKREFERENCEDIRECTORY":              paths.BaseWin + `\`,
		"MSBUILDMULTIPLATFORMSDKREFERENCEDIRECTORY": paths.BaseWin + `\`,

		"WindowsSdkDir_10":             paths.SDKBaseWin + `\`,
		"UniversalCRTSdkDir_10":        paths.SDKBaseWin + `\`,
		"WindowsSdkDir":                paths.SDKBaseWin + `\`,
		"UniversalCRTSdkDir":           paths.SDKBaseWin + `\`,
		"WindowsTargetPlatformVersion": cfg.SDKVer,
		"UCRTContentRoot":              paths.SDKBaseWin + `\`,
		"NETFXKitsDir":                 paths.SDKBaseWin + `\`,
		"NETFXSDKDir":                  paths.SDKBaseWin + `\`,

		// WDK-specific properties; harmless when not building a driver.
		"WDKKitVersion":            "10",
		"Driver_SpectreMitigation": "false",
		"SignMode":                 "off",
		"Inf2CatNoCatalog":         "true",
		"ApiValidator_Enable":      "False",

		"Platform": msbuildPlatform(cfg.Arch),
	}

	// VCInstallDir_<N>/VCToolsInstallDir_<N> are consulted under two
	// completely different numbering schemes, both needing the single real
	// toolchain behind every <N> they might ask for:
	//
	//  - Microsoft.Cpp.Default.props keys its early "is this toolset even
	//    installed" check (the one MSB8020 comes from) off <N> = the
	//    PlatformToolset suffix a .vcxproj actually declares (v142, v143,
	//    v145, ...) - the same short name Microsoft stamps on
	//    VC/Auxiliary/Build/Microsoft.VCToolsVersion.v<N>.default.props for
	//    the downloaded compiler.
	//  - Microsoft.CppBuild.targets (MSB8070) instead keys off <N> = the
	//    MSBuild targets-schema version whose Microsoft.Cpp.props ended up
	//    imported for this run (MSBuild/Microsoft/VC/v150|v160|v170|v180 -
	//    fixed, shipped identically with every MSBuild release, unrelated to
	//    which compiler is installed), to locate the specific toolset
	//    version subfolder.
	//
	// Populate every <N> from both sources, all pointing at the one real
	// toolchain that's installed, so a project pinned to any PlatformToolset
	// resolves at every stage MSBuild checks it.
	for _, n := range toolsetSuffixes(paths.BaseUnix) {
		env["VCInstallDir_"+n] = paths.MSVCBaseWin + `\`
		env["VCToolsInstallDir_"+n] = paths.MSVCDirWin + `\`
	}

	if strings.HasSuffix(paths.MSBuildBinDir, "amd64") {
		env["PreferredToolArchitecture"] = "x64"
	}

	// The WindowsKernelModeDriver10.0/WindowsUserModeDriver10.0
	// PlatformToolsets (registered by `download --with-wdk`, see
	// internal/download/wdk.go) resolve WDKContentRoot through the
	// (nonexistent, under Wine) registry unless it's already set - same
	// DisableRegistryUse workaround as WindowsSdkDir_10 above. WDKBuildFolder
	// picks the per-SDK-build subtree (c/build/<ver>/...) the NuGet
	// package's content is organized under.
	wdkContentRoot := filepath.Join(paths.BaseUnix, "wdk", cfg.Arch, "c")
	if fi, err := os.Stat(wdkContentRoot); err == nil && fi.IsDir() {
		env["WDKContentRoot"] = wineenv.ToWinPath(wdkContentRoot) + `\`
		env["WDKBuildFolder"] = cfg.SDKVer
	}

	return env
}

// toolsetSuffixes collects every numeric <N> that either VCInstallDir_<N>
// lookup mechanism (see msbuildEnv) might be asked to resolve for this
// installation: PlatformToolset short names from
// vc/Auxiliary/Build/Microsoft.VCToolsVersion.v<N>.default.props, MSBuild
// targets-schema versions from MSBuild/Microsoft/VC/v<N>, and every
// historical PlatformToolset name (see wineenv.KnownPlatformToolsets) - a
// project pinned to any of them all resolves to the one real toolchain
// installed.
func toolsetSuffixes(baseUnix string) []string {
	seen := map[string]bool{}
	var suffixes []string

	record := func(n string) {
		if seen[n] {
			return
		}
		seen[n] = true
		suffixes = append(suffixes, n)
	}

	add := func(dir, prefix, suffix string) {
		matches, _ := filepath.Glob(filepath.Join(dir, "*"))
		for _, m := range matches {
			name := filepath.Base(m)
			if prefix != "" {
				if !strings.HasPrefix(name, prefix) {
					continue
				}
				name = strings.TrimPrefix(name, prefix)
			}
			name = strings.TrimSuffix(name, suffix)
			sub := reToolsetDir.FindStringSubmatch(name)
			if sub == nil {
				continue
			}
			record(sub[1])
		}
	}

	add(filepath.Join(baseUnix, "vc", "Auxiliary", "Build"), "Microsoft.VCToolsVersion.", ".default.props")
	add(filepath.Join(baseUnix, "MSBuild", "Microsoft", "VC"), "", "")
	for _, n := range wineenv.KnownPlatformToolsets {
		record(n)
	}

	return suffixes
}

// reGlobalProp matches an MSBuild global-property command-line switch
// ("/p:Name=...", "-property:Name=...", case-insensitive on both the
// -p/-property spelling and the property name) so msbuildGlobalArgs can tell
// whether the caller already pinned a given property themselves.
func reGlobalProp(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)^[-/](p|property):` + regexp.QuoteMeta(name) + `=`)
}

// msbuildGlobalArgs returns /p: switches to prepend to an MSBuild invocation,
// one per forced property not already present in args.
//
// WindowsTargetPlatformVersion is the one property that needs this rather
// than an env var: unlike VCInstallDir_<N> (an input to a props-file
// *lookup*, so any value msbuildEnv sets is visible no matter what a project
// pins its PlatformToolset to), WindowsTargetPlatformVersion is itself the
// value most legacy .vcxproj files hardcode directly in a PropertyGroup -
// and an explicit PropertyGroup assignment always wins over an inherited
// environment variable of the same name. A command-line global property is
// the one thing a project file can't override, which is exactly what's
// needed here: vintner only ever installs one Windows SDK version, so - same
// reasoning as the PlatformToolset fallback above - any project should
// transparently build against that one installed version rather than fail
// outright over an exact version string it happened to be pinned to when
// last saved from a real Windows SDK selector dropdown.
func msbuildGlobalArgs(cfg *wineenv.Config, args []string) []string {
	forced := map[string]string{
		"WindowsTargetPlatformVersion": cfg.SDKVer,
	}

	var out []string
	for name, value := range forced {
		re := reGlobalProp(name)
		alreadySet := false
		for _, a := range args {
			if re.MatchString(a) {
				alreadySet = true
				break
			}
		}
		if !alreadySet {
			out = append(out, "/p:"+name+"="+value)
		}
	}
	return out
}

func msbuildPlatform(arch string) string {
	switch arch {
	case "x86":
		return "Win32"
	case "arm":
		return "ARM"
	case "arm64":
		return "ARM64"
	default:
		return arch
	}
}
