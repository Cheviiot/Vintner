package download

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractNuGetPackageDirRejectsZipSlip(t *testing.T) {
	dest := t.TempDir()
	nupkgPath := filepath.Join(t.TempDir(), "evil.nupkg")
	writeTestZip(t, nupkgPath, map[string]string{
		"c/build/native/include/ok.h": "fine",
		"c/../../../escape.txt":       "should never be written",
	})

	err := extractNuGetPackageDir(nupkgPath, "c/", dest)
	if err == nil {
		t.Fatal("expected an error extracting a zip entry that traverses outside dest, got nil")
	}
	illegal := filepath.Join(dest, "../../../escape.txt")
	if _, statErr := os.Stat(illegal); statErr == nil {
		t.Errorf("zip-slip entry was actually written to %s - traversal was not prevented", illegal)
	}
}

func TestExtractNuGetPackageDirStripsPrefix(t *testing.T) {
	dest := t.TempDir()
	nupkgPath := filepath.Join(t.TempDir(), "fine.nupkg")
	writeTestZip(t, nupkgPath, map[string]string{
		"c/build/native/include/ok.h": "fine",
		"other/ignored.txt":           "not under prefix, skipped",
	})

	if err := extractNuGetPackageDir(nupkgPath, "c/", dest); err != nil {
		t.Fatalf("extracting a well-behaved nupkg should not fail: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "build", "native", "include", "ok.h"))
	if err != nil {
		t.Fatalf("expected prefix-stripped path to exist: %v", err)
	}
	if string(got) != "fine" {
		t.Errorf("content = %q, want %q", got, "fine")
	}
	if _, err := os.Stat(filepath.Join(dest, "ignored.txt")); err == nil {
		t.Error("an entry outside prefix should have been skipped, not extracted")
	}
}

func TestWDKNuGetID(t *testing.T) {
	for _, tc := range []struct{ arch, want string }{
		{"x64", "Microsoft.Windows.WDK.x64"},
		{"x86", "Microsoft.Windows.WDK.x64"}, // no 32-bit package exists
		{"arm64", "Microsoft.Windows.WDK.ARM64"},
	} {
		if got := WDKNuGetID(tc.arch); got != tc.want {
			t.Errorf("WDKNuGetID(%q) = %q, want %q", tc.arch, got, tc.want)
		}
	}
}

func TestSDKBuildPrefix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected []*Package
		want     string
	}{
		{"no SDK package", []*Package{{ID: "Something.Else"}}, ""},
		{"win10sdk", []*Package{{ID: "Win10SDK_10.0.26100", Version: "10.0.26100.1742"}}, "10.0.26100"},
		{"win11sdk case-insensitive id", []*Package{{ID: "WIN11SDK_10.0.22621", Version: "10.0.22621.5"}}, "10.0.22621"},
		{"short version", []*Package{{ID: "Win10SDK_x", Version: "10.0"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SDKBuildPrefix(tc.selected); got != tc.want {
				t.Errorf("SDKBuildPrefix(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFillMissingHostToolsCopiesWithoutOverwriting(t *testing.T) {
	cDir := t.TempDir()
	writeFile(t, filepath.Join(cDir, "bin", "10.0.26100.0", "x64", "stampinf.exe"), "x64-stampinf")
	writeFile(t, filepath.Join(cDir, "bin", "10.0.26100.0", "x64", "inf2cat.exe"), "x64-inf2cat")
	// x86 already ships its own real inf2cat.exe - must not be clobbered.
	writeFile(t, filepath.Join(cDir, "bin", "10.0.26100.0", "x86", "inf2cat.exe"), "real-x86-inf2cat")

	if err := fillMissingHostTools(cDir); err != nil {
		t.Fatal(err)
	}

	x86Dir := filepath.Join(cDir, "bin", "10.0.26100.0", "x86")
	stampinf, err := os.ReadFile(filepath.Join(x86Dir, "stampinf.exe"))
	if err != nil {
		t.Fatalf("expected stampinf.exe to be copied into x86: %v", err)
	}
	if string(stampinf) != "x64-stampinf" {
		t.Errorf("copied stampinf.exe content = %q, want the x64 copy's content", stampinf)
	}

	inf2cat, err := os.ReadFile(filepath.Join(x86Dir, "inf2cat.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(inf2cat) != "real-x86-inf2cat" {
		t.Errorf("inf2cat.exe = %q, want the original x86 file preserved (not overwritten by the x64 copy)", inf2cat)
	}
}

func TestFillMissingHostToolsNoBinDirIsNoop(t *testing.T) {
	if err := fillMissingHostTools(t.TempDir()); err != nil {
		t.Fatalf("missing bin/ dir should be a no-op, got: %v", err)
	}
}

func TestDuplicateVersionedBuildTaskAssemblies(t *testing.T) {
	cDir := t.TempDir()
	buildDir := filepath.Join(cDir, "build")
	writeFile(t, filepath.Join(buildDir, "Microsoft.DriverKit.Build.Tasks.17.0.dll"), "task-dll-bytes")
	writeFile(t, filepath.Join(buildDir, "Microsoft.DriverKit.Build.Tasks.18.0.dll"), "already-there")
	writeFile(t, filepath.Join(buildDir, "unrelated.dll"), "unrelated")

	if err := duplicateVersionedBuildTaskAssemblies(cDir, "18.0"); err != nil {
		t.Fatal(err)
	}

	// Already had an 18.0 copy - must not have been overwritten.
	got, err := os.ReadFile(filepath.Join(buildDir, "Microsoft.DriverKit.Build.Tasks.18.0.dll"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "already-there" {
		t.Errorf("pre-existing 18.0 dll was overwritten: got %q", got)
	}
}

func TestDuplicateVersionedBuildTaskAssembliesCreatesMissingCopy(t *testing.T) {
	cDir := t.TempDir()
	buildDir := filepath.Join(cDir, "build")
	writeFile(t, filepath.Join(buildDir, "sub", "Foo.Bar.17.0.dll"), "bytes")

	if err := duplicateVersionedBuildTaskAssemblies(cDir, "18.0"); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(buildDir, "sub", "Foo.Bar.18.0.dll"))
	if err != nil {
		t.Fatalf("expected a Foo.Bar.18.0.dll duplicate: %v", err)
	}
	if string(got) != "bytes" {
		t.Errorf("duplicated dll content = %q, want %q", got, "bytes")
	}
}

func TestDuplicateVersionedBuildTaskAssembliesNoBuildDirIsNoop(t *testing.T) {
	if err := duplicateVersionedBuildTaskAssemblies(t.TempDir(), "18.0"); err != nil {
		t.Fatalf("missing build/ dir should be a no-op, got: %v", err)
	}
}
