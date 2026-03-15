package installer

import (
	"fmt"
	"os"
	"os/exec"
)

func nixosRebuildBinary() string {
	const currentSystemRebuild = "/run/current-system/sw/bin/nixos-rebuild"
	if _, err := os.Stat(currentSystemRebuild); err == nil {
		return currentSystemRebuild
	}
	return "nixos-rebuild"
}

func rebuildCommand(action, repoRoot string, extraArgs []string) []string {
	command := []string{
		nixosRebuildBinary(),
		action,
		"--impure",
		"--flake", fmt.Sprintf("path:%s#default", repoRoot),
	}
	return append(command, extraArgs...)
}

func RunRebuild(repoRoot string, args []string) error {
	repoRoot, err := normalizeRepoRoot(repoRoot)
	if err != nil {
		return err
	}
	if err := ensureFlakeRepo(repoRoot); err != nil {
		return err
	}

	action := "switch"
	extraArgs := args
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		action = args[0]
		extraArgs = args[1:]
	}

	commandArgs := rebuildCommand(action, repoRoot, extraArgs)
	command := exec.Command(commandArgs[0], commandArgs[1:]...)
	command.Env = os.Environ()
	for key, value := range localStateEnv(repoRoot) {
		command.Env = append(command.Env, key+"="+value)
	}
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Stdin = os.Stdin
	return command.Run()
}
