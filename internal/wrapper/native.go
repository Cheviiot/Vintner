package wrapper

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runNative handles the two tool names that need no Wine/MSVC install at
// all: `cmd` (interprets a small, deliberately narrow subset of cmd.exe's
// command-line syntax - see parseCmdLine) and `findstr` (delegates to
// grep).
func runNative(tool string, args []string) int {
	switch tool {
	case "cmd":
		return execInherit(parseCmdLine(args))
	case "findstr":
		return execInherit(append([]string{"grep"}, args...))
	}
	return 127
}

// parseCmdLine strips cmd.exe's own switches and any leading
// "call <setup-script> ... && ... && " chain a caller prepends to set up a
// real Windows build environment before running the command it actually
// cares about - Boost.Build's msvc toolset does exactly this
// ("cmd /S /C call vcvarsall.bat ... && cl ..."), since real MSVC normally
// needs vcvarsall.bat run first to populate INCLUDE/LIB/PATH. vintner's own
// cl/link/lib wrappers already set those up themselves, so that setup step
// is both unneeded and - being a real .bat invocation - unsupported here;
// only the final "&&"-separated command actually needs to run.
//
// Two forms are accepted:
//   - "//c <command...>" (a single command, no chaining - MSBuild's own
//     PostBuildEvent/CustomBuildStep steps use this)
//   - "/S /C <anything> && <anything> && ... && <command...>" (only the
//     last "&&"-separated segment is executed)
func parseCmdLine(args []string) []string {
	if len(args) > 0 && args[0] == "//c" {
		return args[1:]
	}

	rest := args
	if len(rest) > 0 && strings.EqualFold(rest[0], "/S") {
		rest = rest[1:]
	}
	if len(rest) > 0 && strings.EqualFold(rest[0], "/C") {
		rest = rest[1:]
	}

	last := rest
	for i, a := range rest {
		if a == "&&" {
			last = rest[i+1:]
		}
	}
	return last
}

func execInherit(args []string) int {
	if len(args) == 0 {
		return 0
	}
	tc, cleanup := newToolCommand(args[0], args[1:]...)
	defer cleanup()
	tc.Stdin = os.Stdin
	tc.Stdout = os.Stdout
	tc.Stderr = os.Stderr
	if err := tc.Start(); err != nil {
		return 127
	}
	stopSignals := forwardSignals(tc)
	defer stopSignals()
	if err := tc.Wait(); err != nil {
		if tc.timedOut() {
			fmt.Fprintln(os.Stderr, tc.timeoutMessage())
			return 124
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 127
	}
	return 0
}
