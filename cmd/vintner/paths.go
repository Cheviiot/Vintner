package main

import (
	"github.com/Cheviiot/vintner/internal/wineenv"
)

// defaultToolchainDir is where `download`/`install` operate when the user
// doesn't specify a directory: a hidden ~/.vintner, so it doesn't
// clutter a plain `ls ~` - the same root wineenv.DefaultHome resolves the
// bundled wine and its prefix under.
func defaultToolchainDir() (string, error) {
	return wineenv.DefaultHome()
}
