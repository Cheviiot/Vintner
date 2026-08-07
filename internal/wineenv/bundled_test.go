package wineenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindBundledWineFound(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "wine", BundledWineVersion, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	winePath := filepath.Join(binDir, "wine")
	if err := os.WriteFile(winePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindBundledWine(home)
	if !ok {
		t.Fatal("expected FindBundledWine to find the just-created binary")
	}
	if got != winePath {
		t.Errorf("FindBundledWine() = %q, want %q", got, winePath)
	}
}

func TestFindBundledWineAbsentReportsNotFound(t *testing.T) {
	home := t.TempDir() // no wine/ subdirectory at all

	if _, ok := FindBundledWine(home); ok {
		t.Error("expected FindBundledWine to report not-found when nothing is installed")
	}
}

func TestFindBundledWineWrongVersionIgnored(t *testing.T) {
	home := t.TempDir()
	// A stale bundled wine from before a vintner upgrade bumped
	// BundledWineVersion - installed under a different version directory,
	// so it must not be picked up.
	staleBinDir := filepath.Join(home, "wine", "some-old-version", "bin")
	if err := os.MkdirAll(staleBinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleBinDir, "wine"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, ok := FindBundledWine(home); ok {
		t.Error("expected FindBundledWine to ignore a wine installed under a different version directory")
	}
}

func TestResolveWinePrefersOverrideThenBundledThenSystem(t *testing.T) {
	t.Run("VINTNER_WINE wins over everything", func(t *testing.T) {
		home := t.TempDir()
		setupBundledWine(t, home)
		t.Setenv("PATH", t.TempDir())
		override := filepath.Join(t.TempDir(), "custom-wine")
		if err := os.WriteFile(override, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("VINTNER_WINE", override)

		path, source, err := ResolveWine(home)
		if err != nil {
			t.Fatal(err)
		}
		if path != override || source != WineSourceOverride {
			t.Errorf("ResolveWine() = (%q, %v), want (%q, %v)", path, source, override, WineSourceOverride)
		}
	})

	t.Run("bundled wins over system when no override is set", func(t *testing.T) {
		t.Setenv("VINTNER_WINE", "")
		home := t.TempDir()
		wantPath := setupBundledWine(t, home)
		fakeBinary(t, "wine") // system wine also present on PATH

		path, source, err := ResolveWine(home)
		if err != nil {
			t.Fatal(err)
		}
		if path != wantPath || source != WineSourceBundled {
			t.Errorf("ResolveWine() = (%q, %v), want (%q, %v)", path, source, wantPath, WineSourceBundled)
		}
	})

	t.Run("falls back to system wine when nothing else is available", func(t *testing.T) {
		t.Setenv("VINTNER_WINE", "")
		home := t.TempDir() // no bundled wine
		fakeBinary(t, "wine")

		path, source, err := ResolveWine(home)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(path) != "wine" || source != WineSourceSystem {
			t.Errorf("ResolveWine() = (%q, %v), want system wine", path, source)
		}
	})
}

// unsetEnv removes name from the environment for the duration of the test -
// unlike t.Setenv(name, ""), which still leaves it *present* (just empty),
// this matches the real "caller never touched WINEPREFIX at all" case
// os.LookupEnv distinguishes from an explicit empty value.
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	old, wasSet := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if wasSet {
			os.Setenv(name, old)
		}
	})
}

func TestWinePrefixEnvOnlyForBundledSource(t *testing.T) {
	home := "/home/example/.vintner"

	for _, source := range []WineSource{WineSourceOverride, WineSourceSystem} {
		unsetEnv(t, "WINEPREFIX")
		if got := WinePrefixEnv(home, source); got != nil {
			t.Errorf("WinePrefixEnv(%v) = %v, want nil (only WineSourceBundled should isolate WINEPREFIX)", source, got)
		}
	}
}

func TestWinePrefixEnvIsolatesForBundledSource(t *testing.T) {
	unsetEnv(t, "WINEPREFIX")
	home := "/home/example/.vintner"

	got := WinePrefixEnv(home, WineSourceBundled)
	want := []string{"WINEPREFIX=" + filepath.Join(home, "wineprefix")}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("WinePrefixEnv(bundled) = %v, want %v", got, want)
	}
}

func TestWinePrefixEnvRespectsExplicitOverride(t *testing.T) {
	t.Setenv("WINEPREFIX", "/some/user/chosen/prefix")
	home := "/home/example/.vintner"

	if got := WinePrefixEnv(home, WineSourceBundled); got != nil {
		t.Errorf("WinePrefixEnv() = %v, want nil when the caller already set WINEPREFIX", got)
	}
}

// setupBundledWine creates a fake bundled wine binary under home and
// returns its path.
func setupBundledWine(t *testing.T, home string) string {
	t.Helper()
	binDir := filepath.Join(home, "wine", BundledWineVersion, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(binDir, "wine")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
