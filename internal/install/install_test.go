package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

func TestFindMSVCVersionPicksNewestComplete(t *testing.T) {
	root := t.TempDir()
	// An incomplete version dir (missing "lib") must be skipped even
	// though it lexically sorts newest.
	mustMkdirAll(t, filepath.Join(root, "14.99.99999", "bin"))
	mustMkdirAll(t, filepath.Join(root, "14.99.99999", "include"))
	mustMkdirAll(t, filepath.Join(root, "14.29.30133", "bin"))
	mustMkdirAll(t, filepath.Join(root, "14.29.30133", "include"))
	mustMkdirAll(t, filepath.Join(root, "14.29.30133", "lib"))
	mustMkdirAll(t, filepath.Join(root, "14.16.27023", "bin"))
	mustMkdirAll(t, filepath.Join(root, "14.16.27023", "include"))
	mustMkdirAll(t, filepath.Join(root, "14.16.27023", "lib"))

	got, err := findMSVCVersion(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "14.29.30133" {
		t.Errorf("findMSVCVersion(...) = %q, want %q (the newest complete install, skipping the incomplete 14.99.99999)", got, "14.29.30133")
	}
}

func TestFindMSVCVersionNoneComplete(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "14.29.30133", "bin")) // missing include/lib
	if _, err := findMSVCVersion(root); err == nil {
		t.Error("expected an error when no version dir has bin+include+lib")
	}
}

func TestFindMSVCVersionEmptyDir(t *testing.T) {
	if _, err := findMSVCVersion(t.TempDir()); err == nil {
		t.Error("expected an error for an empty msvcRoot")
	}
}

func TestFindSDKVersionPicksNewest(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"10.0.17763.0", "10.0.26100.0", "10.0.22621.0"} {
		mustMkdirAll(t, filepath.Join(root, v))
	}
	mustMkdirAll(t, filepath.Join(root, "not-an-sdk-dir")) // must be ignored (no "10." prefix)

	got, err := findSDKVersion(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.0.26100.0" {
		t.Errorf("findSDKVersion(...) = %q, want %q", got, "10.0.26100.0")
	}
}

func TestFindSDKVersionNoneFound(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "not-an-sdk-version"))
	if _, err := findSDKVersion(root); err == nil {
		t.Error("expected an error when no dir starts with \"10.\"")
	}
}

func TestHostArch(t *testing.T) {
	wantHost, wantDotnet := "x64", "amd64"
	if runtime.GOARCH == "arm64" {
		wantHost, wantDotnet = "arm64", "arm64"
	}
	host, dotnetHost := hostArch()
	if host != wantHost || dotnetHost != wantDotnet {
		t.Errorf("hostArch() = (%q, %q), want (%q, %q) for GOARCH=%q", host, dotnetHost, wantHost, wantDotnet, runtime.GOARCH)
	}
}

func TestLnSCreatesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := lnS("real.txt", link); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("expected %s to be a symlink: %v", link, err)
	}
	if got != "real.txt" {
		t.Errorf("symlink target = %q, want %q", got, "real.txt")
	}
}

func TestLnSNoopIfAlreadyCorrect(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink("real.txt", link); err != nil {
		t.Fatal(err)
	}
	if err := lnS("real.txt", link); err != nil {
		t.Fatalf("lnS on an already-correct link should be a no-op, not error: %v", err)
	}
	got, _ := os.Readlink(link)
	if got != "real.txt" {
		t.Errorf("symlink target = %q, want unchanged %q", got, "real.txt")
	}
}

// TestLnSRefreshesStaleSymlink covers the real install-time bug this
// exists to fix: a symlink left over from a previous install (e.g. before
// the tool's per-arch binary was renamed, or before a `vintner install`
// re-run after rebuilding the binary) pointing at the wrong target used to
// be silently left alone forever - `cl`/`link`/etc. would keep dispatching
// to a stale binary with no indication anything was wrong. Found by hand
// against a real installation still carrying symlinks to a pre-rename
// binary name.
func TestLnSRefreshesStaleSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink("something-else", link); err != nil {
		t.Fatal(err)
	}
	if err := lnS("real.txt", link); err != nil {
		t.Fatalf("lnS on a stale symlink should refresh it, not error: %v", err)
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("expected %s to still be a symlink: %v", link, err)
	}
	if got != "real.txt" {
		t.Errorf("stale symlink target = %q, want refreshed to %q", got, "real.txt")
	}
}

// TestLnSLeavesRealFileAlone confirms lnS only ever refreshes symlinks it
// itself would have created - something that isn't a symlink at all at the
// link path (a real file, however it got there) is left completely
// untouched rather than replaced.
func TestLnSLeavesRealFileAlone(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(link, []byte("not a symlink"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lnS("real.txt", link); err != nil {
		t.Fatalf("lnS on a real file should be a no-op, not error: %v", err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("a real file at the link path was replaced with a symlink")
	}
	content, err := os.ReadFile(link)
	if err != nil || string(content) != "not a symlink" {
		t.Errorf("real file content changed: %q, %v", content, err)
	}
}

func TestCopyFileOverwritesDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("new content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("stale content that must be replaced, not appended to"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Errorf("dst content = %q, want %q (stale content should have been fully replaced)", got, "new content")
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("dst mode = %v, want 0755", fi.Mode().Perm())
	}
}

func TestFixLibCasingAddsUppercaseSymlinks(t *testing.T) {
	root := t.TempDir()
	x64 := filepath.Join(root, "x64")
	mustMkdirAll(t, x64)
	if err := os.WriteFile(filepath.Join(x64, "libcmt.lib"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "msvcrt.lib" deliberately absent - fixLibCasing must skip names that
	// aren't actually present, not error out.

	if err := fixLibCasing(root); err != nil {
		t.Fatal(err)
	}

	upper := filepath.Join(x64, "LIBCMT.lib")
	target, err := os.Readlink(upper)
	if err != nil {
		t.Fatalf("expected %s to be a symlink to libcmt.lib: %v", upper, err)
	}
	if target != "libcmt.lib" {
		t.Errorf("symlink target = %q, want %q", target, "libcmt.lib")
	}
	if _, err := os.Lstat(filepath.Join(x64, "MSVCRT.lib")); err == nil {
		t.Error("MSVCRT.lib symlink should not have been created - msvcrt.lib was never present")
	}
}

// TestEnsureBundledWineNoOpsWithoutKnownArtifact covers an arch with no
// published wine artifact (wineSHA256 in internal/download/wine.go only
// has entries for the real published amd64/arm64 builds - see the
// reflective-orbiting-sprout plan) - must be a graceful, silent miss, not
// a panic or a loud failure, exactly like bootstrapWine's existing "no
// system wine either" fallback. Deliberately uses a made-up arch rather
// than calling ensureBundledWine(home) directly: this machine's own real
// arch *does* have a genuine published hash now, and a unit test must
// never reach the real network.
func TestEnsureBundledWineNoOpsWithoutKnownArtifact(t *testing.T) {
	home := t.TempDir()

	ensureBundledWineForArch(home, "does-not-exist-arch") // must not panic

	if _, ok := wineenv.FindBundledWine(home); ok {
		t.Error("expected no bundled wine to have been installed (no artifact is known for this arch)")
	}
}

// TestEnsureBundledWineSkipsWhenAlreadyPresent confirms a matching bundled
// wine already on disk is left untouched rather than re-fetched - the
// FindBundledWine check must run, and short-circuit, before any download
// attempt.
func TestEnsureBundledWineSkipsWhenAlreadyPresent(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "wine", wineenv.BundledWineVersion, "bin")
	mustMkdirAll(t, binDir)
	winePath := filepath.Join(binDir, "wine")
	const marker = "already installed - must not be touched"
	if err := os.WriteFile(winePath, []byte(marker), 0o755); err != nil {
		t.Fatal(err)
	}

	ensureBundledWine(home)

	got, err := os.ReadFile(winePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != marker {
		t.Errorf("existing bundled wine was modified: got %q, want it left untouched (%q)", got, marker)
	}
}
