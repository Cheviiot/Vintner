package install

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FixIncludeOptions controls how FixInclude rewrites include directives.
type FixIncludeOptions struct {
	// MapWinSDK restores the canonical "GL/" casing after lowercasing,
	// since that's the cross-platform spelling for that header directory.
	MapWinSDK bool
}

// reIncludeLine matches `#include <foo/bar.h>` or `#include "foo.h"`, but
// not `#include IDENTIFIER` (macro-expanded includes).
var reIncludeLine = regexp.MustCompile(`^\s*#\s*include\s+["<][\w.\\/]+[">]`)

// FixInclude rewrites #include directives under root to reference the
// lowercase header names produced by Lowercase, since MSVC/WinSDK headers
// reference each other with casing that's internally inconsistent (but
// self-consistent once lowercased). Every text file under root is
// rewritten with normalized (LF) line endings as a side effect.
func FixInclude(root string, opts FixIncludeOptions) error {
	var doDir func(dir string) error
	doDir = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if e.Type()&os.ModeSymlink != 0 {
				continue
			}
			if e.IsDir() {
				if err := doDir(path); err != nil {
					return err
				}
				continue
			}
			if err := fixFile(path, opts); err != nil {
				return err
			}
		}
		return nil
	}
	return doDir(root)
}

func fixFile(path string, opts FixIncludeOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if reIncludeLine.MatchString(line) {
			code, comment := line, ""
			if idx := strings.Index(line, "//"); idx >= 0 {
				code, comment = line[:idx], line[idx:]
			}
			code = lowercaseAndSlash(code)
			if opts.MapWinSDK {
				code = strings.Replace(code, "gl/", "GL/", 1)
			}
			line = code + comment
		}
		lines[i] = strings.TrimRight(line, "\r\n")
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// lowercaseAndSlash lowercases ASCII letters and turns backslashes into
// forward slashes, leaving everything else (including non-ASCII bytes)
// untouched.
func lowercaseAndSlash(s string) string {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'A' && c <= 'Z':
			b[i] = c - 'A' + 'a'
		case c == '\\':
			b[i] = '/'
		}
	}
	return string(b)
}
