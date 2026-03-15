package main

import (
	"flag"
	"fmt"
	"os"

	"config-nix-installer/internal/installer"
)

func main() {
	repoRoot := flag.String("repo", "", "path to the repository checkout")
	ageKeyFile := flag.String("age-key-file", "", "path to an age key file")
	flag.Parse()

	if *repoRoot == "" {
		fmt.Fprintln(os.Stderr, "usage: rekey-system --repo <path> [--age-key-file <path>]")
		os.Exit(1)
	}

	if err := installer.RunRekey(*repoRoot, *ageKeyFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
