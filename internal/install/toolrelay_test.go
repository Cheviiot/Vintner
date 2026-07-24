package install

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildToolRelay exercises the file shuffling around the actual
// compiler invocation (write embedded source, run "cl", move the resulting
// .exe into bin/, clean up the .obj and temp source) using a fake `cl`
// shell script standing in for the real Wine-hosted compiler - this can
// run fully offline, without wine or a real MSVC install.
func TestBuildToolRelay(t *testing.T) {
	dest := t.TempDir()
	destBin := filepath.Join(dest, "bin")
	hostDir := filepath.Join(destBin, "x64")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Fake `cl`: just drop toolrelay.exe/.obj next to the source it was
	// given, like the real cl.exe would when invoked without /Fe or /Fo.
	fakeCl := filepath.Join(hostDir, "cl")
	script := "#!/bin/sh\ntouch toolrelay.exe toolrelay.obj\n"
	if err := os.WriteFile(fakeCl, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := buildToolRelay(dest, destBin, "x64"); err != nil {
		t.Fatalf("buildToolRelay: %v", err)
	}

	if !isFile(filepath.Join(destBin, "toolrelay.exe")) {
		t.Errorf("expected %s/toolrelay.exe to exist", destBin)
	}
	if exists(filepath.Join(dest, "toolrelay.obj")) {
		t.Errorf("toolrelay.obj should have been removed from %s", dest)
	}
	if exists(filepath.Join(dest, "toolrelay.cpp")) {
		t.Errorf("temp toolrelay.cpp source should have been removed from %s", dest)
	}
}

func TestBuildToolRelayNoClWrapper(t *testing.T) {
	dest := t.TempDir()
	destBin := filepath.Join(dest, "bin")
	if err := os.MkdirAll(destBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := buildToolRelay(dest, destBin, "x64"); err == nil {
		t.Fatal("expected an error when no host-arch cl wrapper exists")
	}
}
