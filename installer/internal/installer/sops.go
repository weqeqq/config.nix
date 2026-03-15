package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func defaultAgeKeyFile() string {
	if envPath := os.Getenv("SOPS_AGE_KEY_FILE"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}
	defaultPath := filepath.Join(os.Getenv("HOME"), ".config/sops/age/keys.txt")
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}
	return ""
}

func prepareSopsEnv(ageKeyFile string) (map[string]string, error) {
	env := map[string]string{}
	effective := ageKeyFile
	if effective == "" {
		effective = os.Getenv("SOPS_AGE_KEY_FILE")
	}
	if effective == "" {
		effective = defaultAgeKeyFile()
	}
	if effective == "" {
		return env, nil
	}
	normalized, err := normalizeExistingPath(effective)
	if err != nil {
		return nil, fmt.Errorf("age key file not found: %s", effective)
	}
	env["SOPS_AGE_KEY_FILE"] = normalized
	return env, nil
}

func isSopsFile(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "sops:") {
			return true
		}
	}
	return false
}

func sharedUserSecretPath(repoRoot string) string {
	return filepath.Join(repoRoot, "secrets", "user.yaml")
}

func commonSecretPath(repoRoot string) string {
	return filepath.Join(repoRoot, "secrets", "common.yaml")
}

var sopsCanDecrypt = sopsCanDecryptWithConfig

func sopsCanDecryptWithConfig(repoRoot, secretFile string, env map[string]string) bool {
	_, _, err := run([]string{"sops", "--config", filepath.Join(repoRoot, ".sops.yaml"), "--decrypt", secretFile}, env, "")
	return err == nil
}

func encryptedCommonSecretExists(repoRoot string) bool {
	secretFile := commonSecretPath(repoRoot)
	return fileExists(secretFile) && isSopsFile(secretFile)
}

func assertCommonSecretDecryptable(repoRoot string, env map[string]string) error {
	if !encryptedCommonSecretExists(repoRoot) {
		return nil
	}
	if env["SOPS_AGE_KEY_FILE"] == "" {
		return fmt.Errorf("encrypted secrets/common.yaml exists but no age key is available; provide --age-key-file or set SOPS_AGE_KEY_FILE")
	}
	if !sopsCanDecrypt(repoRoot, commonSecretPath(repoRoot), env) {
		return fmt.Errorf("the provided age key cannot decrypt secrets/common.yaml")
	}
	return nil
}

func secretStatus(repoRoot, ageKeyFile string) (SecretStatus, error) {
	settings, err := loadSharedSettings(repoRoot)
	if err != nil {
		return SecretStatus{}, err
	}
	if err := assertOwnerRecipientsReady(settings); err != nil {
		return SecretStatus{}, err
	}

	env, err := prepareSopsEnv(ageKeyFile)
	if err != nil {
		return SecretStatus{}, err
	}
	secretFile := sharedUserSecretPath(repoRoot)
	status := SecretStatus{
		SecretPath:          secretFile,
		ActiveAgeKeyFile:    env["SOPS_AGE_KEY_FILE"],
		SuggestedAgeKeyFile: defaultAgeKeyFile(),
	}

	switch {
	case !fileExists(secretFile):
		status.Mode = SecretModeCreate
	case !isSopsFile(secretFile):
		status.Mode = SecretModeReuse
		status.HasSecret = true
		status.Decryptable = true
	case sopsCanDecrypt(repoRoot, secretFile, env):
		status.Mode = SecretModeReuse
		status.HasSecret = true
		status.Encrypted = true
		status.Decryptable = true
	default:
		status.Mode = SecretModeNeedsAgeKey
		status.HasSecret = true
		status.Encrypted = true
	}

	if fileExists(secretFile) {
		status.HasSecret = true
	}
	return status, nil
}

func renderSopsConfig(repoRoot string) error {
	settings, err := loadSharedSettings(repoRoot)
	if err != nil {
		return err
	}

	keys := uniqueSorted(settings.OwnerAgeRecipients)
	var builder strings.Builder
	builder.WriteString("creation_rules:\n")
	if len(keys) == 0 {
		builder.WriteString("  []\n")
		return os.WriteFile(filepath.Join(repoRoot, ".sops.yaml"), []byte(builder.String()), 0o644)
	}

	for _, pathRegex := range []string{
		"^secrets/common\\.yaml$",
		"^secrets/user\\.yaml$",
	} {
		builder.WriteString("  - path_regex: " + pathRegex + "\n")
		builder.WriteString("    key_groups:\n")
		builder.WriteString("      - age:\n")
		for _, key := range keys {
			builder.WriteString("          - " + key + "\n")
		}
	}

	return os.WriteFile(filepath.Join(repoRoot, ".sops.yaml"), []byte(builder.String()), 0o644)
}

func uniqueSorted(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			filtered = append(filtered, value)
		}
	}
	sort.Strings(filtered)
	result := make([]string, 0, len(filtered))
	for _, value := range filtered {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func writeUserSecret(repoRoot string, mode SecretMode, password string, env map[string]string) error {
	secretFile := sharedUserSecretPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(secretFile), 0o755); err != nil {
		return err
	}

	switch mode {
	case SecretModeCreate, SecretModeReplace:
		if password == "" {
			return fmt.Errorf("password is required when creating or replacing the shared user secret")
		}
		hashed, err := requireOK([]string{"mkpasswd", "--method=yescrypt", "--stdin"}, nil, password+"\n")
		if err != nil {
			return err
		}
		content := fmt.Sprintf("userPasswordHash: %q\n", strings.TrimSpace(hashed))
		if err := os.WriteFile(secretFile, []byte(content), 0o644); err != nil {
			return err
		}
		_, err = requireOK([]string{
			"sops",
			"--config", filepath.Join(repoRoot, ".sops.yaml"),
			"--encrypt",
			"--in-place",
			secretFile,
		}, env, "")
		return err
	case SecretModeReuse:
		if !fileExists(secretFile) {
			return fmt.Errorf("expected existing shared user secret at %s", secretFile)
		}
		if isSopsFile(secretFile) {
			_, err := requireOK([]string{
				"sops",
				"--config", filepath.Join(repoRoot, ".sops.yaml"),
				"updatekeys", "-y",
				secretFile,
			}, env, "")
			return err
		}
		_, err := requireOK([]string{
			"sops",
			"--config", filepath.Join(repoRoot, ".sops.yaml"),
			"--encrypt",
			"--in-place",
			secretFile,
		}, env, "")
		return err
	default:
		return fmt.Errorf("unexpected secret mode: %s", mode)
	}
}

func parseUserPasswordHash(content string) (string, error) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "userPasswordHash:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "userPasswordHash:"))
		value = strings.Trim(value, "\"")
		if value == "" {
			return "", fmt.Errorf("userPasswordHash is empty")
		}
		return value, nil
	}
	return "", fmt.Errorf("userPasswordHash key not found")
}

func readUserPasswordHash(repoRoot string, env map[string]string) (string, error) {
	secretFile := sharedUserSecretPath(repoRoot)
	if !fileExists(secretFile) {
		return "", fmt.Errorf("shared user secret not found: %s", secretFile)
	}

	if isSopsFile(secretFile) {
		decrypted, err := requireOK([]string{
			"sops",
			"--config", filepath.Join(repoRoot, ".sops.yaml"),
			"--decrypt",
			secretFile,
		}, env, "")
		if err != nil {
			return "", err
		}
		return parseUserPasswordHash(decrypted)
	}

	content, err := os.ReadFile(secretFile)
	if err != nil {
		return "", err
	}
	return parseUserPasswordHash(string(content))
}

func writeRuntimeSecretsFile(localDir, passwordHash string) error {
	content := fmt.Sprintf("{\n  userPasswordHash = %q;\n}\n", passwordHash)
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(localDir, "runtime-secrets.nix"), []byte(content), 0o600)
}

func persistInstalledAgeKey(mountPoint, ageKeyFile string) error {
	if ageKeyFile == "" {
		return nil
	}
	content, err := os.ReadFile(ageKeyFile)
	if err != nil {
		return err
	}
	targetDir := filepath.Join(mountPoint, "var/lib/sops-nix")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(targetDir, "key.txt"), content, 0o600)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func RekeySharedSecrets(repoRoot, ageKeyFile string) error {
	repoRoot, err := normalizeRepoRoot(repoRoot)
	if err != nil {
		return err
	}
	if err := ensureFlakeRepo(repoRoot); err != nil {
		return err
	}

	settings, err := loadSharedSettings(repoRoot)
	if err != nil {
		return err
	}
	if err := assertOwnerRecipientsReady(settings); err != nil {
		return err
	}

	env, err := prepareSopsEnv(ageKeyFile)
	if err != nil {
		return err
	}
	if err := renderSopsConfig(repoRoot); err != nil {
		return err
	}

	for _, secretFile := range []string{
		commonSecretPath(repoRoot),
		sharedUserSecretPath(repoRoot),
	} {
		if !fileExists(secretFile) {
			continue
		}
		if isSopsFile(secretFile) {
			if _, err := requireOK([]string{
				"sops",
				"--config", filepath.Join(repoRoot, ".sops.yaml"),
				"updatekeys", "-y",
				secretFile,
			}, env, ""); err != nil {
				return err
			}
			continue
		}
		if _, err := requireOK([]string{
			"sops",
			"--config", filepath.Join(repoRoot, ".sops.yaml"),
			"--encrypt",
			"--in-place",
			secretFile,
		}, env, ""); err != nil {
			return err
		}
	}

	return nil
}
