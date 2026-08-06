package wrapper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClPostProcessRewritesLineDirectivesInPreprocessedOutput(t *testing.T) {
	dir := t.TempDir()
	fi := filepath.Join(dir, "out.i")
	// A #line directive as cl.exe's /P emits it: z:-prefixed, backslash
	// path, doubled ("escaped") backslashes, CRLF line ending.
	input := "#line 1 \"z:\\\\home\\\\user\\\\src\\\\hello.c\"\r\n" +
		"int main(void) { return 0; }\r\n"
	if err := os.WriteFile(fi, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	clPostProcess([]string{"/P", "/Fi" + fi, "hello.c"})

	got, err := os.ReadFile(fi)
	if err != nil {
		t.Fatal(err)
	}
	want := "#line 1 \"/home/user/src/hello.c\"\n" +
		"int main(void) { return 0; }\n"
	if string(got) != want {
		t.Errorf("clPostProcess output = %q, want %q", got, want)
	}
}

// TestClPostProcessRewritesLineDirectiveWithoutZDrivePrefix covers a
// second real shape confirmed against an actual `cl /P` run under wine
// (11.3, MSVC 14.51): an SDK header pulled in via INCLUDE can come back
// as a plain unix-style absolute path with no "z:" prefix at all - Wine's
// own path canonicalization doesn't always preserve the z:\... notation
// it was given - while still escaping the last separator as a doubled
// backslash. stripFirstZDrive is a no-op here (nothing to strip), but the
// backslash->slash rewrite must still apply.
func TestClPostProcessRewritesLineDirectiveWithoutZDrivePrefix(t *testing.T) {
	dir := t.TempDir()
	fi := filepath.Join(dir, "out.i")
	input := "#line 1 \"/home/user/.vintner/toolchain/kits/10/include/10.0.26100.0/ucrt\\\\stdio.h\"\r\n"
	if err := os.WriteFile(fi, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	clPostProcess([]string{"/P", "/Fi" + fi, "hello.c"})

	got, err := os.ReadFile(fi)
	if err != nil {
		t.Fatal(err)
	}
	want := "#line 1 \"/home/user/.vintner/toolchain/kits/10/include/10.0.26100.0/ucrt/stdio.h\"\n"
	if string(got) != want {
		t.Errorf("clPostProcess output = %q, want %q", got, want)
	}
}

func TestClPostProcessNoopWithoutP(t *testing.T) {
	dir := t.TempDir()
	fi := filepath.Join(dir, "out.i")
	original := "#line 1 \"z:\\\\foo.c\"\r\n"
	if err := os.WriteFile(fi, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// No "/P" flag - should leave the file untouched even though -Fi is present.
	clPostProcess([]string{"/Fi" + fi, "foo.c"})

	got, err := os.ReadFile(fi)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("file was modified without /P: got %q, want unchanged %q", got, original)
	}
}

func TestClPostProcessNoopWithoutFi(t *testing.T) {
	// Must not panic or error when -Fi wasn't passed - just silently skip.
	clPostProcess([]string{"/P", "foo.c"})
}

func TestClPostProcessMissingFileIsSilent(t *testing.T) {
	// The referenced -Fi file doesn't exist - clPostProcess must not panic.
	clPostProcess([]string{"/P", "/Fi" + filepath.Join(t.TempDir(), "missing.i"), "foo.c"})
}
