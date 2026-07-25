package wrapper

import (
	"fmt"
	"os"
	"os/exec"
)

// runNative handles the two tool names that need no Wine/MSVC install at
// all: `cmd` (strips a leading "//c" and execs the rest) and `findstr`
// (delegates to grep).
func runNative(tool string, args []string) int {
	switch tool {
	case "cmd":
		if len(args) > 0 && args[0] == "//c" {
			args = args[1:]
		}
		return execInherit(args)
	case "findstr":
		return execInherit(append([]string{"grep"}, args...))
	}
	return 127
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
	stopSignals := forwardSignals(tc.Process)
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
