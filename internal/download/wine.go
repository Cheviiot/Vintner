package download

import (
	"archive/zip"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

// vintner ships its own minimal, purpose-built Wine instead of depending on
// a system package: console-only MSVC tools (cl/link/lib/rc/midl/mc/mt/
// dumpbin/ml/ml64/nmake/armasm/MSBuild) need none of Wine's X11/audio/
// video/Vulkan/USB/Bluetooth/32-bit-host stack, and building without those
// removes the corresponding runtime library dependencies entirely (`ldd` on
// the resulting wine/wineserver shows only libc.so.6) - see the
// reflective-orbiting-sprout plan for the full configure flag list and how
// this was validated (a real Ogre3D CMake+Ninja+MSBuild build, twice, once
// through raw wine and once through vintner's own CLI end to end).
//
// MSBuild.exe needs Wine Mono bundled alongside it too, despite the real
// Microsoft .NET runtime already being downloaded with MSBuild itself:
// Wine's own mscoree.dll intercepts any PE with a CLR header at the loader
// level and requires a matching Wine Mono version before it ever hands
// control to the real runtime - confirmed the hard way (MSBuild.exe failed
// outright with "Wine Mono is not installed" until a matching wine-mono was
// placed under the build's own share/wine/mono/). This artifact bundles
// both wine and wine-mono together as one download, already laid out
// correctly (share/wine/mono/wine-mono-<ver>/) - nothing needs assembling
// after extraction.

// wineArtifactURLTemplate is where vintner's own CI publishes the bundled
// wine build (see the plan's Phase 3) - %s is the Go GOARCH ("amd64"/
// "arm64") of the *host* running vintner, not any MSVC target arch.
const wineArtifactURLTemplate = "https://github.com/Cheviiot/vintner/releases/download/wine-v1/vintner-wine-linux-%s.zip"

// wineSHA256 pins the exact artifact this vintner build expects, per host
// arch - a map (not consts) so tests can point it at a small fake payload
// instead of the real ~100MB+ build.
//
// Empty until Phase 3 actually builds and publishes the CI artifact (see
// the reflective-orbiting-sprout plan) - DownloadBundledWine fails closed
// with a clear error for an arch with no known hash rather than skipping
// verification, and ensureBundledWine (internal/install) treats that as a
// best-effort miss, falling back to system wine exactly like "wine not
// found" does today.
var wineSHA256 = map[string]string{}

// DownloadBundledWine fetches (or reuses a cached copy of) vintner's own
// minimal wine+Wine Mono build for arch into cacheDir, then unpacks it into
// home/wine/wineenv.BundledWineVersion. Returns that directory.
func DownloadBundledWine(arch, cacheDir, home string) (string, error) {
	sum, ok := wineSHA256[arch]
	if !ok {
		return "", fmt.Errorf("no bundled wine artifact known for arch %q yet", arch)
	}

	artifactURL := fmt.Sprintf(wineArtifactURLTemplate, arch)
	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("vintner-wine-%s-%s.zip", wineenv.BundledWineVersion, arch))
	if !isFile(cacheFile) {
		fmt.Println("Downloading vintner's bundled wine (" + wineenv.BundledWineVersion + ")")
		if err := httpDownloadFile(artifactURL, cacheFile); err != nil {
			return "", fmt.Errorf("downloading bundled wine: %w", err)
		}
	} else {
		fmt.Println("Using existing file", filepath.Base(cacheFile))
	}
	got, err := sha256File(cacheFile)
	if err != nil {
		return "", err
	}
	if !equalFoldHex(got, sum) {
		return "", fmt.Errorf("incorrect hash for downloaded file %s, aborting", filepath.Base(cacheFile))
	}

	wineDir := filepath.Join(home, "wine", wineenv.BundledWineVersion)
	if err := extractZipTree(cacheFile, wineDir); err != nil {
		return "", fmt.Errorf("unpacking bundled wine: %w", err)
	}
	return wineDir, nil
}

// extractZipTree extracts every entry of file into dest, preserving each
// entry's mode (needed here so bin/wine, bin/wineserver, etc. keep their
// executable bit - unlike extractVSIXPackage's siblings, there's no
// Contents/$MSBuild subfolder indirection to unwrap: this archive's root
// *is* the tree to install, laid out exactly as home/wine/<version>/
// expects it.
func extractZipTree(file, dest string) error {
	r, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	for _, f := range r.File {
		name, err := url.PathUnescape(f.Name)
		if err != nil {
			name = f.Name
		}
		name = strings.ReplaceAll(name, `\`, "/")
		target, err := safeJoin(dest, name)
		if err != nil {
			return fmt.Errorf("extracting %s: %w", file, err)
		}
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
