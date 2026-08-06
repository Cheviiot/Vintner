package wrapper

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

// e2eBinDir returns a real <dest>/bin/<arch> toolchain directory to run
// end-to-end tests against, from VINTNER_E2E_BIN, or skips the test.
//
// Every other test in this package exercises pure Go logic (arg rewriting,
// env building, process-lifetime primitives) without ever actually
// invoking wine or a real cl.exe/link.exe - nothing catches a regression in
// the actual wine invocation path (buildEnv, wineenv's INCLUDE/LIB
// resolution, the produced binary's CRT/console I/O) short of a maintainer
// running a real build by hand. These tests close that gap, but aren't run
// by CI (no job sets VINTNER_E2E_BIN) since they need a real,
// already-downloaded multi-GB toolchain and a working wine install. Run
// locally after `vintner download --accept-license && vintner install`:
//
//	VINTNER_E2E_BIN=~/.vintner/toolchain/bin/x64 go test ./internal/wrapper/... -run E2E -v
func e2eBinDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("VINTNER_E2E_BIN")
	if dir == "" {
		t.Skip("VINTNER_E2E_BIN not set - skipping end-to-end wine/cl.exe test (see e2eBinDir's doc comment)")
	}
	if _, err := os.Stat(filepath.Join(dir, wineenv.ConfigFileName)); err != nil {
		t.Skipf("VINTNER_E2E_BIN=%s has no %s: %v", dir, wineenv.ConfigFileName, err)
	}
	return dir
}

// TestE2ECompilesAndRunsRealProgram is the strongest test in this repo of
// vintner's actual reason to exist: not just that cl.exe exits 0 (a broken
// INCLUDE/LIB path can still produce a stub or crash at link time in some
// configurations), but that the produced PE is valid enough for wine to
// load and execute it, and that its exit code and stdout survive the full
// wine round trip intact.
func TestE2ECompilesAndRunsRealProgram(t *testing.T) {
	binDir := e2eBinDir(t)
	dir := t.TempDir()

	src := filepath.Join(dir, "hello.c")
	const wantOutput = "hello from vintner e2e"
	const wantExitCode = 42
	program := "#include <stdio.h>\nint main(void) { printf(\"" + wantOutput + "\\n\"); return " + strconv.Itoa(wantExitCode) + "; }\n"
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(dir, "hello.exe")
	// cl.exe with no /Fo writes the .obj into its own process's current
	// working directory, not next to the source or /Fe's exe - which,
	// since Run() doesn't set exec.Cmd.Dir, inherits this test binary's
	// cwd (the package source directory). Pin it explicitly into dir so a
	// stray hello.obj doesn't get left behind in the repo.
	if code := Run("cl", []string{"/nologo", "/Fe" + exe, "/Fo" + dir + string(filepath.Separator), src}, binDir); code != 0 {
		t.Fatalf("vintner cl exited %d compiling %s (see test output above for compiler errors)", code, src)
	}
	if fi, err := os.Stat(exe); err != nil || fi.IsDir() {
		t.Fatalf("expected a compiled %s to exist after `cl` exited 0: %v", exe, err)
	}

	gotExitCode, out := runUnderWine(t, exe)
	if gotExitCode != wantExitCode {
		t.Errorf("compiled program exit code = %d, want %d (output: %s)", gotExitCode, wantExitCode, out)
	}
	if !strings.Contains(out, wantOutput) {
		t.Errorf("compiled program output = %q, want it to contain %q", out, wantOutput)
	}
}

// TestE2ECompileThenLinkSeparately covers `vintner link` on its own -
// TestE2ECompilesAndRunsRealProgram only exercises link.exe indirectly
// (cl.exe invokes it itself to go from .obj to .exe), never the separate
// Run("link", ...) path a real two-step build (compile every .c to .obj,
// then one link.exe invocation) actually uses - a different tool spec
// entry (dirBin vs cl's own filters/post-processing) that nothing else
// here touches at all.
func TestE2ECompileThenLinkSeparately(t *testing.T) {
	binDir := e2eBinDir(t)
	dir := t.TempDir()

	src := filepath.Join(dir, "hello.c")
	const wantOutput = "hello from vintner link e2e"
	const wantExitCode = 7
	program := "#include <stdio.h>\nint main(void) { printf(\"" + wantOutput + "\\n\"); return " + strconv.Itoa(wantExitCode) + "; }\n"
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}

	obj := filepath.Join(dir, "hello.obj")
	if code := Run("cl", []string{"/nologo", "/c", "/Fo" + obj, src}, binDir); code != 0 {
		t.Fatalf("vintner cl /c exited %d compiling %s", code, src)
	}
	if _, err := os.Stat(obj); err != nil {
		t.Fatalf("expected %s to exist after `cl /c` exited 0: %v", obj, err)
	}

	exe := filepath.Join(dir, "hello.exe")
	if code := Run("link", []string{"/nologo", "/out:" + exe, obj}, binDir); code != 0 {
		t.Fatalf("vintner link exited %d linking %s (see test output above for linker errors)", code, obj)
	}
	if fi, err := os.Stat(exe); err != nil || fi.IsDir() {
		t.Fatalf("expected a linked %s to exist after `link` exited 0: %v", exe, err)
	}

	gotExitCode, out := runUnderWine(t, exe)
	if gotExitCode != wantExitCode {
		t.Errorf("linked program exit code = %d, want %d (output: %s)", gotExitCode, wantExitCode, out)
	}
	if !strings.Contains(out, wantOutput) {
		t.Errorf("linked program output = %q, want it to contain %q", out, wantOutput)
	}
}

// runUnderWine runs exe under wine and returns its exit code and combined
// stdout+stderr, failing the test outright (rather than returning an error)
// if wine itself can't be found or the process can't be started at all.
func runUnderWine(t *testing.T, exe string) (exitCode int, output string) {
	t.Helper()
	wineBin, err := wineenv.FindWine()
	if err != nil {
		t.Fatalf("finding wine to run %s: %v", exe, err)
	}
	cmd := exec.Command(wineBin, exe)
	cmd.Env = append(os.Environ(), "WINEDEBUG=-all")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	if runErr == nil {
		return 0, out.String()
	}
	exitErr, ok := runErr.(*exec.ExitError)
	if !ok {
		t.Fatalf("running %s under wine: %v (output: %s)", exe, runErr, out.String())
	}
	return exitErr.ExitCode(), out.String()
}
