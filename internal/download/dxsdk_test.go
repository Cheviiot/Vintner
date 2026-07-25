package download

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withFakeCabextract prepends a directory containing a fake "cabextract"
// script to PATH, so tests can exercise runCabextract/DownloadDXSDK without
// the real tool (or a real DXSDK installer) present. The fake script
// records the arguments it was invoked with to argsFile and creates an
// empty DXSDK/Include and DXSDK/Lib under whatever -d directory it was
// given, mimicking a successful (if empty) extraction.
func withFakeCabextract(t *testing.T, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake cabextract script requires a POSIX shell")
	}
	bin := t.TempDir()
	script := `#!/bin/sh
echo "$@" > "` + argsFile + `"
dest=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-d" ]; then
    dest="$a"
  fi
  prev="$a"
done
mkdir -p "$dest/DXSDK/Include" "$dest/DXSDK/Lib"
`
	path := filepath.Join(bin, "cabextract")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
}

func TestRunCabextractPassesFilterArgs(t *testing.T) {
	dest := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	withFakeCabextract(t, argsFile)

	src := filepath.Join(t.TempDir(), "installer.exe")
	writeFile(t, src, "fake-installer-bytes")

	if err := runCabextract(src, dest, "DXSDK/Include/*", "DXSDK/Lib/*"); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("expected the fake cabextract to have run: %v", err)
	}
	want := "-q -d " + dest + " -F DXSDK/Include/* -F DXSDK/Lib/* " + src + "\n"
	if string(got) != want {
		t.Errorf("cabextract args = %q, want %q", got, want)
	}
}

func TestDownloadDXSDKReusesCachedFileAndRejectsHashMismatch(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	withFakeCabextract(t, argsFile)

	cacheDir := t.TempDir()
	destDir := t.TempDir()
	writeFile(t, filepath.Join(cacheDir, "DXSDK_Jun10.exe"), "not the real installer")

	_, err := DownloadDXSDK(cacheDir, destDir)
	if err == nil {
		t.Fatal("expected an error for a cached file that doesn't match dxsdkSHA256")
	}
	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("cabextract should not have run before the hash was verified")
	}
}

func TestDownloadDXSDKMovesExtractedContentIntoPlace(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	withFakeCabextract(t, argsFile)

	cacheDir := t.TempDir()
	destDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "DXSDK_Jun10.exe")
	writeFile(t, cacheFile, "fake-installer-bytes-for-hash-test")

	// Patch the expected hash to match our fake cached file, since we can't
	// (and shouldn't) fetch or embed the real 600MB installer in a test.
	sum, err := sha256File(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	restore := dxsdkSHA256
	dxsdkSHA256 = sum
	defer func() { dxsdkSHA256 = restore }()

	dxsdkDir, err := DownloadDXSDK(cacheDir, destDir)
	if err != nil {
		t.Fatal(err)
	}
	if dxsdkDir != filepath.Join(destDir, "DXSDK") {
		t.Errorf("DownloadDXSDK returned %q, want %q", dxsdkDir, filepath.Join(destDir, "DXSDK"))
	}
	for _, sub := range []string{"Include", "Lib"} {
		if !isDir(filepath.Join(dxsdkDir, sub)) {
			t.Errorf("expected %s/%s to exist after extraction", dxsdkDir, sub)
		}
	}
}
