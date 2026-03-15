package installer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func LoadSession() (Session, func(), error) {
	ensureNixConfig()
	repoRoot, sourceKind, cleanup, err := prepareInstallRepoRoot()
	if err != nil {
		return Session{}, nil, err
	}

	disks, err := listDisks()
	if err != nil {
		cleanup()
		return Session{}, nil, err
	}

	settings, err := loadSharedSettings(repoRoot)
	if err != nil {
		cleanup()
		return Session{}, nil, err
	}
	if err := assertOwnerRecipientsReady(settings); err != nil {
		cleanup()
		return Session{}, nil, err
	}

	plan, err := loadInstallPlan(repoRoot)
	if err != nil {
		cleanup()
		return Session{}, nil, err
	}

	detected, err := detectHardware("/")
	if err != nil {
		cleanup()
		return Session{}, nil, err
	}

	requiredTools := map[string]bool{}
	for _, tool := range []string{
		"disko",
		"findmnt",
		"git",
		"lsblk",
		"mkpasswd",
		"nix",
		"nixos-generate-config",
		"nixos-install",
		"sops",
		"tar",
	} {
		requiredTools[tool] = ensureTool(tool) == nil
	}

	return Session{
		RepoRoot: repoRoot,
		Preflight: Preflight{
			UEFI:          fileExists("/sys/firmware/efi"),
			Revision:      flakeRevisionLabel(repoRoot),
			RepoRoot:      repoRoot,
			SourceKind:    sourceKind,
			RequiredTools: requiredTools,
		},
		Disks:       disks,
		UserName:    settings.User.Name,
		InstallPlan: plan,
		Detected:    detected,
	}, cleanup, nil
}

func SecretStatusFor(repoRoot, ageKeyFile string) (SecretStatus, error) {
	return secretStatus(repoRoot, ageKeyFile)
}

func emit(sink func(Event), event Event) {
	if sink != nil {
		sink(event)
	}
}

func diskoCommand(configPath string) []string {
	return []string{
		"disko",
		"--mode", "destroy,format,mount",
		"--yes-wipe-all-disks",
		configPath,
	}
}

func nixosInstallCommand(mountPoint, repoRoot, output string) []string {
	return []string{
		"nixos-install",
		"--root", mountPoint,
		"--flake", fmt.Sprintf("path:%s#%s", repoRoot, output),
		"--impure",
		"--no-root-passwd",
	}
}

func renderDiskoConfig(diskoPath, disk, luksPasswordFile string) string {
	return fmt.Sprintf("(import %q {\n  diskDevice = %q;\n  passwordFile = %q;\n})\n", diskoPath, disk, luksPasswordFile)
}

func streamCommand(sink func(Event), phase Phase, env map[string]string, cmd []string) error {
	command := exec.Command(cmd[0], cmd[1:]...)
	command.Env = os.Environ()
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	command.Stderr = command.Stdout
	if err := command.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		emit(sink, Event{Kind: EventPhaseLog, Phase: phase, Message: line, RawLine: line})
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	if err := command.Wait(); err != nil {
		return fmt.Errorf("%s failed: %w", strings.Join(cmd, " "), err)
	}
	return nil
}

func copyRepoSnapshot(repoRoot, targetRoot string) error {
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return err
	}

	create := exec.Command(
		"tar",
		"--exclude=./result",
		"--exclude=./result-*",
		"--exclude=./.direnv",
		"-C", repoRoot,
		"-cf", "-",
		".",
	)
	extract := exec.Command("tar", "-C", targetRoot, "-xf", "-")

	reader, writer := io.Pipe()
	create.Stdout = writer
	var createErr strings.Builder
	var extractErr strings.Builder
	create.Stderr = &createErr
	extract.Stderr = &extractErr
	extract.Stdin = reader

	if err := create.Start(); err != nil {
		return err
	}
	if err := extract.Start(); err != nil {
		return err
	}

	createWait := create.Wait()
	_ = writer.Close()
	extractWait := extract.Wait()
	_ = reader.Close()

	if createWait != nil {
		msg := strings.TrimSpace(createErr.String())
		if msg == "" {
			msg = createWait.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	if extractWait != nil {
		msg := strings.TrimSpace(extractErr.String())
		if msg == "" {
			msg = extractWait.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func stageInstallArtifacts(repoRoot string) error {
	stagePaths := []string{
		".sops.yaml",
		filepath.Join("secrets", "user.yaml"),
	}
	if fileExists(filepath.Join(repoRoot, "secrets", "common.yaml")) {
		stagePaths = append(stagePaths, filepath.Join("secrets", "common.yaml"))
	}
	_, err := requireOK(append([]string{"git", "-C", repoRoot, "add", "--"}, stagePaths...), nil, "")
	return err
}

func writeJSONFile(path string, payload any) error {
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func installReceiptPayload(machineState MachineState, plan InstallPlan, user string) map[string]any {
	return map[string]any{
		"installDisk":    machineState.InstallDisk,
		"initialOutput":  plan.InitialOutput,
		"finalOutput":    plan.FinalOutput,
		"repoPath":       "/etc/nixos",
		"user":           user,
		"installedAt":    machineState.InstalledAt,
		"needsFinalize":  plan.NeedsFinalize,
		"machineId":      machineState.MachineID,
		"hostName":       machineState.HostName,
		"platformKind":   machineState.Platform.Kind,
		"graphicsVendor": machineState.Graphics.Vendor,
	}
}

func RunInstall(request InstallRequest, sink func(Event)) error {
	ensureNixConfig()
	repoRoot, err := normalizeRepoRoot(request.RepoRoot)
	if err != nil {
		return err
	}
	if err := ensureFlakeRepo(repoRoot); err != nil {
		return err
	}

	disk, err := assertSafeInstallDisk(request.Disk)
	if err != nil {
		return err
	}
	if request.MountPoint == "" {
		request.MountPoint = "/mnt"
	}

	if !fileExists("/sys/firmware/efi") {
		return fmt.Errorf("installer requires UEFI mode; /sys/firmware/efi is missing")
	}
	for _, tool := range []string{
		"disko",
		"findmnt",
		"git",
		"lsblk",
		"mkpasswd",
		"nix",
		"nixos-generate-config",
		"nixos-install",
		"sops",
		"tar",
	} {
		if err := ensureTool(tool); err != nil {
			return err
		}
	}

	settings, err := loadSharedSettings(repoRoot)
	if err != nil {
		return err
	}
	if err := assertOwnerRecipientsReady(settings); err != nil {
		return err
	}

	plan, err := loadInstallPlan(repoRoot)
	if err != nil {
		return err
	}

	env, err := prepareSopsEnv(request.AgeKeyFile)
	if err != nil {
		return err
	}
	if request.SecretMode == SecretModeCreate || request.SecretMode == SecretModeReplace {
		if request.Password == "" {
			return fmt.Errorf("password is required when creating or replacing the shared user secret")
		}
	}
	if request.LUKSPassword == "" {
		return fmt.Errorf("LUKS password is required")
	}

	tmpDir, err := os.MkdirTemp("/tmp", "config-nix-exec.")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	luksPasswordFile := filepath.Join(tmpDir, "luks-password")
	if err := os.WriteFile(luksPasswordFile, []byte(request.LUKSPassword), 0o600); err != nil {
		return err
	}

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhasePrepare, Message: "Validating shared settings and preparing local machine state"})
	localDir := localStateDirForRepo(repoRoot)
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePrepare, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhasePrepare, Message: fmt.Sprintf("Using target disk %s", disk)})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhaseDetect, Message: "Detecting the current hardware profile"})
	detected, err := detectHardware("/")
	if err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseDetect, Message: err.Error()})
		return err
	}
	machineID, err := generateMachineID()
	if err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseDetect, Message: err.Error()})
		return err
	}
	machineState := buildMachineState(settings, disk, detected, time.Now(), machineID)
	if err := writeMachineStateFile(localDir, machineState); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseDetect, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhaseDetect, Message: fmt.Sprintf("Detected %s graphics on %s", machineState.Graphics.Vendor, machineState.Platform.Kind)})

	diskoConfigPath := filepath.Join(tmpDir, "disko-config.nix")
	diskoConfig := renderDiskoConfig(filepath.Join(repoRoot, "disko.nix"), disk, luksPasswordFile)
	if err := os.WriteFile(diskoConfigPath, []byte(diskoConfig), 0o644); err != nil {
		return err
	}

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhasePartition, Message: fmt.Sprintf("Partitioning and mounting %s", disk)})
	if err := os.MkdirAll(request.MountPoint, 0o755); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePartition, Message: err.Error()})
		return err
	}
	diskoEnv := map[string]string{"DISKO_ROOT_MOUNTPOINT": request.MountPoint}
	for key, value := range env {
		diskoEnv[key] = value
	}
	if err := streamCommand(sink, PhasePartition, diskoEnv, diskoCommand(diskoConfigPath)); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePartition, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhasePartition, Message: fmt.Sprintf("Mounted target filesystem at %s", request.MountPoint)})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhaseHardware, Message: "Generating local hardware-configuration.nix"})
	hardware, err := requireOK([]string{
		"nixos-generate-config",
		"--root", request.MountPoint,
		"--no-filesystems",
		"--show-hardware-config",
	}, nil, "")
	if err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseHardware, Message: err.Error()})
		return err
	}
	if err := writeHardwareConfigFile(localDir, hardware); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseHardware, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhaseHardware, Message: "Wrote local/hardware-configuration.nix"})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhaseSecrets, Message: "Preparing shared secrets and local runtime secrets"})
	if err := renderSopsConfig(repoRoot); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
		return err
	}
	if err := writeUserSecret(repoRoot, request.SecretMode, request.Password, env); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
		return err
	}
	userPasswordHash, err := readUserPasswordHash(repoRoot, env)
	if err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
		return err
	}
	if err := writeRuntimeSecretsFile(localDir, userPasswordHash); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
		return err
	}
	if fileExists(filepath.Join(repoRoot, "secrets", "common.yaml")) && isSopsFile(filepath.Join(repoRoot, "secrets", "common.yaml")) {
		if _, err := requireOK([]string{
			"sops",
			"--config", filepath.Join(repoRoot, ".sops.yaml"),
			"updatekeys", "-y",
			filepath.Join(repoRoot, "secrets", "common.yaml"),
		}, env, ""); err != nil {
			emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
			return err
		}
	}
	if !isGitCheckout(repoRoot) {
		err := fmt.Errorf("installer requires a git checkout at %s", repoRoot)
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
		return err
	}
	if err := stageInstallArtifacts(repoRoot); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhaseSecrets, Message: "Shared secrets prepared and local runtime state written"})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhasePersist, Message: "Copying the repo and local machine state into /etc/nixos"})
	targetRepoRoot := filepath.Join(request.MountPoint, "etc/nixos")
	if err := copyRepoSnapshot(repoRoot, targetRepoRoot); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePersist, Message: err.Error()})
		return err
	}

	stateDir := filepath.Join(request.MountPoint, "var/lib/config-nix")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePersist, Message: err.Error()})
		return err
	}
	if err := writeJSONFile(filepath.Join(stateDir, "install-receipt.json"), installReceiptPayload(machineState, plan, settings.User.Name)); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePersist, Message: err.Error()})
		return err
	}
	if plan.NeedsFinalize {
		if _, err := os.Create(filepath.Join(stateDir, "finalize-pending")); err != nil {
			emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePersist, Message: err.Error()})
			return err
		}
		if err := writeJSONFile(filepath.Join(stateDir, "finalize-status.json"), map[string]any{
			"status":    "pending",
			"stage":     "waiting-first-boot",
			"updatedAt": time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePersist, Message: err.Error()})
			return err
		}
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhasePersist, Message: "Persisted /etc/nixos and local install state"})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhaseInstall, Message: fmt.Sprintf("Installing NixOS output %s", plan.InitialOutput)})
	installEnv := localStateEnv(targetRepoRoot)
	for key, value := range env {
		installEnv[key] = value
	}
	if err := streamCommand(sink, PhaseInstall, installEnv, nixosInstallCommand(request.MountPoint, targetRepoRoot, plan.InitialOutput)); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseInstall, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhaseInstall, Message: fmt.Sprintf("Installed %s", plan.InitialOutput)})
	emit(sink, Event{
		Kind:    EventInstallDone,
		Phase:   PhaseInstall,
		Message: "Installation completed successfully",
		InstallResult: &InstallResult{
			Disk:          disk,
			InitialOutput: plan.InitialOutput,
			FinalOutput:   plan.FinalOutput,
			NeedsFinalize: plan.NeedsFinalize,
			RepoPath:      "/etc/nixos",
			ReceiptPath:   "/var/lib/config-nix/install-receipt.json",
		},
	})
	return nil
}
