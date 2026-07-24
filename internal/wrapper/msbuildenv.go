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

		"DisableRegistryUse": "true",
		"VCToolsVersion":     cfg.MSVCVer,
		"VsInstallRoot":      paths.BaseWin + `\`,
		"VSInstallDir":       paths.BaseWin + `\`,

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

	// Microsoft.Cpp.props resolves the compiler/toolset location through
	// VCInstallDir_<N>/VCToolsInstallDir_<N>, where <N> is whatever numeric
	// suffix the installed MSBuild toolset property sheets use (e.g.
	// .../MSBuild/Microsoft/VC/v180 -> "180"). Populate every one actually
	// present, so a project pinned to any of them resolves to the one real
	// toolchain that's installed.
	matches, _ := filepath.Glob(filepath.Join(paths.BaseUnix, "MSBuild", "Microsoft", "VC", "v*"))
	for _, m := range matches {
		sub := reToolsetDir.FindStringSubmatch(filepath.Base(m))
		if sub == nil {
			continue
		}
		env["VCInstallDir_"+sub[1]] = paths.MSVCBaseWin + `\`
		env["VCToolsInstallDir_"+sub[1]] = paths.MSVCDirWin + `\`
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
