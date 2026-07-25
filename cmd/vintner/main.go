// Command vintner cross compiles with the real MSVC toolchain on Linux
// via Wine. It's a multi-call binary that behaves as `cl`, `link`, `lib`,
// `rc`, `midl`, `mt`, `dumpbin`, `msbuild`, etc. when invoked under one of
// those names (via symlinks set up by `vintner install`) or as
// `vintner <tool> ...` directly (see runTool), and otherwise exposes the
// `download`/`install`/`env`/`version` management subcommands.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Cheviiot/vintner/internal/i18n"
	"github.com/Cheviiot/vintner/internal/wrapper"
)

// version is set at build time via -ldflags "-X main.version=X.Y.Z";
// left as "dev" for plain `go build`/`go run`.
var version = "dev"

func main() {
	base := filepath.Base(os.Args[0])
	name := strings.TrimSuffix(strings.ToLower(base), ".exe")

	if wrapper.IsTool(name) {
		os.Exit(wrapper.Run(name, os.Args[1:], ""))
	}

	os.Exit(runCLI(os.Args[1:]))
}

func runCLI(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	if wrapper.IsTool(args[0]) {
		return runTool(args[0], args[1:])
	}

	switch args[0] {
	case "download", "dl":
		return runDownload(args[1:])
	case "install", "i":
		return runInstall(args[1:])
	case "env", "e":
		return runEnv(args[1:])
	case "completion":
		return runCompletion(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "version", "v", "--version":
		fmt.Println(versionString())
		return 0
	case "-h", "--help", "help", "h":
		printUsage()
		return 0
	default:
		fmt.Fprint(os.Stderr, i18n.T("main.unknown_subcommand", args[0]))
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, i18n.T("main.usage"))
}
