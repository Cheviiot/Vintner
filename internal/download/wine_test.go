package download

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

func TestDownloadBundledWineUnknownArchFailsWithoutNetworkAccess(t *testing.T) {
	cacheDir := t.TempDir()
	home := t.TempDir()

	_, err := DownloadBundledWine("does-not-exist-arch", cacheDir, home)
	if err == nil {
		t.Fatal("expected an error for an arch with no known hash")
	}
	entries, _ := os.ReadDir(cacheDir)
	if len(entries) != 0 {
		t.Errorf("expected no download attempt for an unknown arch, cacheDir has: %v", entries)
	}
}

func TestDownloadBundledWineReusesCachedFileAndRejectsHashMismatch(t *testing.T) {
	const arch = "amd64"
	restore := wineSHA256
	wineSHA256 = map[string]string{arch: "0000000000000000000000000000000000000000000000000000000000000000"}
	defer func() { wineSHA256 = restore }()

	cacheDir := t.TempDir()
	home := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "vintner-wine-"+wineenv.BundledWineVersion+"-"+arch+".zip")
	writeFile(t, cacheFile, "not a real wine build")

	_, err := DownloadBundledWine(arch, cacheDir, home)
	if err == nil {
		t.Fatal("expected an error for a cached file that doesn't match the pinned hash")
	}
	if _, statErr := os.Stat(filepath.Join(home, "wine")); statErr == nil {
		t.Error("nothing should have been extracted before the hash was verified")
	}
}

func TestDownloadBundledWineExtractsIntoVersionedDir(t *testing.T) {
	const arch = "amd64"
	cacheDir := t.TempDir()
	home := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "vintner-wine-"+wineenv.BundledWineVersion+"-"+arch+".zip")
	writeTestZip(t, cacheFile, map[string]string{
		"bin/wine":                           "#!/bin/sh\necho fake wine\n",
		"share/wine/mono/wine-mono-11.2.0/x": "fake mono payload",
	})

	sum, err := sha256File(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	restore := wineSHA256
	wineSHA256 = map[string]string{arch: sum}
	defer func() { wineSHA256 = restore }()

	wineDir, err := DownloadBundledWine(arch, cacheDir, home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "wine", wineenv.BundledWineVersion)
	if wineDir != want {
		t.Errorf("DownloadBundledWine returned %q, want %q", wineDir, want)
	}
	if !isFile(filepath.Join(wineDir, "bin", "wine")) {
		t.Error("expected bin/wine to exist after extraction")
	}
	if !isFile(filepath.Join(wineDir, "share", "wine", "mono", "wine-mono-11.2.0", "x")) {
		t.Error("expected the bundled wine-mono payload to exist after extraction")
	}

	// Reusing the already-cached, already-verified file a second time
	// shouldn't need to redownload or fail.
	if _, err := DownloadBundledWine(arch, cacheDir, home); err != nil {
		t.Errorf("second DownloadBundledWine call (cached file already present) failed: %v", err)
	}
}
