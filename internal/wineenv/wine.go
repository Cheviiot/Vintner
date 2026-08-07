package wineenv

import (
	"fmt"
	"os"
	"os/exec"
)

// FindWine locates the wine binary to use: VINTNER_WINE (an explicit
// override - pointing this at a self-built/bundled wine tree is how the
// rest of vintner's own wine invocation, env setup, and process-lifecycle
// code gets exercised against one without any further code changes) takes
// priority, then wine64, then wine, matching the original wrappers'
// `command -v wine64 || command -v wine`.
func FindWine() (string, error) {
	if p := os.Getenv("VINTNER_WINE"); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("wine64"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("wine"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("neither wine64 nor wine found in PATH (install the wine package)")
}
