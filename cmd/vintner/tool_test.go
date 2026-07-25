package main

import (
	"path/filepath"
	"testing"
)

func TestRunToolReportsMissingToolchain(t *testing.T) {
	t.Setenv("VINTNER_BIN", filepath.Join(t.TempDir(), "does-not-exist"))
	if code := runTool("cl", nil); code != 1 {
		t.Errorf("runTool with a nonexistent VINTNER_BIN = %d, want 1", code)
	}
}

func TestRunToolUsesVINTNERBinOverDefault(t *testing.T) {
	// A directory that exists but has no env.json - past the "toolchain
	// found at all" check, into wrapper.Run's own (already-tested)
	// env.json-loading error path. Confirms VINTNER_BIN is actually being
	// read and passed through, without needing a full fake toolchain.
	t.Setenv("VINTNER_BIN", t.TempDir())
	if code := runTool("cl", nil); code != 1 {
		t.Errorf("runTool with an empty VINTNER_BIN dir = %d, want 1 (from the missing env.json)", code)
	}
}
