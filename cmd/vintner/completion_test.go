package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/Cheviiot/vintner/internal/wrapper"
)

// TestCompletionScriptsAreSyntacticallyValid catches the easy way to break
// these: a typo in the hand-maintained flag lists that produces invalid
// shell syntax. It shells out to bash/zsh -n rather than parsing the script
// itself, so it's testing exactly what a user's shell would see.
func TestCompletionScriptsAreSyntacticallyValid(t *testing.T) {
	for _, tc := range []struct {
		shell  string
		script string
	}{
		{"bash", bashCompletionScript()},
		{"zsh", zshCompletionScript()},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			if _, err := exec.LookPath(tc.shell); err != nil {
				t.Skipf("%s not installed", tc.shell)
			}
			cmd := exec.Command(tc.shell, "-n", "/dev/stdin")
			cmd.Stdin = strings.NewReader(tc.script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s -n rejected the completion script: %v\n%s", tc.shell, err, out)
			}
		})
	}
}

// TestCompletionScriptsListEveryTool guards against the exact staleness bug
// found and fixed alongside this test: a hand-copied tool/flag list here
// drifting from the real set in internal/wrapper (or download.go's flags)
// as tools/flags get added. Every current tool name must appear in both
// generated scripts.
func TestCompletionScriptsListEveryTool(t *testing.T) {
	bash := bashCompletionScript()
	zsh := zshCompletionScript()
	for _, name := range wrapper.ToolNames() {
		if !strings.Contains(bash, name) {
			t.Errorf("bash completion script doesn't mention tool %q", name)
		}
		if !strings.Contains(zsh, name+":") {
			t.Errorf("zsh completion script doesn't mention tool %q", name)
		}
	}
}

func TestRunCompletionUnknownShell(t *testing.T) {
	if code := runCompletion([]string{"fish"}); code != 1 {
		t.Errorf("runCompletion([\"fish\"]) = %d, want 1", code)
	}
	if code := runCompletion(nil); code != 1 {
		t.Errorf("runCompletion(nil) = %d, want 1", code)
	}
	if code := runCompletion([]string{"bash", "extra"}); code != 1 {
		t.Errorf("runCompletion with extra arg = %d, want 1", code)
	}
}
