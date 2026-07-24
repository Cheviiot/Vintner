package main

import (
	"fmt"
	"os"

	"github.com/Cheviiot/msvc-go-wine/internal/install"
)

func runInstall(args []string) int {
	if len(args) != 1 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, "usage: msvc-go-wine install <dest>")
		return 1
	}
	dest := args[0]

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine install:", err)
		return 1
	}

	if err := install.Install(dest, self); err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine install:", err)
		return 1
	}
	fmt.Println("Done. Add", dest+"/bin/<arch> to PATH to use cl, link, lib, ...")
	return 0
}
