package main

import (
	"flag"
	"fmt"
	"os"

	"config-nix-installer/internal/installer"
)

func main() {
	repoRoot := flag.String("repo", "/etc/nixos", "path to the installed repository")
	markerPath := flag.String("marker-path", "/var/lib/config-nix/finalize-pending", "path to the finalize marker file")
	statusPath := flag.String("status-path", "/var/lib/config-nix/finalize-status.json", "path to the finalize status file")
	flag.Parse()

	if err := installer.RunFinalize(installer.FinalizeRequest{
		RepoRoot:   *repoRoot,
		MarkerPath: *markerPath,
		StatusPath: *statusPath,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
