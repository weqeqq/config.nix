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

func sopsCanDecrypt(repoRoot, secretFile string, env map[string]string) bool {
	_, _, err := run([]string{"sops", "--config", filepath.Join(repoRoot, ".sops.yaml"), "--decrypt", secretFile}, env, "")
	return err == nil
}

func secretStatus(repoRoot, host, ageKeyFile string) (SecretStatus, error) {
	meta, err := loadAllHostMeta(repoRoot)
	if err != nil {
		return SecretStatus{}, err
	}
	hostMeta, ok := meta[host]
	if !ok {
		return SecretStatus{}, fmt.Errorf("unknown host: %s", host)
	}
	if err := assertOwnerRecipientsReady(host, hostMeta); err != nil {
		return SecretStatus{}, err
	}

	env, err := prepareSopsEnv(ageKeyFile)
	if err != nil {
		return SecretStatus{}, err
	}
	secretFile := filepath.Join(repoRoot, "secrets", "hosts", host+".yaml")
	status := SecretStatus{
		Host:                host,
		HostSecretPath:      secretFile,
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
	meta, err := loadAllHostMeta(repoRoot)
	if err != nil {
		return err
	}

	hostNames := make([]string, 0, len(meta))
	commonKeys := make([]string, 0)
	for host, hostMeta := range meta {
		hostNames = append(hostNames, host)
		commonKeys = append(commonKeys, hostMeta.OwnerAgeRecipients...)
	}
	sort.Strings(hostNames)
	commonKeys = uniqueSorted(commonKeys)

	var builder strings.Builder
	builder.WriteString("creation_rules:\n")
	if len(commonKeys) > 0 {
		builder.WriteString("  - path_regex: ^secrets/common\\.yaml$\n")
		builder.WriteString("    key_groups:\n")
		builder.WriteString("      - age:\n")
		for _, key := range commonKeys {
			builder.WriteString("          - " + key + "\n")
		}
	}

	for _, host := range hostNames {
		keys := append([]string(nil), meta[host].OwnerAgeRecipients...)
		hostPubFile := filepath.Join(repoRoot, "secrets", "hosts", host+".age.pub")
		if fileExists(hostPubFile) {
			if content, err := os.ReadFile(hostPubFile); err == nil {
				keys = append(keys, strings.TrimSpace(string(content)))
			}
		}
		keys = uniqueSorted(keys)
		if len(keys) == 0 {
			continue
		}
		builder.WriteString(fmt.Sprintf("  - path_regex: ^secrets/hosts/%s\\.yaml$\n", host))
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

func writeHostSecret(repoRoot, host string, mode SecretMode, password string, env map[string]string) error {
	hostSecretFile := filepath.Join(repoRoot, "secrets", "hosts", host+".yaml")
	if err := os.MkdirAll(filepath.Dir(hostSecretFile), 0o755); err != nil {
		return err
	}

	switch mode {
	case SecretModeCreate, SecretModeReplace:
		if password == "" {
			return fmt.Errorf("password is required when creating or replacing the host secret")
		}
		hashed, err := requireOK([]string{"mkpasswd", "--method=yescrypt", "--stdin"}, nil, password+"\n")
		if err != nil {
			return err
		}
		content := fmt.Sprintf("userPasswordHash: %q\n", strings.TrimSpace(hashed))
		if err := os.WriteFile(hostSecretFile, []byte(content), 0o644); err != nil {
			return err
		}
		_, err = requireOK([]string{
			"sops",
			"--config", filepath.Join(repoRoot, ".sops.yaml"),
			"--encrypt",
			"--in-place",
			hostSecretFile,
		}, env, "")
		return err
	case SecretModeReuse:
		if !fileExists(hostSecretFile) {
			return fmt.Errorf("expected existing host secret at %s", hostSecretFile)
		}
		if isSopsFile(hostSecretFile) {
			_, err := requireOK([]string{
				"sops",
				"--config", filepath.Join(repoRoot, ".sops.yaml"),
				"updatekeys", "-y",
				hostSecretFile,
			}, env, "")
			return err
		}
		_, err := requireOK([]string{
			"sops",
			"--config", filepath.Join(repoRoot, ".sops.yaml"),
			"--encrypt", "--in-place",
			hostSecretFile,
		}, env, "")
		return err
	default:
		return fmt.Errorf("unexpected secret mode: %s", mode)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
