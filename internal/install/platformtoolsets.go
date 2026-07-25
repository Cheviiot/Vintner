package install

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

var rePlatformToolsetDir = regexp.MustCompile(`^v(\d+)$`)

// aliasPlatformToolsets makes every historical PlatformToolset name in
// wineenv.KnownPlatformToolsets resolve to the one compiler `download`
// actually fetched.
//
// MSBuild decides whether a PlatformToolset is "installed" at all - the
// check behind MSB8020 - by testing whether
// MSBuild/Microsoft/VC/v<schema>/Platforms/<arch>/PlatformToolsets/<toolset>/
// exists on disk (Microsoft.Cpp.props, via
// ToolLocationHelper.FindRootFolderWhereAllFilesExist). That's a plain file
// lookup, not influenced by any environment variable - unlike the later,
// env-var-driven VCInstallDir_<N> checks internal/wrapper's msbuildEnv
// covers, this one needs the actual directory to exist under dest.
// Microsoft's own downloaded MSBuild package only ships a PlatformToolsets
// entry for the exact generation matching the fetched compiler, so any
// project pinned to an older PlatformToolset (v142 for a project last saved
// under VS2019, say) fails this check outright even though the one real
// toolchain installed could easily build it.
//
// Toolset.props/Toolset.targets don't hardcode a version number (they just
// import version-agnostic files like Microsoft.Cpp.MSVC.Toolset.<arch>.props),
// so a symlink under any other historical name is a correct, transparent
// alias rather than a divergent copy.
func aliasPlatformToolsets(dest string) error {
	schemaDirs, err := filepath.Glob(filepath.Join(dest, "MSBuild", "Microsoft", "VC", "v*"))
	if err != nil {
		return err
	}
	for _, schemaDir := range schemaDirs {
		archDirs, err := filepath.Glob(filepath.Join(schemaDir, "Platforms", "*", "PlatformToolsets"))
		if err != nil {
			return err
		}
		for _, toolsetsDir := range archDirs {
			if err := aliasOneDir(toolsetsDir); err != nil {
				return err
			}
		}
	}
	return nil
}

// aliasOneDir symlinks every name in wineenv.KnownPlatformToolsets that
// doesn't already exist in toolsetsDir onto whichever real v<N> toolset
// subdirectory is actually present there.
func aliasOneDir(toolsetsDir string) error {
	entries, err := os.ReadDir(toolsetsDir)
	if err != nil {
		return err
	}
	var real string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if rePlatformToolsetDir.MatchString(e.Name()) {
			real = e.Name()
			break
		}
	}
	if real == "" {
		// Nothing numeric here (e.g. only the WindowsKernelModeDriver10.0-style
		// WDK toolsets) - nothing to alias.
		return nil
	}

	for _, n := range wineenv.KnownPlatformToolsets {
		alias := "v" + n
		if alias == real {
			continue
		}
		aliasPath := filepath.Join(toolsetsDir, alias)
		if exists(aliasPath) {
			continue
		}
		if err := os.Symlink(real, aliasPath); err != nil {
			return err
		}
	}
	return nil
}
