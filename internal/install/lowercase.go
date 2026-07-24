package install

import (
	"os"
	"path/filepath"
	"strings"
)

// LowercaseOptions controls how Lowercase renames entries.
type LowercaseOptions struct {
	// Symlink adds lowercase-named symlinks alongside the original entries
	// instead of renaming them in place (used for WinSDK/MSVC headers so
	// both casings stay available; renaming would break other packages
	// that reference the original casing).
	Symlink bool
	// MapWinSDK is shorthand for path-keyed overrides that keep the "GL"
	// header directory's canonical uppercase spelling.
	MapWinSDK bool
}

// Lowercase recursively lowercases every file/dir name under root, merging
// into an existing same-named lowercase directory on collision.
func Lowercase(root string, opts LowercaseOptions) error {
	mapPaths := map[string]string{}
	if opts.MapWinSDK {
		mapPaths["gl"] = "GL"
	}

	remap := func(relPath string) string {
		rp := strings.TrimSuffix(relPath, "/")
		base := rp
		if idx := strings.LastIndexByte(rp, '/'); idx >= 0 {
			base = rp[idx+1:]
		}
		if opts.MapWinSDK {
			if v, ok := mapPaths[strings.ToLower(rp)]; ok {
				return v
			}
		}
		return strings.ToLower(base)
	}

	var doDir func(dir, relPath string) error
	doDir = func(dir, relPath string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			name := e.Name()
			childPath := filepath.Join(dir, name)
			if e.IsDir() {
				if err := doDir(childPath, relPath+name+"/"); err != nil {
					return err
				}
				continue
			}
			newName := remap(relPath + name)
			if newName != name {
				if err := renameOrSymlink(childPath, dir, newName, opts.Symlink); err != nil {
					return err
				}
			}
		}

		var newName string
		if relPath == "" {
			newName = strings.ToLower(filepath.Base(dir))
		} else {
			newName = remap(relPath)
		}
		oldName := filepath.Base(dir)
		if oldName == newName {
			return nil
		}
		parent := filepath.Dir(dir)
		newPath := filepath.Join(parent, newName)
		if fi, err := os.Stat(newPath); err == nil && fi.IsDir() {
			return combineIntoDir(dir, newPath, opts.Symlink)
		}
		return renameOrSymlink(dir, parent, newName, opts.Symlink)
	}

	return doDir(root, "")
}

func renameOrSymlink(src, destDir, destName string, symlink bool) error {
	dest := filepath.Join(destDir, destName)
	if symlink {
		if _, err := os.Lstat(dest); err == nil {
			// A conflicting entry already exists at dest - this happens on
			// case-insensitive filesystems where dest and src are the same
			// path, so treat it as already done rather than failing.
			return nil
		}
		rel, err := filepath.Rel(destDir, src)
		if err != nil {
			rel = src
		}
		return os.Symlink(rel, dest)
	}
	return os.Rename(src, dest)
}

// combineIntoDir moves (or symlinks) every entry of src into the already-existing
// dest, recursing into same-named subdirectories, then removes src (unless
// symlink mode, where src's real content must be left in place).
func combineIntoDir(src, dest string, symlink bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		srcChild := filepath.Join(src, name)
		destChild := filepath.Join(dest, name)
		if fi, err := os.Stat(destChild); err == nil && fi.IsDir() && e.IsDir() {
			if err := combineIntoDir(srcChild, destChild, symlink); err != nil {
				return err
			}
			continue
		}
		if err := renameOrSymlink(srcChild, dest, name, symlink); err != nil {
			return err
		}
	}
	if !symlink {
		return os.Remove(src)
	}
	return nil
}
