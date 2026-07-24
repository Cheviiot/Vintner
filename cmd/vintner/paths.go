package main

import (
	"os"
	"path/filepath"
)

// defaultToolchainDir is where `download`/`install` operate when the user
// doesn't specify a directory: a hidden ~/.vintner, so it doesn't
// clutter a plain `ls ~`.
func defaultToolchainDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vintner"), nil
}
