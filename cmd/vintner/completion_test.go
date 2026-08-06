package main

import (
	"os"
	"os/exec"
	"regexp"
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
// found and fixed alongside this test: a hand-copied tool list here
// drifting from the real set in internal/wrapper as tools get added. Every
// current tool name must appear in both generated scripts.
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

var (
	// fs.String("name", ...), fs.Bool("name", ...), fs.Int("name", ...).
	reDownloadFlagDeclSimple = regexp.MustCompile(`fs\.(?:String|Bool|Int)\("([a-z][a-z0-9-]*)"`)
	// fs.Var(&someVar, "name", "usage") - a different shape (the flag name
	// isn't the first argument), needs its own pattern.
	reDownloadFlagDeclVar = regexp.MustCompile(`fs\.Var\([^,]+,\s*"([a-z][a-z0-9-]*)"`)
)

// downloadGoFlagNames extracts every flag name download.go's runDownload
// actually registers (fs.String/Bool/Int/Var), by reading its source
// directly rather than duplicating the list by hand a second time in this
// test - the whole point being caught here is a hand-copied list drifting
// out of sync with the real one.
func downloadGoFlagNames(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("download.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, re := range []*regexp.Regexp{reDownloadFlagDeclSimple, reDownloadFlagDeclVar} {
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			names = append(names, m[1])
		}
	}
	if len(names) < 20 { // sanity check: the regexes didn't silently stop matching
		t.Fatalf("only found %d flag declarations in download.go via regexp - the extraction regexes likely need updating to match a changed fs.X(...) call style", len(names))
	}
	return names
}

// TestCompletionScriptsListEveryDownloadFlag guards the actual historical
// bug (see runCompletion's doc comment: "--with-dxsdk missing ... after
// being added to download.go") - TestCompletionScriptsListEveryTool only
// ever covered the tool-name list, never the flag lists this file's own
// package doc blames for that regression, so a newly added download flag
// could still silently go unlisted here.
func TestCompletionScriptsListEveryDownloadFlag(t *testing.T) {
	bash := bashCompletionScript()
	zsh := zshCompletionScript()
	for _, name := range downloadGoFlagNames(t) {
		flag := "--" + name
		if !strings.Contains(bash, flag) {
			t.Errorf("bash completion's downloadFlags doesn't mention %q (download.go registers it)", flag)
		}
		if !strings.Contains(zsh, flag+"[") {
			t.Errorf("zsh completion's download flags array doesn't mention %q (download.go registers it)", flag)
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
