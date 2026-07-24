package wrapper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRewriteArg(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "inc")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "foo.h")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single-letter option + abs dir", "-I" + sub, "-Iz:" + sub},
		{"two-letter option + abs file", "-Fo" + file, "-Foz:" + file},
		{"long colon option + abs file", "-MANIFESTINPUT:" + file, "-MANIFESTINPUT:z:" + file},
		{"bare absolute path", file, "z:" + file},
		{"plain flag untouched", "-nologo", "-nologo"},
		{"nonexistent dir untouched", "-I/does/not/exist/at/all", "-I/does/not/exist/at/all"},
		{"root-level bare path untouched", "/nologo", "/nologo"},
		{"relative path untouched", "test.c", "test.c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteArg(tc.in); got != tc.want {
				t.Errorf("rewriteArg(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRewriteArgsPreservesOrderAndLength(t *testing.T) {
	in := []string{"/nologo", "-c", "test.c"}
	out := RewriteArgs(in)
	if len(out) != len(in) {
		t.Fatalf("length changed: %v -> %v", in, out)
	}
}
