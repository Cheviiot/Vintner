package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Cheviiot/msvc-go-wine/internal/wineenv"
)

// runEnv prints shell `export` statements for INCLUDE/LIB (converted from
// wine's "z:\..." notation to plain unix paths) and TARGET_TRIPLE, for
// driving clang-cl/lld-link directly without Wine.
// Usage: eval "$(msvc-go-wine env --bin <dest>/bin/<arch>)"
func runEnv(args []string) int {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	bin := fs.String("bin", "", "the <dest>/bin/<arch> directory produced by `msvc-go-wine install`")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bin == "" {
		fmt.Fprintln(os.Stderr, "usage: msvc-go-wine env --bin <dest>/bin/<arch>")
		return 1
	}

	cfg, err := wineenv.Load(*bin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine env:", err)
		return 1
	}
	baseUnix, err := wineenv.FindBaseUnix(*bin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine env:", err)
		return 1
	}
	paths := wineenv.NewPaths(cfg, baseUnix)

	triple, ok := targetTriples[cfg.Arch]
	if !ok {
		fmt.Fprintf(os.Stderr, "msvc-go-wine env: unknown arch %q\n", cfg.Arch)
		return 1
	}

	fmt.Printf("export INCLUDE=%q\n", toUnixPathList(paths.Include))
	fmt.Printf("export LIB=%q\n", toUnixPathList(paths.Lib))
	fmt.Printf("export TARGET_TRIPLE=%q\n", triple)
	return 0
}

var targetTriples = map[string]string{
	"x86":   "i686-windows-msvc",
	"x64":   "x86_64-windows-msvc",
	"arm":   "armv7-windows-msvc",
	"arm64": "aarch64-windows-msvc",
}

// toUnixPathList does a blanket removal of the "z:" drive prefix and
// backslash->slash conversion across the whole semicolon-joined path list.
func toUnixPathList(s string) string {
	s = strings.ReplaceAll(s, "z:", "")
	s = strings.ReplaceAll(s, `\`, "/")
	return s
}
