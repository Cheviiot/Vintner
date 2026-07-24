package main

import (
	"fmt"
	"os"

	"github.com/Cheviiot/msvc-go-wine/internal/i18n"
	"github.com/Cheviiot/msvc-go-wine/internal/install"
)

func runInstall(args []string) int {
	if len(args) > 1 || (len(args) == 1 && (args[0] == "-h" || args[0] == "--help")) {
		fmt.Fprintln(os.Stderr, i18n.T("install.usage"))
		return 1
	}

	var dest string
	if len(args) == 1 {
		dest = args[0]
	} else {
		def, err := defaultToolchainDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "msvc-go-wine install:", err)
			return 1
		}
		dest = def
		fmt.Println(i18n.T("install.default_dir", dest))
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine install:", err)
		return 1
	}

	if err := install.Install(dest, self); err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine install:", err)
		return 1
	}
	fmt.Println(i18n.T("install.done", dest+"/bin/<arch>"))
	return 0
}
