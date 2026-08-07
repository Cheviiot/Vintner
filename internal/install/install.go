// Package install wires up a downloaded MSVC/WinSDK tree so the vintner
// wrapper commands can find it: locating the installed toolchain/SDK
// versions, fixing up header/library name casing, laying out the
// per-architecture tool symlinks, and building the toolrelay helper.
package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Cheviiot/vintner/assets"
	"github.com/Cheviiot/vintner/internal/lock"
	"github.com/Cheviiot/vintner/internal/wineenv"
)

var archs = []string{"x86", "x64", "arm", "arm64"}

// Install wires up dest (a directory previously populated by
// `vintner download --dest dest`) with the tool wrapper symlinks and
// env.json config the wrapper runtime expects. selfBinary is the path to
// the currently running vintner executable, copied into dest/bin so
// the arch-specific tool symlinks have something to point at.
func Install(dest, selfBinary string) error {
	dest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(dest); err != nil || !fi.IsDir() {
		return fmt.Errorf("destination %q is not a directory", dest)
	}

	unlock, err := lock.Acquire(dest)
	if err != nil {
		return err
	}
	defer unlock()

	// Targets are relative so the whole installed tree stays relocatable -
	// moving or renaming dest doesn't break these symlinks the way an
	// absolute target baked in at install time would.
	if err := lnS("Windows Kits", filepath.Join(dest, "kits")); err != nil {
		return err
	}
	if err := lnS("VC", filepath.Join(dest, "vc")); err != nil {
		return err
	}
	if err := lnS("Tools", filepath.Join(dest, "vc", "tools")); err != nil {
		return err
	}
	if err := lnS("MSVC", filepath.Join(dest, "vc", "tools", "msvc")); err != nil {
		return err
	}

	msvcRoot := filepath.Join(dest, "vc", "tools", "msvc")
	msvcVer, err := findMSVCVersion(msvcRoot)
	if err != nil {
		return err
	}
	fmt.Println("Using MSVC version", msvcVer)
	msvcDir := filepath.Join(msvcRoot, msvcVer)

	if err := fixLibCasing(filepath.Join(msvcDir, "lib")); err != nil {
		return err
	}

	realToolset, err := aliasPlatformToolsets(dest)
	if err != nil {
		return err
	}

	includeDir := filepath.Join(msvcDir, "include")
	if err := Lowercase(includeDir, LowercaseOptions{Symlink: true}); err != nil {
		return fmt.Errorf("lowercasing %s: %w", includeDir, err)
	}
	if err := FixInclude(includeDir, FixIncludeOptions{}); err != nil {
		return fmt.Errorf("fixing includes in %s: %w", includeDir, err)
	}
	atlIncludeDir := filepath.Join(msvcDir, "atlmfc", "include")
	if isDir(atlIncludeDir) {
		if err := FixInclude(atlIncludeDir, FixIncludeOptions{}); err != nil {
			return fmt.Errorf("fixing includes in %s: %w", atlIncludeDir, err)
		}
	}

	binDir := filepath.Join(msvcDir, "bin")
	if err := removeVctip(binDir); err != nil {
		return err
	}
	if err := renameHostDirs(binDir); err != nil {
		return err
	}

	kits10 := filepath.Join(dest, "kits", "10")
	if !isDir(kits10) {
		return fmt.Errorf("%s not found - expected a Windows SDK already unpacked by `vintner download`", kits10)
	}
	if err := lnS("Lib", filepath.Join(kits10, "lib")); err != nil {
		return err
	}
	if err := lnS("Include", filepath.Join(kits10, "include")); err != nil {
		return err
	}

	sdkVer, err := findSDKVersion(filepath.Join(kits10, "include"))
	if err != nil {
		return err
	}
	fmt.Println("Using SDK version", sdkVer)

	for _, sub := range []string{"um", "shared", "winrt", "km"} {
		dir := filepath.Join(kits10, "include", sdkVer, sub)
		if !isDir(dir) {
			continue
		}
		if err := Lowercase(dir, LowercaseOptions{Symlink: true, MapWinSDK: true}); err != nil {
			return fmt.Errorf("lowercasing %s: %w", dir, err)
		}
		if err := FixInclude(dir, FixIncludeOptions{MapWinSDK: true}); err != nil {
			return fmt.Errorf("fixing includes in %s: %w", dir, err)
		}
	}
	wdfDir := filepath.Join(kits10, "include", "wdf")
	if isDir(wdfDir) {
		if err := Lowercase(wdfDir, LowercaseOptions{Symlink: true, MapWinSDK: true}); err != nil {
			return err
		}
		if err := FixInclude(wdfDir, FixIncludeOptions{MapWinSDK: true}); err != nil {
			return err
		}
	}

	for _, arch := range archs {
		for _, sub := range []string{"um", "km"} {
			dir := filepath.Join(kits10, "lib", sdkVer, sub, arch)
			if !isDir(dir) {
				continue
			}
			if err := Lowercase(dir, LowercaseOptions{Symlink: true}); err != nil {
				return fmt.Errorf("lowercasing %s: %w", dir, err)
			}
		}
	}

	host, dotnetHost := hostArch()

	modulesRel := filepath.Join("VC", "Tools", "MSVC", msvcVer, "modules")
	if isDir(filepath.Join(dest, modulesRel)) {
		if err := lnS(modulesRel, filepath.Join(dest, "modules")); err != nil {
			return err
		}
	}

	destBin := filepath.Join(dest, "bin")
	if err := os.MkdirAll(destBin, 0o755); err != nil {
		return err
	}
	sharedBinary := filepath.Join(destBin, "vintner")
	if err := copyFile(selfBinary, sharedBinary, 0o755); err != nil {
		return fmt.Errorf("installing shared binary: %w", err)
	}

	installed := 0
	for _, arch := range archs {
		clExe := filepath.Join(msvcDir, "bin", "Host"+host, arch, "cl.exe")
		if !isFile(clExe) {
			continue
		}
		if err := setupWrapperDir(destBin, selfBinary, arch, host, dotnetHost, msvcVer, sdkVer, realToolset); err != nil {
			return err
		}
		installed++
		fmt.Println("Installed tool wrappers for", arch)
	}
	if installed == 0 {
		return fmt.Errorf("no target architecture found under %s/bin/Host%s/*", msvcDir, host)
	}

	if bootstrapWine() {
		// Best-effort: if this fails, the wrapper runtime just falls back
		// to invoking tools directly through wine, without toolrelay.exe's
		// mt.exe/CMake exit-code fixup.
		if err := buildToolRelay(dest, destBin, host); err != nil {
			fmt.Println("Building toolrelay failed (continuing without it):", err)
		} else {
			fmt.Println("Build toolrelay done.")
		}
	}
	return nil
}

func findMSVCVersion(msvcRoot string) (string, error) {
	entries, err := os.ReadDir(msvcRoot)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", msvcRoot, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	// `ls -r`: reverse of the default (lexical) sort order.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		dir := filepath.Join(msvcRoot, name)
		if isDir(filepath.Join(dir, "bin")) && isDir(filepath.Join(dir, "include")) && isDir(filepath.Join(dir, "lib")) {
			return name, nil
		}
	}
	return "", fmt.Errorf("no suitable MSVC version found under %s", msvcRoot)
}

func findSDKVersion(includeDir string) (string, error) {
	entries, err := os.ReadDir(includeDir)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", includeDir, err)
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "10.") {
			versions = append(versions, e.Name())
		}
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no Windows SDK version (10.*) found under %s", includeDir)
	}
	sort.Strings(versions)
	return versions[len(versions)-1], nil
}

// fixLibCasing adds uppercase symlinks (LIBCMT.lib -> libcmt.lib, etc) so
// lld-link can resolve the /DEFAULTLIB directives cl.exe emits, which name
// these libs in upper case, on a case-sensitive filesystem.
func fixLibCasing(libDir string) error {
	names := []string{"libcmt", "libcmtd", "msvcrt", "msvcrtd", "oldnames"}
	for _, arch := range archs {
		dir := filepath.Join(libDir, arch)
		if !isDir(dir) {
			continue
		}
		for _, n := range names {
			lower := filepath.Join(dir, n+".lib")
			if !isFile(lower) {
				continue
			}
			upper := filepath.Join(dir, strings.ToUpper(n)+".lib")
			if err := lnS(n+".lib", upper); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeVctip deletes any vctip.exe found under dir: it phones home to
// Microsoft and is known to cause problems under Wine.
func removeVctip(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "vctip.exe") {
			return os.Remove(path)
		}
		return nil
	})
}

// renameHostDirs normalizes the several casings different MSVC releases
// have used for their host-arch bin directories.
func renameHostDirs(binDir string) error {
	renames := []struct{ from, to string }{
		{"HostX64", "Hostx64"},     // 15.x - 16.4
		{"HostARM64", "Hostarm64"}, // 17.2 - 17.3
		{"HostArm64", "Hostarm64"}, // 17.4
	}
	for _, r := range renames {
		from := filepath.Join(binDir, r.from)
		to := filepath.Join(binDir, r.to)
		if isDir(from) && !exists(to) {
			if err := os.Rename(from, to); err != nil {
				return err
			}
		}
	}
	oldArm64 := filepath.Join(binDir, "Hostarm64", "ARM64")
	newArm64 := filepath.Join(binDir, "Hostarm64", "arm64")
	if isDir(oldArm64) && !exists(newArm64) {
		if err := os.Rename(oldArm64, newArm64); err != nil {
			return err
		}
	}
	return nil
}

// setupWrapperDir creates <destBin>/<arch> with its own local copy of the
// vintner binary (not a symlink to the shared one in destBin), and
// symlinks every tool name to that LOCAL copy.
//
// This matters: the wrapper runtime locates its own install root via
// os.Executable(), which fully resolves symlinks (like /proc/self/exe does)
// - it can't rely on os.Args[0], since not every shell passes a
// PATH-resolved absolute path as argv[0] (some just pass the bare command
// name, e.g. "cl", which would make a naive argv[0]-based lookup resolve
// against the caller's cwd instead of the install dir). A same-directory
// symlink (cl -> vintner) resolves to a binary that's still in the
// right arch dir; a symlink to a binary one level up (cl -> ../vintner)
// would not be.
func setupWrapperDir(destBin, selfBinary, arch, host, dotnetHost, msvcVer, sdkVer, platformToolset string) error {
	archDir := filepath.Join(destBin, arch)
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		return err
	}
	localBinary := filepath.Join(archDir, "vintner")
	if err := copyFile(selfBinary, localBinary, 0o755); err != nil {
		return fmt.Errorf("installing per-arch binary: %w", err)
	}
	for name := range toolNames {
		if err := lnS("vintner", filepath.Join(archDir, name)); err != nil {
			return err
		}
		if err := lnS("vintner", filepath.Join(archDir, name+".exe")); err != nil {
			return err
		}
	}
	cfg := &wineenv.Config{
		Arch:            arch,
		Host:            host,
		DotnetHost:      dotnetHost,
		MSVCVer:         msvcVer,
		SDKVer:          sdkVer,
		PlatformToolset: platformToolset,
	}
	return cfg.Save(archDir)
}

// toolNames is the set of tool wrapper symlinks created in every arch dir.
var toolNames = map[string]bool{
	"cl": true, "link": true, "lib": true, "ml": true, "ml64": true,
	"mc": true, "midl": true, "mt": true, "rc": true, "dumpbin": true,
	"msbuild": true, "nmake": true, "armasm": true, "armasm64": true,
	"cmd": true, "findstr": true,
}

func hostArch() (host, dotnetHost string) {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64", "arm64"
	default:
		return "x64", "amd64"
	}
}

// bootstrapWine runs `wineboot --init` to set up the wine prefix ahead of
// time, and reports whether wine is available at all (so callers can decide
// whether to attempt building toolrelay.exe).
func bootstrapWine() bool {
	wine, err := wineenv.FindWine()
	if err != nil {
		fmt.Println("wine not found, skipping wineboot bootstrap and toolrelay build (install wine before running the tools)")
		return false
	}
	fmt.Println("Bootstrapping wine prefix...")
	if err := runQuiet(wine, "wineboot", "--init"); err != nil {
		fmt.Println("wineboot --init failed (continuing):", err)
	}
	return true
}

// buildToolRelay compiles the vendored toolrelay.cpp helper using the
// freshly-installed host-arch cl wrapper. toolrelay.exe is what lets the
// wrapper runtime give mt.exe's CMake-compatibility exit code
// (0x41020001 -> 0xbb) a chance to survive Wine's own exit-code truncation -
// see internal/wrapper's runViaToolRelay. Best-effort: any failure here just
// means the wrapper runtime falls back to a plain wine invocation without
// that fixup.
func buildToolRelay(dest, destBin, host string) error {
	clWrapper := filepath.Join(destBin, host, "cl")
	if !isFile(clWrapper) {
		return fmt.Errorf("no cl wrapper for host arch %s at %s", host, clWrapper)
	}

	srcPath := filepath.Join(dest, "toolrelay.cpp")
	if err := os.WriteFile(srcPath, assets.ToolRelaySource, 0o644); err != nil {
		return err
	}
	defer os.Remove(srcPath)

	fmt.Println("Build toolrelay ...")
	cmd := exec.Command(clWrapper, "/EHsc", "/O2", srcPath)
	cmd.Dir = dest
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compiling toolrelay.cpp: %w", err)
	}

	exePath := filepath.Join(dest, "toolrelay.exe")
	objPath := filepath.Join(dest, "toolrelay.obj")
	defer os.Remove(objPath)
	if !isFile(exePath) {
		return fmt.Errorf("cl did not produce %s", exePath)
	}
	return os.Rename(exePath, filepath.Join(destBin, "toolrelay.exe"))
}

func runQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "WINEDEBUG=-all")
	return cmd.Run()
}

// lnS creates the symlink link -> target, refreshing it if link is already a
// symlink pointing somewhere else (a stale wrapper from a previous install -
// e.g. a rebuilt/upgraded vintner binary, or a project rename: this exact
// scenario was hit by an installation still carrying symlinks to the tool's
// pre-rename binary name, silently running that old build forever since
// nothing ever pointed re-installs at the new one). Something at link that
// isn't a symlink at all (a real file) is left alone either way - whatever
// put it there, it's not this function's to replace.
func lnS(target, link string) error {
	if cur, err := os.Readlink(link); err == nil {
		if cur == target {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	} else if _, statErr := os.Lstat(link); statErr == nil {
		return nil
	}
	return os.Symlink(target, link)
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	_ = os.Remove(dst)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
