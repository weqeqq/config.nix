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

	hosts, err := loadHosts(repoRoot)
	if err != nil {
		cleanup()
		return Session{}, nil, err
	}

	requiredTools := map[string]bool{}
	for _, tool := range []string{
		"age-keygen",
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
		Hosts: hosts,
		Disks: disks,
	}, cleanup, nil
}

func SecretStatusFor(repoRoot, host, ageKeyFile string) (SecretStatus, error) {
	return secretStatus(repoRoot, host, ageKeyFile)
}

func emit(sink func(Event), event Event) {
	if sink != nil {
		sink(event)
	}
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

func stageInstallArtifacts(repoRoot, host string) error {
	stagePaths := []string{
		".sops.yaml",
		filepath.Join("hosts", host, "hardware-configuration.nix"),
		filepath.Join("secrets", "hosts", host+".age.pub"),
		filepath.Join("secrets", "hosts", host+".yaml"),
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

func installReceiptPayload(host, disk, initialOutput, finalOutput, user string, needsFinalize bool) map[string]any {
	return map[string]any{
		"host":          host,
		"disk":          disk,
		"initialOutput": initialOutput,
		"finalOutput":   finalOutput,
		"repoPath":      "/etc/nixos",
		"user":          user,
		"installedAt":   time.Now().UTC().Format(time.RFC3339),
		"needsFinalize": needsFinalize,
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
		"age-keygen",
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

	metaMap, err := loadAllHostMeta(repoRoot)
	if err != nil {
		return err
	}
	hostMeta, ok := metaMap[request.Host]
	if !ok {
		return fmt.Errorf("unknown host: %s", request.Host)
	}
	if err := assertOwnerRecipientsReady(request.Host, hostMeta); err != nil {
		return err
	}
	plan, err := hostInstallPlan(repoRoot, request.Host)
	if err != nil {
		return err
	}

	env, err := prepareSopsEnv(request.AgeKeyFile)
	if err != nil {
		return err
	}
	if request.SecretMode == SecretModeCreate || request.SecretMode == SecretModeReplace {
		if request.Password == "" {
			return fmt.Errorf("password is required when creating or replacing the host secret")
		}
	}

	tmpDir, err := os.MkdirTemp("/tmp", "config-nix-exec.")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	diskoConfigPath := filepath.Join(tmpDir, "disko-config.nix")
	diskoConfig := fmt.Sprintf("(import %q {\n  diskDevice = %q;\n})\n", filepath.Join(repoRoot, "hosts", request.Host, "disko.nix"), disk)
	if err := os.WriteFile(diskoConfigPath, []byte(diskoConfig), 0o644); err != nil {
		return err
	}

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhasePrepare, Message: "Validating install plan and rendering the disko configuration"})
	if err := os.MkdirAll(request.MountPoint, 0o755); err != nil {
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhasePrepare, Message: fmt.Sprintf("Using host %s and target disk %s", request.Host, disk)})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhasePartition, Message: fmt.Sprintf("Partitioning and mounting %s", disk)})
	diskoEnv := map[string]string{"DISKO_ROOT_MOUNTPOINT": request.MountPoint}
	for key, value := range env {
		diskoEnv[key] = value
	}
	if err := streamCommand(sink, PhasePartition, diskoEnv, []string{"disko", "--mode", "destroy,format,mount", diskoConfigPath}); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePartition, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhasePartition, Message: fmt.Sprintf("Mounted target filesystem at %s", request.MountPoint)})

	hostDir := filepath.Join(repoRoot, "hosts", request.Host)
	hostPubFile := filepath.Join(repoRoot, "secrets", "hosts", request.Host+".age.pub")

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhaseHardware, Message: "Generating hardware-configuration.nix"})
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
	if err := os.WriteFile(filepath.Join(hostDir, "hardware-configuration.nix"), []byte(hardware), 0o644); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseHardware, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhaseHardware, Message: fmt.Sprintf("Wrote hosts/%s/hardware-configuration.nix", request.Host)})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhaseHostKey, Message: "Creating the target host age key"})
	hostKeyDir := filepath.Join(request.MountPoint, "var/lib/sops-nix")
	if err := os.MkdirAll(hostKeyDir, 0o700); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseHostKey, Message: err.Error()})
		return err
	}
	if _, err := requireOK([]string{"age-keygen", "-o", filepath.Join(hostKeyDir, "key.txt")}, nil, ""); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseHostKey, Message: err.Error()})
		return err
	}
	if err := os.Chmod(filepath.Join(hostKeyDir, "key.txt"), 0o600); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseHostKey, Message: err.Error()})
		return err
	}
	hostPub, err := requireOK([]string{"age-keygen", "-y", filepath.Join(hostKeyDir, "key.txt")}, nil, "")
	if err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseHostKey, Message: err.Error()})
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hostPubFile), 0o755); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseHostKey, Message: err.Error()})
		return err
	}
	if err := os.WriteFile(hostPubFile, []byte(strings.TrimSpace(hostPub)+"\n"), 0o644); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseHostKey, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhaseHostKey, Message: fmt.Sprintf("Wrote secrets/hosts/%s.age.pub", request.Host)})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhaseSecrets, Message: "Rendering sops rules and preparing host secrets"})
	if err := renderSopsConfig(repoRoot); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
		return err
	}
	if err := writeHostSecret(repoRoot, request.Host, request.SecretMode, request.Password, env); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
		return err
	}
	commonSecret := filepath.Join(repoRoot, "secrets", "common.yaml")
	if isSopsFile(commonSecret) {
		if _, err := requireOK([]string{
			"sops",
			"--config", filepath.Join(repoRoot, ".sops.yaml"),
			"updatekeys", "-y",
			commonSecret,
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
	if err := stageInstallArtifacts(repoRoot, request.Host); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseSecrets, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhaseSecrets, Message: fmt.Sprintf("Staged generated files for %s", request.Host)})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhasePersist, Message: "Copying the full git checkout into /etc/nixos"})
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
	if err := writeJSONFile(filepath.Join(stateDir, "install-receipt.json"), installReceiptPayload(request.Host, disk, plan.InitialOutput, plan.FinalOutput, hostMeta.User.Name, plan.NeedsFinalize)); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePersist, Message: err.Error()})
		return err
	}
	if plan.NeedsFinalize {
		if _, err := os.Create(filepath.Join(stateDir, "finalize-pending")); err != nil {
			emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePersist, Message: err.Error()})
			return err
		}
		if err := writeJSONFile(filepath.Join(stateDir, "finalize-status.json"), map[string]any{
			"host":      request.Host,
			"status":    "pending",
			"updatedAt": time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			emit(sink, Event{Kind: EventPhaseFailed, Phase: PhasePersist, Message: err.Error()})
			return err
		}
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhasePersist, Message: "Persisted /etc/nixos and wrote install state files"})

	emit(sink, Event{Kind: EventPhaseStart, Phase: PhaseInstall, Message: fmt.Sprintf("Installing NixOS output %s", plan.InitialOutput)})
	if err := streamCommand(sink, PhaseInstall, env, []string{
		"nixos-install",
		"--root", request.MountPoint,
		"--flake", fmt.Sprintf("path:%s#%s", targetRepoRoot, plan.InitialOutput),
	}); err != nil {
		emit(sink, Event{Kind: EventPhaseFailed, Phase: PhaseInstall, Message: err.Error()})
		return err
	}
	emit(sink, Event{Kind: EventPhaseComplete, Phase: PhaseInstall, Message: fmt.Sprintf("Installed %s", plan.InitialOutput)})
	emit(sink, Event{
		Kind:    EventInstallDone,
		Phase:   PhaseInstall,
		Message: "Installation completed successfully",
		InstallResult: &InstallResult{
			Host:          request.Host,
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
