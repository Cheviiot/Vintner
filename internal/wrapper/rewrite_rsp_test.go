package wrapper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// unixPathFromAtArg recovers the real unix path from a rewritten "@z:\..."
// response-file arg (the form wineenv.ToWinPath produces), so the test can
// read the file back and check what actually got written.
func unixPathFromAtArg(t *testing.T, arg string) string {
	t.Helper()
	p := strings.TrimPrefix(arg, "@")
	p = strings.TrimPrefix(p, "z:")
	return strings.ReplaceAll(p, `\`, "/")
}

func TestRewriteResponseFileArgsRewritesUnixPaths(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "inc")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "foo.h")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	rsp := filepath.Join(dir, "args.rsp")
	content := "-I" + sub + " -Fo" + file + " test.c"
	if err := os.WriteFile(rsp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := rewriteResponseFileArgs([]string{"cl", "@" + rsp})
	if len(out) != 2 {
		t.Fatalf("length changed: %v", out)
	}
	if out[0] != "cl" {
		t.Fatalf("unrelated arg touched: %v", out)
	}
	if !strings.HasPrefix(out[1], "@z:") {
		t.Fatalf("expected a z:-prefixed response-file arg, got %q", out[1])
	}
	newPath := unixPathFromAtArg(t, out[1])

	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("reading rewritten response file: %v", err)
	}
	want := "-Iz:" + sub + " -Foz:" + file + " test.c"
	if string(got) != want {
		t.Errorf("rewritten content = %q, want %q", got, want)
	}
}

func TestRewriteResponseFileArgsPreservesQuotedTokensWithSpaces(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "inc")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	rsp := filepath.Join(dir, "args.rsp")
	content := `-I` + sub + ` "C:\Program Files\Foo\bar.h" test.c`
	if err := os.WriteFile(rsp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := rewriteResponseFileArgs([]string{"@" + rsp})
	newPath := unixPathFromAtArg(t, out[0])
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("reading rewritten response file: %v", err)
	}
	want := `-Iz:` + sub + ` "C:\Program Files\Foo\bar.h" test.c`
	if string(got) != want {
		t.Errorf("rewritten content = %q, want %q", got, want)
	}
}

func TestRewriteResponseFileArgsHandlesUTF16BOM(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "inc")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	rsp := filepath.Join(dir, "args.rsp")
	content := "-I" + sub + " test.c"
	u := utf16.Encode([]rune(content))
	raw := make([]byte, 2+len(u)*2)
	raw[0], raw[1] = 0xFF, 0xFE
	for i, v := range u {
		raw[2+i*2] = byte(v)
		raw[2+i*2+1] = byte(v >> 8)
	}
	if err := os.WriteFile(rsp, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	out := rewriteResponseFileArgs([]string{"@" + rsp})
	newPath := unixPathFromAtArg(t, out[0])
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("reading rewritten response file: %v", err)
	}
	if len(got) < 2 || got[0] != 0xFF || got[1] != 0xFE {
		t.Fatalf("rewritten file lost its UTF-16LE BOM: % x", got[:min(4, len(got))])
	}
	u16 := make([]uint16, 0, (len(got)-2)/2)
	for i := 2; i+1 < len(got); i += 2 {
		u16 = append(u16, uint16(got[i])|uint16(got[i+1])<<8)
	}
	gotStr := string(utf16.Decode(u16))
	want := "-Iz:" + sub + " test.c"
	if gotStr != want {
		t.Errorf("rewritten content = %q, want %q", gotStr, want)
	}
}

func TestRewriteResponseFileArgsLeavesMissingFileAlone(t *testing.T) {
	in := []string{"cl", "@/does/not/exist.rsp"}
	out := rewriteResponseFileArgs(in)
	if out[1] != in[1] {
		t.Errorf("missing response file should be left untouched, got %q", out[1])
	}
}

func TestRewriteResponseFileArgsNoOpWithoutAtArgs(t *testing.T) {
	in := []string{"/nologo", "-c", "test.c"}
	out := rewriteResponseFileArgs(in)
	if len(out) != len(in) {
		t.Fatalf("length changed: %v -> %v", in, out)
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("arg %d changed: %q -> %q", i, in[i], out[i])
		}
	}
}
