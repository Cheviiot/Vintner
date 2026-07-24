package wrapper

import (
	"regexp"
	"strings"
)

// lineFilter rewrites a single line of tool output. nil means "no extra
// rewriting" (CR-stripping still always happens, see stripCR).
type lineFilter func(line string) string

// reZDrive matches the wine "z:" drive prefix followed by a path separator,
// case-insensitively, anywhere in the line - mirroring sed's `s/z:([\\/])/\1/i`
// (no /g flag: only the first occurrence is touched).
var reZDrive = regexp.MustCompile(`(?i)z:([\\/])`)

// stripFirstZDrive removes the first "z:" preceding a path separator,
// keeping the separator itself, matching the original's un-global sed rule.
func stripFirstZDrive(line string) string {
	loc := reZDrive.FindStringSubmatchIndex(line)
	if loc == nil {
		return line
	}
	sep := line[loc[2]:loc[3]]
	return line[:loc[0]] + sep + line[loc[1]:]
}

var (
	reNoteIncluding    = regexp.MustCompile(`^Note: including file: `)
	reLineDirective    = regexp.MustCompile(`^[ \t]*#[ \t]*line[ \t]`)
	reNoteErrorWarning = regexp.MustCompile(`(?i)^z:.*\([0-9]+\): (note|error c[0-9]{4}|warning c[0-9]{4}): `)
	reDumpbinPath      = regexp.MustCompile(`^(Dump of file |  PDB file found at )`)
)

// clStdoutFilter rewrites cl's "Note: including file:", "#line", and
// note/warning/error diagnostic lines from wine's z:\... notation back to
// plain unix paths.
func clStdoutFilter(line string) string {
	switch {
	case reNoteIncluding.MatchString(line):
		return strings.ReplaceAll(stripFirstZDrive(line), `\`, `/`)
	case reLineDirective.MatchString(line):
		return strings.ReplaceAll(stripFirstZDrive(line), `\\`, `/`)
	case reNoteErrorWarning.MatchString(line):
		return strings.ReplaceAll(stripFirstZDrive(line), `\`, `/`)
	default:
		return line
	}
}

// clStderrFilter only rewrites "Note: including file:" lines - cl's stderr
// diagnostics don't carry the z:\... prefix the stdout ones do.
func clStderrFilter(line string) string {
	if reNoteIncluding.MatchString(line) {
		return strings.ReplaceAll(stripFirstZDrive(line), `\`, `/`)
	}
	return line
}

// dumpbinStdoutFilter mirrors dumpbin's unixify_path variant.
func dumpbinStdoutFilter(line string) string {
	if reDumpbinPath.MatchString(line) {
		return strings.ReplaceAll(stripFirstZDrive(line), `\`, `/`)
	}
	return line
}

// stripCR mirrors the base `s/\r//` rule every wrapper prepends: removes the
// first carriage return in the line (CRLF line endings only ever carry one).
func stripCR(line string) string {
	return strings.Replace(line, "\r", "", 1)
}
