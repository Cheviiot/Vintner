package wrapper

import (
	"os"
	"path/filepath"
	"regexp"
)

// Argument path rewriting: MSVC/Wine sometimes fails to resolve relative-
// looking includes passed as plain unix absolute paths (see
// https://bugs.winehq.org/show_bug.cgi?id=55200); the fix is to rewrite
// `-I/abs/path` into `-Iz:/abs/path` and similar, trying each option-prefix
// shape in priority order until one matches.
var (
	reOpt1 = regexp.MustCompile(`^[-/][A-Za-z](/.*)$`)                   // -I/path, /I/path
	reOpt2 = regexp.MustCompile(`^[-/][A-Za-z][A-Za-z](/.*)$`)           // -Fo/path
	reOpt3 = regexp.MustCompile(`^[-/][A-Za-z][A-Za-z][A-Za-z]*:(/.*)$`) // -MANIFESTINPUT:/path
	reBare = regexp.MustCompile(`^(/.*)$`)                               // /abs/path alone
)

// RewriteArgs rewrites absolute unix paths embedded in tool arguments into
// "z:/abs/path" form, only when the path's parent directory actually exists
// on disk and isn't "/" - matching the original bash's `[ -d "$(dirname ..)" ]`
// guard, which keeps short flags like "/P" or "/D" untouched.
func RewriteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = rewriteArg(a)
	}
	return out
}

func rewriteArg(a string) string {
	var path string
	switch {
	case reOpt1.MatchString(a):
		path = reOpt1.FindStringSubmatch(a)[1]
	case reOpt2.MatchString(a):
		path = reOpt2.FindStringSubmatch(a)[1]
	case reOpt3.MatchString(a):
		path = reOpt3.FindStringSubmatch(a)[1]
	case reBare.MatchString(a):
		path = reBare.FindStringSubmatch(a)[1]
	default:
		return a
	}

	dir := filepath.Dir(path)
	if dir == "/" {
		return a
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return a
	}

	prefix := a[:len(a)-len(path)]
	return prefix + "z:" + path
}
