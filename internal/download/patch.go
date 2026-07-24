package download

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Cheviiot/msvc-go-wine/assets"
)

// ApplyCompatibilityFixes applies the embedded Wine compatibility patches
// (assets/patches) against a freshly unpacked+moved dest tree: a
// "foo.props.patch" is git-applied over dest/foo.props (skipped if already
// applied), a "foo.patch"-less file is copied verbatim, and a "foo.remove"
// marker deletes dest/foo.
func ApplyCompatibilityFixes(dest string) error {
	return fs.WalkDir(assets.Patches, "patches", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, "patches/")
		ext := filepath.Ext(rel)
		target := strings.TrimSuffix(rel, ext)

		switch ext {
		case ".patch":
			return applyGitPatch(dest, path, target)
		case ".remove":
			full := filepath.Join(dest, target)
			if isFile(full) {
				fmt.Println("Removing", target)
				return os.Remove(full)
			}
			return nil
		default:
			fmt.Println("Copying", rel)
			data, err := assets.Patches.ReadFile(path)
			if err != nil {
				return err
			}
			full := filepath.Join(dest, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			return os.WriteFile(full, data, 0o644)
		}
	})
}

func applyGitPatch(dest, embeddedPath, target string) error {
	full := filepath.Join(dest, target)
	if !isFile(full) {
		return nil
	}
	data, err := assets.Patches.ReadFile(embeddedPath)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "msvc-go-wine-*.patch")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	// Skip if the patch has already been applied (reverse-apply check),
	// so re-running download/install stays idempotent.
	check := exec.Command("git", "--work-tree=.", "apply", "--quiet", "--reverse", "--check", tmp.Name())
	check.Dir = dest
	if check.Run() == nil {
		return nil
	}

	fmt.Println("Patching", target)
	apply := exec.Command("git", "--work-tree=.", "apply", tmp.Name())
	apply.Dir = dest
	apply.Stderr = os.Stderr
	return apply.Run()
}
