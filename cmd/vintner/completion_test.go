package main

import (
	"os/exec"
	"strings"
	"testing"
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
		{"bash", bashCompletionScript},
		{"zsh", zshCompletionScript},
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
