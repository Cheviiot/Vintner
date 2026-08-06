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
	if code := Run("cl", []string{"/nologo", "/Fe" + exe, src}, binDir); code != 0 {
		t.Fatalf("vintner cl exited %d compiling %s (see test output above for compiler errors)", code, src)
	}
	if fi, err := os.Stat(exe); err != nil || fi.IsDir() {
		t.Fatalf("expected a compiled %s to exist after `cl` exited 0: %v", exe, err)
	}

	wineBin, err := wineenv.FindWine()
	if err != nil {
		t.Fatalf("finding wine to run the compiled binary: %v", err)
	}
	cmd := exec.Command(wineBin, exe)
	cmd.Env = append(os.Environ(), "WINEDEBUG=-all")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()

	gotExitCode := 0
	if runErr != nil {
		exitErr, ok := runErr.(*exec.ExitError)
		if !ok {
			t.Fatalf("running compiled %s under wine: %v (output: %s)", exe, runErr, out.String())
		}
		gotExitCode = exitErr.ExitCode()
	}
	if gotExitCode != wantExitCode {
		t.Errorf("compiled program exit code = %d, want %d (output: %s)", gotExitCode, wantExitCode, out.String())
	}
	if !strings.Contains(out.String(), wantOutput) {
		t.Errorf("compiled program output = %q, want it to contain %q", out.String(), wantOutput)
	}
}
