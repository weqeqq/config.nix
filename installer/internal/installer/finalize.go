package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type FinalizeRequest struct {
	MarkerPath string
	RepoRoot   string
	StatusPath string
}

func writeSBCTLConfig(configFile, pkiBundle string) error {
	content := fmt.Sprintf(`landlock: true
keydir: %s/keys
guid: %s/GUID
files_db: %s/files.json
bundles_db: %s/bundles.json
keys:
  pk:
    privkey: %s/keys/PK/PK.key
    pubkey: %s/keys/PK/PK.pem
    type: file
  kek:
    privkey: %s/keys/KEK/KEK.key
    pubkey: %s/keys/KEK/KEK.pem
    type: file
  db:
    privkey: %s/keys/db/db.key
    pubkey: %s/keys/db/db.pem
    type: file
`,
		pkiBundle, pkiBundle, pkiBundle, pkiBundle,
		pkiBundle, pkiBundle,
		pkiBundle, pkiBundle,
		pkiBundle, pkiBundle,
	)
	return os.WriteFile(configFile, []byte(content), 0o600)
}

func writeFinalizeStatus(statusPath, status, stage, message string) error {
	return writeJSONFile(statusPath, map[string]any{
		"status":    status,
		"stage":     stage,
		"message":   message,
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

func RunFinalize(request FinalizeRequest) (err error) {
	repoRoot, err := normalizeRepoRoot(request.RepoRoot)
	if err != nil {
		return err
	}
	if err := ensureFlakeRepo(repoRoot); err != nil {
		return err
	}

	if request.MarkerPath == "" {
		request.MarkerPath = "/var/lib/config-nix/finalize-pending"
	}
	if request.StatusPath == "" {
		request.StatusPath = "/var/lib/config-nix/finalize-status.json"
	}
	if !fileExists(request.MarkerPath) {
		return fmt.Errorf("finalize marker not found: %s", request.MarkerPath)
	}

	defer func() {
		if err != nil {
			_ = writeFinalizeStatus(request.StatusPath, "failed", "error", err.Error())
		}
	}()

	settings, err := loadSharedSettings(repoRoot)
	if err != nil {
		return err
	}
	plan, err := loadInstallPlan(repoRoot)
	if err != nil {
		return err
	}
	if !plan.NeedsFinalize {
		return writeFinalizeStatus(request.StatusPath, "skipped", "completed", "no deferred finalization required")
	}

	if err := writeFinalizeStatus(request.StatusPath, "running", "prepare", "starting deferred system finalization"); err != nil {
		return err
	}

	secureBoot := settings.Boot.SecureBoot
	var sbctlConfig string
	if secureBoot.Enable {
		pkiBundle := secureBoot.PkiBundle
		if pkiBundle == "" {
			pkiBundle = "/var/lib/sbctl"
		}
		if err := os.MkdirAll(pkiBundle, 0o700); err != nil {
			return err
		}
		if err := writeFinalizeStatus(request.StatusPath, "running", "secure-boot-keys", "ensuring Secure Boot keys exist"); err != nil {
			return err
		}

		configFile, configErr := os.CreateTemp("", "config-nix-sbctl.*.yaml")
		if configErr != nil {
			return configErr
		}
		sbctlConfig = configFile.Name()
		_ = configFile.Close()
		defer func() { _ = os.Remove(sbctlConfig) }()

		if err := writeSBCTLConfig(sbctlConfig, pkiBundle); err != nil {
			return err
		}

		if !fileExists(filepath.Join(pkiBundle, "keys", "db", "db.key")) {
			cmd := exec.Command("sbctl", "--config", sbctlConfig, "create-keys")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return err
			}
		}
	}

	if err := writeFinalizeStatus(request.StatusPath, "running", "rebuild", "building the final signed system profile"); err != nil {
		return err
	}
	cmd := exec.Command(rebuildCommand("boot", repoRoot, nil)[0], rebuildCommand("boot", repoRoot, nil)[1:]...)
	cmd.Env = os.Environ()
	for key, value := range localStateEnv(repoRoot) {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	if secureBoot.Enable {
		if err := writeFinalizeStatus(request.StatusPath, "running", "secure-boot-enroll", "enrolling Secure Boot keys"); err != nil {
			return err
		}
		cmd := exec.Command("sbctl", "--config", sbctlConfig, "enroll-keys", "--microsoft")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	if err := os.Remove(request.MarkerPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := writeFinalizeStatus(request.StatusPath, "success", "completed", "finalization completed successfully; rebooting"); err != nil {
		return err
	}

	if err := exec.Command("sync").Run(); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "--no-block", "reboot").Run(); err != nil {
		return err
	}
	return nil
}

func LoadFinalizeStatus(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}
