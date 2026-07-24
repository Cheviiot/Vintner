package wrapper

import (
	"os"
	"strings"
)

// clPostProcess mirrors cl's post-run fixup for `/P /Fi<file>` (preprocess-
// to-file): the generated file still has CRLF endings and z:-prefixed
// #line directives, so run it through the same line-directive rewrite used
// for stdout.
func clPostProcess(origArgs []string) {
	var hasP bool
	var fiFile string
	for _, a := range origArgs {
		switch {
		case a == "-P" || a == "/P":
			hasP = true
		case strings.HasPrefix(a, "-Fi") || strings.HasPrefix(a, "/Fi"):
			fiFile = a[3:]
		}
	}
	if !hasP || fiFile == "" {
		return
	}
	data, err := os.ReadFile(fiFile)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		line = stripCR(line)
		if reLineDirective.MatchString(line) {
			line = strings.ReplaceAll(stripFirstZDrive(line), `\\`, `/`)
		}
		lines[i] = line
	}
	_ = os.WriteFile(fiFile, []byte(strings.Join(lines, "\n")), 0o644)
}
