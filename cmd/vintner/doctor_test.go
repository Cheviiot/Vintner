package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

func TestRunDoctorUsage(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		if code := runDoctor([]string{flag}); code != 1 {
			t.Errorf("runDoctor([%q]) = %d, want 1", flag, code)
		}
	}
}

func TestDoctorReportOkWarnFail(t *testing.T) {
	d := &doctorReport{}
	d.ok("fine")
	d.warn("meh")
	if d.failed {
		t.Fatal("ok/warn must not mark the report as failed")
	}
	d.fail("broken")
	if !d.failed {
		t.Fatal("fail must mark the report as failed")
	}
}

func TestInstalledArchDirsFindsOnlyDirsWithEnvJSON(t *testing.T) {
	destBin := t.TempDir()
	for _, dir := range []string{"x64", "x86", "not-a-toolchain"} {
		if err := os.MkdirAll(filepath.Join(destBin, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"x64", "x86"} {
		if err := os.WriteFile(filepath.Join(destBin, dir, wineenv.ConfigFileName), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := installedArchDirs(destBin)
	want := map[string]bool{"x64": true, "x86": true}
	if len(got) != len(want) {
		t.Fatalf("installedArchDirs = %v, want exactly %v", got, want)
	}
	for _, arch := range got {
		if !want[arch] {
			t.Errorf("installedArchDirs returned unexpected entry %q", arch)
		}
	}
}

func TestInstalledArchDirsMissingDir(t *testing.T) {
	if got := installedArchDirs(filepath.Join(t.TempDir(), "does-not-exist")); got != nil {
		t.Errorf("installedArchDirs on a missing dir = %v, want nil", got)
	}
}

func TestCheckToolchainAtMissingEnvJSON(t *testing.T) {
	d := &doctorReport{}
	d.checkToolchainAt(t.TempDir(), "test")
	if !d.failed {
		t.Error("checkToolchainAt against a dir with no env.json should fail the report")
	}
}

func TestTrimNewline(t *testing.T) {
	cases := map[string]string{
		"wine-9.0\n":   "wine-9.0",
		"wine-9.0\r\n": "wine-9.0",
		"wine-9.0":     "wine-9.0",
		"":             "",
	}
	for in, want := range cases {
		if got := trimNewline(in); got != want {
			t.Errorf("trimNewline(%q) = %q, want %q", in, got, want)
		}
	}
}
