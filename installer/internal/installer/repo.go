package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ensureNixConfig() {
	const line = "experimental-features = nix-command flakes"
	current := os.Getenv("NIX_CONFIG")
	if current == "" {
		_ = os.Setenv("NIX_CONFIG", line)
		return
	}
	if strings.Contains(current, line) {
		return
	}
	_ = os.Setenv("NIX_CONFIG", current+"\n"+line)
}

func run(cmd []string, env map[string]string, stdin string) (string, string, error) {
	if len(cmd) == 0 {
		return "", "", errors.New("empty command")
	}

	command := exec.Command(cmd[0], cmd[1:]...)
	if env != nil {
		command.Env = os.Environ()
		for key, value := range env {
			command.Env = append(command.Env, key+"="+value)
		}
	}

	if stdin != "" {
		command.Stdin = strings.NewReader(stdin)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func requireOK(cmd []string, env map[string]string, stdin string) (string, error) {
	stdout, stderr, err := run(cmd, env, stdin)
	if err != nil {
		details := strings.TrimSpace(stderr)
		if details == "" {
			details = strings.TrimSpace(stdout)
		}
		if details == "" {
			details = err.Error()
		}
		return "", fmt.Errorf("%s failed: %s", strings.Join(cmd, " "), details)
	}
	return stdout, nil
}

func ensureTool(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required command not found: %s", name)
	}
	return nil
}

func normalizeRepoRoot(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("repo path is empty")
	}
	return filepath.Abs(expandUserPath(path))
}

func normalizeExistingPath(path string) (string, error) {
	expanded, err := filepath.Abs(expandUserPath(path))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(expanded); err != nil {
		return "", err
	}
	return expanded, nil
}

func expandUserPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func gitTopLevelFromCwd() string {
	stdout, _, err := run([]string{"git", "rev-parse", "--show-toplevel"}, nil, "")
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(stdout)
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	return abs
}

func ensureFlakeRepo(repoRoot string) error {
	if _, err := os.Stat(filepath.Join(repoRoot, "flake.nix")); err != nil {
		return fmt.Errorf("repo checkout not found at %s", repoRoot)
	}
	return nil
}

func isGitCheckout(repoRoot string) bool {
	_, _, err := run([]string{"git", "-C", repoRoot, "rev-parse", "--show-toplevel"}, nil, "")
	return err == nil
}

func flakeRefForRepo(repoRoot string) string {
	return "path:" + repoRoot
}

func localStateDirForRepo(repoRoot string) string {
	return filepath.Join(repoRoot, "local")
}

func localStateEnv(repoRoot string) map[string]string {
	return map[string]string{
		"CONFIG_NIX_LOCAL_STATE_DIR": localStateDirForRepo(repoRoot),
	}
}

func assertExpectedRepoRevision(repoRoot string) error {
	expectedRev := os.Getenv("CONFIG_NIX_BOOTSTRAP_REV")
	if expectedRev == "" || !isGitCheckout(repoRoot) {
		return nil
	}
	stdout, err := requireOK([]string{"git", "-C", repoRoot, "rev-parse", "HEAD"}, nil, "")
	if err != nil {
		return nil
	}
	actual := strings.TrimSpace(stdout)
	if actual == "" || actual == expectedRev {
		return nil
	}
	return fmt.Errorf("existing checkout at %s is at %s but installer expects %s", repoRoot, actual, expectedRev)
}

func copyDirWritable(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()

		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode()|0o200)
		if err != nil {
			return err
		}
		defer targetFile.Close()

		_, err = io.Copy(targetFile, source)
		return err
	})
}

func bootstrapRepoCheckout(repoRoot string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(repoRoot), 0o755); err != nil {
		return "", err
	}

	repoURL := os.Getenv("CONFIG_NIX_BOOTSTRAP_REPO_URL")
	repoRev := os.Getenv("CONFIG_NIX_BOOTSTRAP_REV")
	flakeSource := os.Getenv("CONFIG_NIX_FLAKE_SOURCE")

	switch {
	case repoURL != "":
		if _, err := requireOK([]string{"git", "clone", repoURL, repoRoot}, nil, ""); err != nil {
			return "", err
		}
		if repoRev != "" {
			if _, err := requireOK([]string{"git", "-C", repoRoot, "checkout", repoRev}, nil, ""); err != nil {
				return "", err
			}
		}
		return repoRoot, nil
	case flakeSource != "":
		if err := os.MkdirAll(repoRoot, 0o755); err != nil {
			return "", err
		}
		if err := copyDirWritable(flakeSource, repoRoot); err != nil {
			return "", err
		}
		return repoRoot, nil
	default:
		return "", fmt.Errorf("cannot bootstrap a writable repo checkout automatically")
	}
}

func prepareInstallRepoRoot() (repoRoot string, sourceKind string, cleanup func(), err error) {
	if root := gitTopLevelFromCwd(); root != "" {
		if err := ensureFlakeRepo(root); err != nil {
			return "", "", nil, err
		}
		if err := assertExpectedRepoRevision(root); err != nil {
			return "", "", nil, err
		}
		return root, "local", func() {}, nil
	}

	tempRoot, err := os.MkdirTemp("/tmp", "config-nix-install.")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tempRoot) }
	if _, err := bootstrapRepoCheckout(tempRoot); err != nil {
		cleanup()
		return "", "", nil, err
	}
	if err := ensureFlakeRepo(tempRoot); err != nil {
		cleanup()
		return "", "", nil, err
	}
	if err := assertExpectedRepoRevision(tempRoot); err != nil {
		cleanup()
		return "", "", nil, err
	}
	return tempRoot, "temporary", cleanup, nil
}

func nixEvalJSON(repoRoot, attribute string, target any) error {
	ensureNixConfig()
	stdout, err := requireOK(
		[]string{
			"nix",
			"--extra-experimental-features",
			"nix-command flakes",
			"eval",
			"--json",
			flakeRefForRepo(repoRoot) + "#" + attribute,
		},
		nil,
		"",
	)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(stdout), target)
}

func flakeRevisionLabel(repoRoot string) string {
	if isGitCheckout(repoRoot) {
		stdout, _, err := run([]string{"git", "-C", repoRoot, "rev-parse", "--short", "HEAD"}, nil, "")
		if err == nil {
			if trimmed := strings.TrimSpace(stdout); trimmed != "" {
				return trimmed
			}
		}
	}

	bootstrapRev := os.Getenv("CONFIG_NIX_BOOTSTRAP_REV")
	if bootstrapRev == "" {
		return "unknown"
	}
	if len(bootstrapRev) > 12 {
		return bootstrapRev[:12]
	}
	return bootstrapRev
}

var loadSharedSettings = loadSharedSettingsFromNix

func loadSharedSettingsFromNix(repoRoot string) (SharedSettings, error) {
	var settings SharedSettings
	if err := nixEvalJSON(repoRoot, "lib.sharedSettings", &settings); err != nil {
		return SharedSettings{}, err
	}
	return settings, nil
}

var loadInstallPlan = loadInstallPlanFromNix

func loadInstallPlanFromNix(repoRoot string) (InstallPlan, error) {
	var plan InstallPlan
	if err := nixEvalJSON(repoRoot, "lib.installPlan", &plan); err != nil {
		return InstallPlan{}, err
	}
	return plan, nil
}

func assertOwnerRecipientsReady(settings SharedSettings) error {
	if len(settings.OwnerAgeRecipients) == 0 {
		return fmt.Errorf("settings.nix must define at least one ownerAgeRecipients entry")
	}
	for _, recipient := range settings.OwnerAgeRecipients {
		if strings.Contains(strings.ToLower(recipient), "replace") {
			return fmt.Errorf("replace ownerAgeRecipients in settings.nix before running the installer")
		}
	}
	return nil
}
