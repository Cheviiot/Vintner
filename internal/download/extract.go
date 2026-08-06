package download

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// UnpackSelectedPackages unpacks every selected package's cached payloads
// into unpack (VSIX packages as plain zips, Win10SDK/Win11SDK via the
// external msiextract).
func UnpackSelectedPackages(selected []*Package, cacheDir, unpack string) error {
	if err := os.MkdirAll(unpack, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(unpack, "MSBuild"), 0o755); err != nil {
		return err
	}
	for _, p := range selected {
		dir := filepath.Join(cacheDir, p.Key())
		switch p.Type {
		case "Component", "Workload", "Group":
			continue
		case "Vsix":
			fmt.Println("Unpacking", p.ID)
			for _, pl := range p.Payloads {
				listing := filepath.Join(unpack, p.Key()+"-listing.txt")
				if err := extractVSIXPackage(filepath.Join(dir, pl.Name()), unpack, listing); err != nil {
					return fmt.Errorf("unpacking %s: %w", p.ID, err)
				}
			}
		default:
			if strings.HasPrefix(p.ID, "Win10SDK") || strings.HasPrefix(p.ID, "Win11SDK") {
				fmt.Println("Unpacking", p.ID)
				if err := extractWindowsSDKPackage(dir, p.Payloads, unpack); err != nil {
					return fmt.Errorf("unpacking %s: %w", p.ID, err)
				}
			} else {
				fmt.Println("Skipping unpacking of", p.ID, "of type", p.Type)
			}
		}
	}
	return nil
}

// extractVSIXPackage extracts a VSIX (plain zip) into a scratch dir under
// dest, then merges its "Contents" (and WDK's "$MSBuild") subtree into
// dest.
func extractVSIXPackage(file, dest, listingPath string) error {
	r, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer r.Close()

	tmp := filepath.Join(dest, "vsix")
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}

	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
		name, err := url.PathUnescape(f.Name)
		if err != nil {
			name = f.Name
		}
		target, err := safeJoin(tmp, name)
		if err != nil {
			return fmt.Errorf("extracting %s: %w", file, err)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractZipEntry(f, target); err != nil {
			return err
		}
	}
	if err := os.WriteFile(listingPath, []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil {
		return err
	}

	if contents := filepath.Join(tmp, "Contents"); isDir(contents) {
		if err := combineDirTrees(contents, dest); err != nil {
			return err
		}
	}
	// This archive directory structure is used by the WDK.vsix.
	if msbuild := filepath.Join(tmp, "$MSBuild"); isDir(msbuild) {
		if err := combineDirTrees(msbuild, filepath.Join(dest, "MSBuild")); err != nil {
			return err
		}
	}
	return os.RemoveAll(tmp)
}

// safeJoin joins base and name, rejecting the result if it would resolve
// outside base ("zip slip": archive/zip doesn't sanitize entry names
// itself, so a malicious/malformed VSIX with an entry like
// "../../../../home/user/.bashrc" would otherwise write wherever the zip
// says to via a plain filepath.Join).
func safeJoin(base, name string) (string, error) {
	target := filepath.Join(base, name)
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("zip entry %q escapes extraction directory %s", name, base)
	}
	return target, nil
}

func extractZipEntry(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	mode := f.Mode()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// extractWindowsSDKPackage extracts every .msi payload of a WinSDK package
// via msiextract, and symlinks "Program Files" to "." so files msiextract
// unpacks there land at the unpack root (matching msiexec's own behavior on
// Windows).
func extractWindowsSDKPackage(src string, payloads []Payload, dest string) error {
	pf := filepath.Join(dest, "Program Files")
	if !exists(pf) {
		if err := os.Symlink(".", pf); err != nil {
			return err
		}
	}
	for _, pl := range payloads {
		name := pl.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".msi") {
			continue
		}
		fmt.Println("Extracting", name)
		srcFile := filepath.Join(src, name)
		logPath := filepath.Join(dest, "WinSDK-"+name+"-listing.txt")
		if err := runMsiExtract(srcFile, dest, logPath); err != nil {
			return fmt.Errorf("msiextract %s: %w", name, err)
		}
	}
	return nil
}

func runMsiExtract(srcFile, dest, logPath string) error {
	if _, err := exec.LookPath("msiextract"); err != nil {
		return fmt.Errorf("msiextract not found in PATH (install the msitools package): %w", err)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command("msiextract", "-C", dest, srcFile)
	cmd.Stdout = logFile
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RelocateBuildTools relocates the components CLI tools actually need (VC,
// Windows Kits, DIA SDK, MSBuild, Common7/Tools) from the scratch unpack
// dir into dest, so the rest of unpack can be discarded.
func RelocateBuildTools(unpack, dest string) error {
	components := []string{"VC", "Windows Kits", "DIA SDK", "MSBuild", filepath.Join("Common7", "Tools")}
	for _, c := range components {
		if err := combineDirTrees(filepath.Join(unpack, c), filepath.Join(dest, c)); err != nil {
			return fmt.Errorf("moving %s: %w", c, err)
		}
	}
	return nil
}

// combineDirTrees moves src into dest, recursively merging where a
// same-named directory already exists in dest (matching case-insensitively)
// rather than clobbering it - MSVC/WinSDK packages aren't casing-consistent
// about where they put things.
func combineDirTrees(src, dest string) error {
	if !isDir(src) {
		return nil
	}
	if !isDir(dest) {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.Rename(src, dest)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	destEntries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	destNames := map[string]string{}
	for _, e := range destEntries {
		destNames[strings.ToLower(e.Name())] = e.Name()
	}

	for _, e := range entries {
		n := e.Name()
		srcName := filepath.Join(src, n)
		destName := filepath.Join(dest, n)
		if e.IsDir() {
			if isDir(destName) {
				if err := combineDirTrees(srcName, destName); err != nil {
					return err
				}
				continue
			}
			if actual, ok := destNames[strings.ToLower(n)]; ok {
				if err := combineDirTrees(srcName, filepath.Join(dest, actual)); err != nil {
					return err
				}
				continue
			}
			if err := os.Rename(srcName, destName); err != nil {
				return err
			}
			continue
		}
		if err := os.Rename(srcName, destName); err != nil {
			return err
		}
	}
	return nil
}

// CopyRedirectedAssemblies works around Wine not honoring <dependentAssembly>
// codeBase redirects in an .exe.config file, by copying the referenced DLL
// next to the executable directly.
func CopyRedirectedAssemblies(app string) error {
	cfgPath := app + ".config"
	if !isFile(cfgPath) {
		return nil
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	var cfg struct {
		Runtime struct {
			AssemblyBinding struct {
				DependentAssembly []struct {
					CodeBase struct {
						Href string `xml:"href,attr"`
					} `xml:"codeBase"`
				} `xml:"dependentAssembly"`
			} `xml:"assemblyBinding"`
		} `xml:"runtime"`
	}
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing %s: %w", cfgPath, err)
	}

	dest := filepath.Dir(app)
	for _, da := range cfg.Runtime.AssemblyBinding.DependentAssembly {
		href := strings.ReplaceAll(da.CodeBase.Href, "\\", "/")
		if href == "" {
			continue
		}
		src := filepath.Join(dest, href)
		if isFile(src) {
			if err := copyFile(src, filepath.Join(dest, filepath.Base(src)), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	_ = os.Remove(dst)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
