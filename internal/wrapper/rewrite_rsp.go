package wrapper

import (
	"os"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

// reAtArg matches a response-file switch ("@path") - the form Ninja and
// MSBuild's VCToolTask-family tasks (CL, Link, LIB) both use to pass a long
// argument list via a file instead of the process's own command line, to
// stay under Windows' command-line length limit.
var reAtArg = regexp.MustCompile(`^@(.+)$`)

// rewriteResponseFileArgs extends RewriteArgs' winehq-55200 workaround (see
// rewrite.go) to reach arguments living inside a "@file" response file: a
// unix-absolute path in e.g. "-I/abs/path" needs the same "z:" treatment
// whether it arrived as a normal argument or, because CMake+Ninja (or an
// MSBuild C++ task) decided to keep the command line short, as a line inside
// a response file RewriteArgs never gets to see. Args without a matching,
// readable response file (including every ordinary non-"@" argument) pass
// through unchanged.
func rewriteResponseFileArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, a := range out {
		m := reAtArg.FindStringSubmatch(a)
		if m == nil {
			continue
		}
		if newPath, ok := rewriteResponseFile(m[1]); ok {
			out[i] = "@" + wineenv.ToWinPath(newPath)
		}
	}
	return out
}

// rewriteResponseFile reads the response file at path (a plain unix path -
// Ninja/CMake write these directly on the unix filesystem, no wine
// translation needed to read them), rewrites any unquoted token that
// rewriteArg would also rewrite as a normal argument, and - only if
// something actually changed - writes the result to a new temp file,
// returning its path. ok is false if the file couldn't be read or nothing
// needed rewriting, in which case the caller leaves the original "@path"
// argument untouched.
//
// Quoted tokens are left completely untouched rather than unescaped and
// re-quoted: the paths this exists to fix are always unquoted unix paths
// with no embedded spaces, while a quoted token is realistically a
// Windows-style path containing spaces (e.g. "C:\Program Files\...",
// extremely common in MSBuild-generated response files) that must survive
// byte-for-byte.
func rewriteResponseFile(path string) (newPath string, ok bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	content, encode := decodeRsp(raw)

	var b strings.Builder
	changed := false
	last := 0
	for _, t := range tokenizeRsp(content) {
		b.WriteString(content[last:t.start])
		tok := content[t.start:t.end]
		if !t.quoted {
			if rewritten := rewriteArg(tok); rewritten != tok {
				changed = true
				tok = rewritten
			}
		}
		b.WriteString(tok)
		last = t.end
	}
	b.WriteString(content[last:])

	if !changed {
		return "", false
	}

	f, err := os.CreateTemp("", "vintner-rsp-*.rsp")
	if err != nil {
		return "", false
	}
	defer f.Close()
	if _, err := f.Write(encode(b.String())); err != nil {
		return "", false
	}
	return f.Name(), true
}

// rspToken is one whitespace-delimited token of a response file's content,
// as a byte range into that content - not unescaped, so untouched tokens
// can be copied back out verbatim.
type rspToken struct {
	start, end int
	quoted     bool // contains at least one real (non-file-boundary) quote
}

// tokenizeRsp splits content into rspTokens using the same argv convention
// CommandLineToArgvW (and, by extension, both Ninja's and MSBuild's
// CommandLineBuilder) use: tokens are whitespace-separated outside "..."
// quoted regions, and a run of N backslashes immediately before a quote
// collapses the quoting/escaping decision on that quote alone (odd N: the
// last backslash escapes the quote, a literal '"' character, no toggle;
// even N: the quote toggles quoted-region state) - backslashes elsewhere are
// always literal. This only needs to find token boundaries and flag which
// ones contain a quote, not fully unescape them.
func tokenizeRsp(content string) []rspToken {
	var tokens []rspToken
	n := len(content)
	isSpace := func(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

	i := 0
	for i < n {
		for i < n && isSpace(content[i]) {
			i++
		}
		if i >= n {
			break
		}
		start := i
		quoted := false
		inQuotes := false
	tokenLoop:
		for i < n {
			switch c := content[i]; {
			case c == '\\':
				j := i
				for j < n && content[j] == '\\' {
					j++
				}
				if j < n && content[j] == '"' {
					quoted = true
					if (j-i)%2 == 0 {
						inQuotes = !inQuotes
					}
					i = j + 1
				} else {
					i = j
				}
			case c == '"':
				quoted = true
				inQuotes = !inQuotes
				i++
			case !inQuotes && isSpace(c):
				break tokenLoop
			default:
				i++
			}
		}
		tokens = append(tokens, rspToken{start: start, end: i, quoted: quoted})
	}
	return tokens
}

// decodeRsp detects a UTF-16LE BOM (what VCToolTask writes real response
// files as - see Microsoft.Build.CPPTasks.Common's
// VCToolTask.ResponseFileEncoding) and returns content decoded to a plain
// Go string plus a matching encoder to use when writing the rewritten
// content back out. Anything without that BOM (the common case: Ninja
// writes plain text) passes through as-is.
func decodeRsp(raw []byte) (content string, encode func(string) []byte) {
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xFE {
		return string(raw), func(s string) []byte { return []byte(s) }
	}

	u16 := make([]uint16, 0, (len(raw)-2)/2)
	for i := 2; i+1 < len(raw); i += 2 {
		u16 = append(u16, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	content = string(utf16.Decode(u16))

	encode = func(s string) []byte {
		u := utf16.Encode([]rune(s))
		out := make([]byte, 2+len(u)*2)
		out[0], out[1] = 0xFF, 0xFE
		for i, v := range u {
			out[2+i*2] = byte(v)
			out[2+i*2+1] = byte(v >> 8)
		}
		return out
	}
	return content, encode
}
