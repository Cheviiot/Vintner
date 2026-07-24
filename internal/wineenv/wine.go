package wineenv

import (
	"fmt"
	"os/exec"
)

// FindWine locates the wine binary to use, preferring wine64 like the
// original wrappers (`command -v wine64 || command -v wine`).
func FindWine() (string, error) {
	if p, err := exec.LookPath("wine64"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("wine"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("neither wine64 nor wine found in PATH")
}
