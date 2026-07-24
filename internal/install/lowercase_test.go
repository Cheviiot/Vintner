package install

import (
	"os"
	"path/filepath"
	"testing"
)

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLowercaseRenameMode(t *testing.T) {
	root := t.TempDir()
	// The top-level dir passed to Lowercase is itself lowercased too when
	// not already lowercase (matching the original perl script's dodir,
	// which lowercases relpath=="" using the dir's own basename).
	tree := filepath.Join(root, "Include")
	mustWriteFile(t, filepath.Join(tree, "Foo", "Bar.H"), "content")

	if err := Lowercase(tree, LowercaseOptions{}); err != nil {
		t.Fatal(err)
	}

	lowerTree := filepath.Join(root, "include")
	if !isFile(filepath.Join(lowerTree, "foo", "bar.h")) {
		t.Fatalf("expected lowercased path to exist under %s", lowerTree)
	}
	if isDir(tree) {
		t.Fatalf("original-cased top dir should have been renamed away")
	}
}

func TestLowercaseSymlinkMode(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "include")
	mustWriteFile(t, filepath.Join(tree, "Foo", "Bar.h"), "content")

	if err := Lowercase(tree, LowercaseOptions{Symlink: true}); err != nil {
		t.Fatal(err)
	}

	// Original casing must still be reachable (symlink mode never deletes).
	if !isFile(filepath.Join(tree, "Foo", "Bar.h")) {
		t.Fatalf("original-cased file should still exist in symlink mode")
	}
	// Lowercase alias must resolve to the same content.
	data, err := os.ReadFile(filepath.Join(tree, "foo", "bar.h"))
	if err != nil {
		t.Fatalf("expected lowercase alias to resolve: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("got %q", data)
	}
}

func TestLowercaseMergeOnCollision(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "include")
	// Both "GL" and "gl" exist as siblings; lowercasing "GL" must merge its
	// contents into the already-lowercase "gl" rather than clobbering it.
	mustWriteFile(t, filepath.Join(tree, "gl", "existing.h"), "existing")
	mustWriteFile(t, filepath.Join(tree, "GL", "New.h"), "new")

	if err := Lowercase(tree, LowercaseOptions{}); err != nil {
		t.Fatal(err)
	}

	if !isFile(filepath.Join(tree, "gl", "existing.h")) {
		t.Errorf("pre-existing lowercase file lost during merge")
	}
	if !isFile(filepath.Join(tree, "gl", "new.h")) {
		t.Errorf("merged file not found at lowercase destination")
	}
	if isDir(filepath.Join(tree, "GL")) {
		t.Errorf("source dir should have been removed after merge (non-symlink mode)")
	}
}

func TestLowercaseMapWinSDKPreservesGL(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "um")
	mustWriteFile(t, filepath.Join(tree, "GL", "gl.h"), "content")

	if err := Lowercase(tree, LowercaseOptions{Symlink: true, MapWinSDK: true}); err != nil {
		t.Fatal(err)
	}

	if !isDir(filepath.Join(tree, "GL")) {
		t.Errorf("GL directory casing should be preserved under -map_winsdk")
	}
	if isDir(filepath.Join(tree, "gl")) {
		t.Errorf("no lowercase alias should be created for GL under -map_winsdk (name maps back to itself)")
	}
}

func TestFixIncludeLowercasesAndConvertsSlashes(t *testing.T) {
	root := t.TempDir()
	header := filepath.Join(root, "foo.h")
	mustWriteFile(t, header, "#include <Some\\Path.H>\r\n#include \"Other.h\" // comment\r\nplain line\r\n")

	if err := FixInclude(root, FixIncludeOptions{}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(header)
	if err != nil {
		t.Fatal(err)
	}
	want := "#include <some/path.h>\n#include \"other.h\" // comment\nplain line\n"
	if string(data) != want {
		t.Errorf("got %q want %q", data, want)
	}
}

func TestFixIncludeMapWinSDKPreservesGL(t *testing.T) {
	root := t.TempDir()
	header := filepath.Join(root, "foo.h")
	mustWriteFile(t, header, "#include <GL/gl.h>\n")

	if err := FixInclude(root, FixIncludeOptions{MapWinSDK: true}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(header)
	if err != nil {
		t.Fatal(err)
	}
	want := "#include <GL/gl.h>\n"
	if string(data) != want {
		t.Errorf("got %q want %q", data, want)
	}
}

func TestFixIncludeSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "Real.h")
	mustWriteFile(t, real, "#include <Foo.h>\n")
	link := filepath.Join(root, "alias.h")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := FixInclude(root, FixIncludeOptions{}); err != nil {
		t.Fatal(err)
	}

	// The symlink itself must be untouched (still a symlink to the same
	// target); only Real.h's contents get rewritten.
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("alias.h should still be a symlink: %v", err)
	}
	if target != real {
		t.Errorf("symlink target changed: %q", target)
	}
}
