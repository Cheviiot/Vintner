package wrapper

import (
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
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 127
	}
	return 0
}
