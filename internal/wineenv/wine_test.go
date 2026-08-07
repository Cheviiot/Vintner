package wineenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeBinary(t *testing.T, name string) {
	t.Helper()
	t.Setenv("VINTNER_WINE", "")
	bin := t.TempDir()
	path := filepath.Join(bin, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestFindWinePrefersWine64(t *testing.T) {
	t.Setenv("VINTNER_WINE", "")
	bin := t.TempDir()
	for _, name := range []string{"wine64", "wine"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)

	got, err := FindWine()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "wine64" {
		t.Errorf("FindWine() = %q, want wine64 to be preferred over wine", got)
	}
}

func TestFindWineFallsBackToWine(t *testing.T) {
	fakeBinary(t, "wine")

	got, err := FindWine()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "wine" {
		t.Errorf("FindWine() = %q, want wine", got)
	}
}

func TestFindWinePrefersVintnerWineEnv(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"wine64", "wine"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)

	override := filepath.Join(t.TempDir(), "custom-wine")
	if err := os.WriteFile(override, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VINTNER_WINE", override)

	got, err := FindWine()
	if err != nil {
		t.Fatal(err)
	}
	if got != override {
		t.Errorf("FindWine() = %q, want VINTNER_WINE (%q) to win over both PATH entries", got, override)
	}
}

func TestFindWineErrorIsActionable(t *testing.T) {
	t.Setenv("VINTNER_WINE", "")
	t.Setenv("PATH", t.TempDir()) // empty dir, neither binary present

	_, err := FindWine()
	if err == nil {
		t.Fatal("expected an error when neither wine64 nor wine is on PATH")
	}
	if !strings.Contains(err.Error(), "install") {
		t.Errorf("FindWine() error = %q, want it to say what to install (matching msiextract/cabextract's error style)", err)
	}
}
