package wineenv

import (
	"os"
	"path/filepath"
	"strings"
)

// Paths holds every path/env-var derived from a Config plus the on-disk
// installation root - INCLUDE, LIB, WINEPATH and friends, in the Windows
// notation Wine expects.
type Paths struct {
	BaseUnix      string // <dest>, absolute unix path
	MSVCDirUnix   string // <dest>/vc/tools/msvc/<ver>
	BinDir        string // <dest>/vc/tools/msvc/<ver>/bin/Host<host>/<arch> - cl/link/lib/ml/nmake/armasm live here
	SDKBinDir     string // <dest>/kits/10/bin/<sdkver>/<host> - mc/midl/mt/rc live here
	MSBuildBinDir string // <dest>/MSBuild/Current/Bin/<dotnetHost> - MSBuild.exe lives here

	Include          string
	Lib              string
	LibPath          string
	WinePath         string
	WineDLLOverrides string
}

// FindBaseUnix locates the installation root starting from the directory the
// tool wrapper was invoked from (scriptDir), searching one or two levels up
// for a "vc" entry - matching the original wrappers' `dirname "$0"`/".." walk,
// which tolerates wrappers living either directly in <dest>/bin/<arch> or
// nested one level deeper.
func FindBaseUnix(scriptDir string) (string, error) {
	base, err := filepath.Abs(filepath.Join(scriptDir, ".."))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(base, "vc")); err != nil {
		base = filepath.Join(base, "..")
	}
	return filepath.Abs(base)
}

// NewPaths computes all derived paths/env-vars for cfg installed at baseUnix.
func NewPaths(cfg *Config, baseUnix string) *Paths {
	winBase := "z:" + toWindowsSlashes(baseUnix)
	msvcBase := winBase + `\vc`
	sdkBase := winBase + `\kits\10`
	msvcDirWin := msvcBase + `\tools\msvc\` + cfg.MSVCVer
	sdkIncludeWin := sdkBase + `\include\` + cfg.SDKVer
	sdkLibWin := sdkBase + `\lib\` + cfg.SDKVer

	msvcDirUnix := filepath.Join(baseUnix, "vc", "tools", "msvc", cfg.MSVCVer)
	binDir := filepath.Join(msvcDirUnix, "bin", "Host"+cfg.Host, cfg.Arch)
	sdkBinDir := filepath.Join(baseUnix, "kits", "10", "bin", cfg.SDKVer, cfg.Host)
	msbuildBinDir := filepath.Join(baseUnix, "MSBuild", "Current", "Bin", cfg.DotnetHost)

	include := strings.Join([]string{
		msvcDirWin + `\atlmfc\include`,
		msvcDirWin + `\include`,
		sdkIncludeWin + `\shared`,
		sdkIncludeWin + `\ucrt`,
		sdkIncludeWin + `\um`,
		sdkIncludeWin + `\winrt`,
		sdkIncludeWin + `\km`,
	}, ";")

	lib := strings.Join([]string{
		msvcDirWin + `\atlmfc\lib\` + cfg.Arch,
		msvcDirWin + `\lib\` + cfg.Arch,
		sdkLibWin + `\ucrt\` + cfg.Arch,
		sdkLibWin + `\um\` + cfg.Arch,
		sdkLibWin + `\km\` + cfg.Arch,
	}, ";")

	// WINEPATH: unix dirs with slashes swapped for backslashes (no drive
	// letter - Wine accepts this for locating DLLs), plus the MSVC host bin
	// dir in full windows notation, deliberately always the *host* (not
	// target) x64/arm64 tool dir for the third entry - that's where DLLs
	// like mspdbcore.dll actually live.
	winePath := strings.Join([]string{
		toWindowsSlashes(binDir),
		toWindowsSlashes(sdkBinDir),
		msvcDirWin + `\bin\Host` + cfg.Host + `\` + cfg.Host,
	}, ";")

	return &Paths{
		BaseUnix:      baseUnix,
		MSVCDirUnix:   msvcDirUnix,
		BinDir:        binDir,
		SDKBinDir:     sdkBinDir,
		MSBuildBinDir: msbuildBinDir,

		Include:          include,
		Lib:              lib,
		LibPath:          lib,
		WinePath:         winePath,
		WineDLLOverrides: "vcruntime140=n;vcruntime140_1=n",
	}
}

func toWindowsSlashes(p string) string {
	return strings.ReplaceAll(p, "/", `\`)
}

// ToWinPath prefixes a unix absolute path with the "z:" drive Wine maps the
// unix root to, converting slashes to backslashes.
func ToWinPath(unixPath string) string {
	return "z:" + toWindowsSlashes(unixPath)
}
