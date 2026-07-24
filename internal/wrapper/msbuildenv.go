package wrapper

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Cheviiot/msvc-go-wine/internal/wineenv"
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
		"DisableRegistryUse": "true",
		"VCToolsVersion":     cfg.MSVCVer,
		"VsInstallRoot":      paths.BaseWin + `\`,
		"VSInstallDir":       paths.BaseWin + `\`,

		"MicrosoftKitRoot":                          paths.BaseWin + `\`,
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
