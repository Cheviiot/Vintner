// Command msvc-go-wine cross compiles with the real MSVC toolchain on Linux
// via Wine. It's a multi-call binary that behaves as `cl`, `link`, `lib`,
// `rc`, `midl`, `mt`, `dumpbin`, `msbuild`, etc. when invoked under one of
// those names (via symlinks set up by `msvc-go-wine install`), and
// otherwise exposes the `download`/`install`/`env`/`version` management
// subcommands.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Cheviiot/msvc-go-wine/internal/wrapper"
)

func main() {
	base := filepath.Base(os.Args[0])
	name := strings.TrimSuffix(strings.ToLower(base), ".exe")

	if _, ok := wrapper.Tools[name]; ok {
		os.Exit(wrapper.Run(name, os.Args[1:]))
	}
	if name == "cmd" || name == "findstr" {
		os.Exit(wrapper.Run(name, os.Args[1:]))
	}

	os.Exit(runCLI(os.Args[1:]))
}

func runCLI(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	switch args[0] {
	case "download":
		return runDownload(args[1:])
	case "install":
		return runInstall(args[1:])
	case "env":
		return runEnv(args[1:])
	case "version":
		fmt.Println("msvc-go-wine dev")
		return 0
	case "-h", "--help", "help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "msvc-go-wine: unknown subcommand %q\n\n", args[0])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `msvc-go-wine - cross compile with MSVC on Linux via Wine

Usage:
  msvc-go-wine download --dest <dir> [options]   fetch and unpack MSVC/WinSDK
  msvc-go-wine install <dir>                     wire up wrappers for a downloaded MSVC
  msvc-go-wine env --bin <dir/bin/arch>           print INCLUDE/LIB for native clang-cl/lld-link use
  msvc-go-wine version                           print the version

Once installed, add <dir>/bin/<arch> to PATH and invoke the tools directly:
  cl, link, lib, ml, ml64, mc, midl, mt, rc, dumpbin, msbuild, nmake, armasm, armasm64, cmd, findstr
`)
}
