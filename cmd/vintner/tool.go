package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Cheviiot/vintner/internal/wrapper"
)

// runTool dispatches `vintner <tool> [args...]` (cl, link, msbuild, ...)
// directly, without needing <dest>/bin/<arch> on PATH or a same-directory
// symlink pointing back at this binary.
//
// Resolves which toolchain bin dir to use from VINTNER_BIN if set (same
// meaning as `env --bin`: point it directly at a <dest>/bin/<arch>
// directory - useful for a non-default --dest, or to pick a specific
// architecture when more than one is installed), else defaults to
// <defaultToolchainDir>/bin/<hostArch>, the layout a plain `vintner
// download && vintner install` with no --dest override produces.
func runTool(tool string, args []string) int {
	binDir := os.Getenv("VINTNER_BIN")
	if binDir == "" {
		def, err := defaultToolchainDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "vintner:", err)
			return 1
		}
		binDir = filepath.Join(def, "bin", detectHostArch())
	}
	if fi, err := os.Stat(binDir); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr,
			"vintner: no installed toolchain found at %s\n"+
				"Run `vintner download --accept-license && vintner install` first, "+
				"or set VINTNER_BIN to an existing <dest>/bin/<arch> directory.\n",
			binDir)
		return 1
	}
	return wrapper.Run(tool, args, binDir)
}
