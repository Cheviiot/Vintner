package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultToolchainDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}
	got, err := defaultToolchainDir()
	if err != nil {
		t.Fatalf("defaultToolchainDir() error: %v", err)
	}
	want := filepath.Join(home, ".vintner")
	if got != want {
		t.Errorf("defaultToolchainDir() = %q, want %q", got, want)
	}
}
