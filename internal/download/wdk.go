package download

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The actual driver headers/libs (ntddk.h, wdf, km/um import libs) aren't
// part of the VS installer manifest at all - Component.Microsoft.Windows.
// DriverKit.BuildTools (see select.go) only registers the
// WindowsKernelModeDriver10.0/WindowsUserModeDriver10.0 PlatformToolsets in
// the MSBuild tree. The content those toolsets actually need ships as a
// separate per-architecture NuGet package, independently of the vsman
// pipeline: https://www.nuget.org/packages/Microsoft.Windows.WDK.x64 (and
// .ARM64). This file fetches and unpacks that package directly through the
// NuGet v3 flat-container API, without needing nuget.exe or a project
// restore.

const nugetFlatContainer = "https://api.nuget.org/v3-flatcontainer"

// WDKNuGetID returns the nuget.org package id providing WDK content for
// arch ("x86"/"x64" both use the x64 package - there's no 32-bit WDK
// package - "arm64" uses the ARM64 one).
func WDKNuGetID(arch string) string {
	if arch == "arm64" {
		return "Microsoft.Windows.WDK.ARM64"
	}
	return "Microsoft.Windows.WDK.x64"
}

// nugetVersionIndex is the "versions" list nuget.org's flat-container index
// returns, oldest first.
type nugetVersionIndex struct {
	Versions []string `json:"versions"`
}

var reNuGetPrerelease = regexp.MustCompile(`-`)

// FetchLatestWDKVersion returns the newest stable (non-prerelease) version
// of arch's WDK NuGet package, preferring one whose version starts with
// sdkBuild (e.g. "10.0.26100", to match an already-selected Windows SDK) if
// any such version exists; otherwise it returns the newest stable version
// overall.
func FetchLatestWDKVersion(arch, sdkBuild string) (string, error) {
	id := strings.ToLower(WDKNuGetID(arch))
	data, err := httpGet(nugetFlatContainer + "/" + id + "/index.json")
	if err != nil {
		return "", fmt.Errorf("listing %s versions: %w", WDKNuGetID(arch), err)
	}
	var idx nugetVersionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return "", fmt.Errorf("parsing %s version index: %w", WDKNuGetID(arch), err)
	}

	var latestMatching, latestAny string
	for _, v := range idx.Versions {
		if reNuGetPrerelease.MatchString(v) {
			continue
		}
		latestAny = v
		if sdkBuild != "" && strings.HasPrefix(v, sdkBuild) {
			latestMatching = v
		}
	}
	if latestMatching != "" {
		return latestMatching, nil
	}
	if latestAny != "" {
		return latestAny, nil
	}
	return "", fmt.Errorf("no stable release of %s found", WDKNuGetID(arch))
}

// SDKBuildPrefix extracts the "10.0.26100"-style build prefix from a
// selected Win10SDK/Win11SDK package's version (e.g. "10.0.26100.1742" ->
// "10.0.26100"), used to pick a matching WDK NuGet version. Returns "" if
// selected contains no such package.
func SDKBuildPrefix(selected []*Package) string {
	for _, p := range selected {
		id := strings.ToLower(p.ID)
		if strings.HasPrefix(id, "win10sdk") || strings.HasPrefix(id, "win11sdk") {
			parts := strings.SplitN(p.Version, ".", 4)
			if len(parts) >= 3 {
				return strings.Join(parts[:3], ".")
			}
		}
	}
	return ""
}

// DownloadWDK fetches (or reuses a cached copy of) arch's WDK NuGet package
// at version, then unpacks its "c" directory - headers, libs, and the
// per-SDK-build MSBuild props/targets the DriverKit.BuildTools PlatformToolset
// registration imports via $(WDKContentRoot) - into destDir/wdk/<arch>/c.
// Kept as its own self-contained tree rather than merged into the regular
// SDK dirs, matching how $(WDKContentRoot) is meant to be used.
//
// vsVersion is the installed MSBuild's own $(VisualStudioVersion) (e.g.
// "18.0"): the package's WindowsDriver.common.targets loads its custom
// build tasks from an assembly named for that property
// (Microsoft.DriverKit.Build.Tasks.$(VisualStudioVersion).dll), but the
// package only ships one built for whatever (older) VS generation it
// targeted - see duplicateVersionedBuildTaskAssemblies.
func DownloadWDK(arch, version, cacheDir, destDir, vsVersion string) (string, error) {
	id := WDKNuGetID(arch)
	idLower := strings.ToLower(id)
	nupkgURL := fmt.Sprintf("%s/%s/%s/%s.%s.nupkg", nugetFlatContainer, idLower, version, idLower, version)

	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("%s-%s.nupkg", idLower, version))
	if !isFile(cacheFile) {
		fmt.Printf("Downloading %s %s\n", id, version)
		if err := httpDownloadFile(nupkgURL, cacheFile); err != nil {
			return "", fmt.Errorf("downloading %s %s: %w", id, version, err)
		}
	} else {
		fmt.Printf("Using existing file %s\n", filepath.Base(cacheFile))
	}

	wdkDir := filepath.Join(destDir, "wdk", arch)
	cDir := filepath.Join(wdkDir, "c")
	if err := extractNuGetPackageDir(cacheFile, "c/", cDir); err != nil {
		return "", fmt.Errorf("unpacking %s %s: %w", id, version, err)
	}
	if err := duplicateVersionedBuildTaskAssemblies(cDir, vsVersion); err != nil {
		return "", fmt.Errorf("adapting %s %s to VisualStudioVersion %s: %w", id, version, vsVersion, err)
	}
	if err := fillMissingHostTools(cDir); err != nil {
		return "", fmt.Errorf("filling in missing x86 host tools for %s %s: %w", id, version, err)
	}
	return wdkDir, nil
}

// fillMissingHostTools copies every file from each cDir/bin/<sdkver>/x64
// directory into its sibling .../x86 directory, adding whatever's missing
// without overwriting anything already there. The WDK NuGet package's x86
// host-tool directory only ships a handful of genuinely x86-specific files
// (Inf2Cat.exe among them); most driver build tools (stampinf.exe included)
// only ship as x64 binaries, but WindowsDriver.Common.targets hardcodes an
// x86 tool path (WDKBinRoot_x86) with no fallback. This isn't "faking" an
// x86 binary - it's just running the real x64 PE binaries from a directory
// MSBuild happens to call "x86"; Wine doesn't care what the folder is
// named, only the PE header when it execs the file, and we're always on an
// x64 host/Wine setup here.
func fillMissingHostTools(cDir string) error {
	binDir := filepath.Join(cDir, "bin")
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		x64Dir := filepath.Join(binDir, e.Name(), "x64")
		if !isDir(x64Dir) {
			continue
		}
		x86Dir := filepath.Join(binDir, e.Name(), "x86")
		if err := os.MkdirAll(x86Dir, 0o755); err != nil {
			return err
		}
		files, err := os.ReadDir(x64Dir)
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			dst := filepath.Join(x86Dir, f.Name())
			if isFile(dst) {
				continue
			}
			if err := copyFile(filepath.Join(x64Dir, f.Name()), dst, 0o755); err != nil {
				return err
			}
		}
	}
	return nil
}

var reVersionedDLL = regexp.MustCompile(`^(.*)\.\d+\.\d+\.dll$`)

// duplicateVersionedBuildTaskAssemblies copies every "*.<oldver>.dll" file
// under cDir/build (the driver build task assemblies, e.g.
// Microsoft.DriverKit.Build.Tasks.17.0.dll) to a sibling
// "*.<targetVSVersion>.dll" when one doesn't already exist. The WDK NuGet
// package bundles these named for whatever VS generation it was built
// against; our installed MSBuild may be newer and looks them up by its own
// $(VisualStudioVersion). The assemblies aren't VS-version-specific in
// behavior - only the file name encodes an expected caller - so serving the
// same bytes under the new name is safe.
func duplicateVersionedBuildTaskAssemblies(cDir, targetVSVersion string) error {
	buildDir := filepath.Join(cDir, "build")
	if !isDir(buildDir) {
		return nil
	}
	return filepath.WalkDir(buildDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		m := reVersionedDLL.FindStringSubmatch(d.Name())
		if m == nil || strings.HasSuffix(d.Name(), "."+targetVSVersion+".dll") {
			return nil
		}
		target := filepath.Join(filepath.Dir(path), m[1]+"."+targetVSVersion+".dll")
		if isFile(target) {
			return nil
		}
		return copyFile(path, target, 0o644)
	})
}

// extractNuGetPackageDir extracts every entry of nupkgFile whose name starts
// with prefix into dest, stripping prefix from each resulting path.
func extractNuGetPackageDir(nupkgFile, prefix, dest string) error {
	r, err := zip.OpenReader(nupkgFile)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, f := range r.File {
		name, err := url.PathUnescape(f.Name)
		if err != nil {
			name = f.Name
		}
		name = strings.ReplaceAll(name, `\`, "/")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, rel)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractZipEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}
