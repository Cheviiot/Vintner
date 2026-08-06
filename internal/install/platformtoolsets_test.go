package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

func TestAliasPlatformToolsetsAliasesRealToolset(t *testing.T) {
	dest := t.TempDir()
	toolsetsDir := filepath.Join(dest, "MSBuild", "Microsoft", "VC", "v180", "Platforms", "x64", "PlatformToolsets")
	realDir := filepath.Join(toolsetsDir, "v145")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "Toolset.props"), []byte("<Project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A WDK toolset entry alongside it - must not be mistaken for the real
	// numeric toolset or itself get aliased over.
	if err := os.MkdirAll(filepath.Join(toolsetsDir, "WindowsKernelModeDriver10.0"), 0o755); err != nil {
		t.Fatal(err)
	}

	real, err := aliasPlatformToolsets(dest)
	if err != nil {
		t.Fatalf("aliasPlatformToolsets: %v", err)
	}
	if real != "145" {
		t.Errorf("aliasPlatformToolsets(...) real = %q, want \"145\"", real)
	}

	for _, n := range wineenv.KnownPlatformToolsets {
		alias := filepath.Join(toolsetsDir, "v"+n)
		fi, err := os.Lstat(alias)
		if err != nil {
			t.Errorf("expected v%s alias to exist: %v", n, err)
			continue
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("v%s should be a symlink, got mode %v", n, fi.Mode())
			continue
		}
		target, err := os.Readlink(alias)
		if err != nil {
			t.Fatal(err)
		}
		if target != "v145" {
			t.Errorf("v%s symlink target = %q, want \"v145\"", n, target)
		}
		// Follow the alias and confirm it actually reaches the real content.
		if !isFile(filepath.Join(toolsetsDir, "v"+n, "Toolset.props")) {
			t.Errorf("v%s/Toolset.props not reachable through the alias", n)
		}
	}

	if exists(filepath.Join(toolsetsDir, "WindowsKernelModeDriver10.0", "v"+wineenv.KnownPlatformToolsets[0])) {
		t.Error("WDK toolset directory should not have been touched")
	}
}

func TestAliasPlatformToolsetsDoesNotOverwriteExisting(t *testing.T) {
	dest := t.TempDir()
	toolsetsDir := filepath.Join(dest, "MSBuild", "Microsoft", "VC", "v180", "Platforms", "x64", "PlatformToolsets")
	if err := os.MkdirAll(filepath.Join(toolsetsDir, "v145"), 0o755); err != nil {
		t.Fatal(err)
	}
	// v142 already genuinely installed (e.g. a real VS install with several
	// side-by-side toolsets) - must be left alone, not replaced with an alias.
	real142 := filepath.Join(toolsetsDir, "v142")
	if err := os.MkdirAll(real142, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real142, "Toolset.props"), []byte("<!-- real v142 -->"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := aliasPlatformToolsets(dest); err != nil {
		t.Fatalf("aliasPlatformToolsets: %v", err)
	}

	fi, err := os.Lstat(real142)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("pre-existing v142 directory should not have been replaced with a symlink")
	}
}

func TestAliasPlatformToolsetsNoNumericToolset(t *testing.T) {
	dest := t.TempDir()
	// Only a WDK-style toolset present, nothing numeric to alias from.
	toolsetsDir := filepath.Join(dest, "MSBuild", "Microsoft", "VC", "v180", "Platforms", "x64", "PlatformToolsets")
	if err := os.MkdirAll(filepath.Join(toolsetsDir, "WindowsUserModeDriver10.0"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := aliasPlatformToolsets(dest); err != nil {
		t.Fatalf("aliasPlatformToolsets: %v", err)
	}

	for _, n := range wineenv.KnownPlatformToolsets {
		if exists(filepath.Join(toolsetsDir, "v"+n)) {
			t.Errorf("v%s should not have been created with no real numeric toolset present", n)
		}
	}
}
