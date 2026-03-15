package main

import (
	"flag"
	"fmt"
	"os"

	"config-nix-installer/internal/installer"
)

func main() {
	repoRoot := flag.String("repo", "/etc/nixos", "path to the repository checkout")
	flag.Parse()

	if err := installer.RunRebuild(*repoRoot, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
