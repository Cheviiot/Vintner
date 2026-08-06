package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Cheviiot/vintner/internal/i18n"
	"github.com/Cheviiot/vintner/internal/wineenv"
	"github.com/Cheviiot/vintner/internal/wrapper"
)

// runDoctor checks the pieces vintner actually needs at runtime - wine
// itself, the optional extraction tools download needs, and every
// installed <dest>/bin/<arch> toolchain's on-disk layout - and prints a
// pass/fail checklist. The point is surfacing a broken setup as a short,
// readable report instead of a wine-specific error buried deep inside a
// build (see internal/wineenv.FindWine's own error, which this reuses).
func runDoctor(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(os.Stderr, i18n.T("doctor.usage"))
		return 1
	}

	d := &doctorReport{}
	d.checkWine()
	d.checkExtractionTools()
	d.checkToolchains()

	if d.failed {
		fmt.Println(i18n.T("doctor.summary_fail"))
		return 1
	}
	fmt.Println(i18n.T("doctor.summary_ok"))
	return 0
}

type doctorReport struct {
	failed bool
}

func (d *doctorReport) ok(format string, args ...any) {
	fmt.Printf("  [ok]   "+format+"\n", args...)
}

func (d *doctorReport) warn(format string, args ...any) {
	fmt.Printf("  [warn] "+format+"\n", args...)
}

func (d *doctorReport) fail(format string, args ...any) {
	fmt.Printf("  [FAIL] "+format+"\n", args...)
	d.failed = true
}

func (d *doctorReport) checkWine() {
	fmt.Println(i18n.T("doctor.section_wine"))

	wineBin, err := wineenv.FindWine()
	if err != nil {
		d.fail("%s", err)
		return
	}
	d.ok("found: %s", wineBin)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, wineBin, "--version").Output()
	if err != nil {
		d.fail("%s --version failed: %v", wineBin, err)
		return
	}
	d.ok("runs: %s", trimNewline(string(out)))

	if wrapper.CgroupCleanupAvailable() {
		d.ok("cgroup cleanup: available (guarantees cleanup of Wine-hosted workers that detach into their own Unix session, e.g. via setsid() - see msbuildNodeReuseArgs in internal/wrapper/msbuildenv.go)")
	} else {
		d.warn("cgroup cleanup: not available (cgroup v2 not delegated here - common in some containers) - falls back to process-group-only cleanup, which can't reach a Wine-hosted worker that detaches into its own Unix session")
	}
}

func (d *doctorReport) checkExtractionTools() {
	fmt.Println(i18n.T("doctor.section_extract"))

	if _, err := exec.LookPath("msiextract"); err != nil {
		d.warn(i18n.T("doctor.msitools_missing"))
	} else {
		d.ok("msitools: found (needed by `vintner download`)")
	}

	if _, err := exec.LookPath("cabextract"); err != nil {
		d.warn(i18n.T("doctor.cabextract_missing"))
	} else {
		d.ok("cabextract: found (needed by --with-wdk/--with-dxsdk)")
	}
}

func (d *doctorReport) checkToolchains() {
	fmt.Println(i18n.T("doctor.section_toolchain"))

	if binDir := os.Getenv("VINTNER_BIN"); binDir != "" {
		d.checkToolchainAt(binDir, "VINTNER_BIN="+binDir)
		return
	}

	def, err := defaultToolchainDir()
	if err != nil {
		d.fail("%s", err)
		return
	}
	destBin := filepath.Join(def, "bin")
	archDirs := installedArchDirs(destBin)
	if len(archDirs) == 0 {
		d.fail(i18n.T("doctor.no_toolchain", destBin))
		return
	}
	for _, arch := range archDirs {
		d.checkToolchainAt(filepath.Join(destBin, arch), arch)
	}
}

// installedArchDirs returns the subdirectories of destBin that carry their
// own env.json, i.e. every architecture `vintner install` actually set up
// (there can be more than one - e.g. x86 and x64 side by side).
func installedArchDirs(destBin string) []string {
	entries, err := os.ReadDir(destBin)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(destBin, e.Name(), wineenv.ConfigFileName)); err == nil {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs
}

func (d *doctorReport) checkToolchainAt(binDir, label string) {
	cfg, err := wineenv.Load(binDir)
	if err != nil {
		d.fail("%s: %s", label, err)
		return
	}
	baseUnix, err := wineenv.FindBaseUnix(binDir)
	if err != nil {
		d.fail("%s: %s", label, err)
		return
	}
	d.ok("%s: MSVC %s, SDK %s, root %s", label, cfg.MSVCVer, cfg.SDKVer, baseUnix)

	paths := wineenv.NewPaths(cfg, baseUnix)
	for _, dir := range []struct{ name, path string }{
		{"MSVC bin", paths.BinDir},
		{"SDK bin", paths.SDKBinDir},
		{"MSBuild bin", paths.MSBuildBinDir},
	} {
		if fi, err := os.Stat(dir.path); err != nil || !fi.IsDir() {
			d.fail("%s: %s missing: %s", label, dir.name, dir.path)
		} else {
			d.ok("%s: %s: %s", label, dir.name, dir.path)
		}
	}

	relay := filepath.Join(baseUnix, "bin", "toolrelay.exe")
	if fi, err := os.Stat(relay); err != nil || fi.IsDir() {
		d.warn("%s: toolrelay.exe not built (mt.exe's CMake exit-code translation won't apply; re-run `vintner install` to retry)", label)
	} else {
		d.ok("%s: toolrelay.exe: %s", label, relay)
	}
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
