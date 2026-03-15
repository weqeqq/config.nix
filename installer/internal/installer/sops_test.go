package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSopsConfigIncludesSharedRules(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "settings.nix"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	original := loadSharedSettings
	loadSharedSettings = func(string) (SharedSettings, error) {
		return SharedSettings{
			OwnerAgeRecipients: []string{"age1owner", "age1other"},
		}, nil
	}
	defer func() { loadSharedSettings = original }()

	if err := renderSopsConfig(repoRoot); err != nil {
		t.Fatalf("render sops config: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(repoRoot, ".sops.yaml"))
	if err != nil {
		t.Fatalf("read sops config: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "path_regex: ^secrets/common\\.yaml$") {
		t.Fatalf("missing common rule: %s", text)
	}
	if !strings.Contains(text, "path_regex: ^secrets/user\\.yaml$") {
		t.Fatalf("missing shared user rule: %s", text)
	}
	if strings.Contains(text, "secrets/hosts") {
		t.Fatalf("host-specific rules should be gone: %s", text)
	}
}

func TestAssertCommonSecretDecryptableRequiresAgeKey(t *testing.T) {
	repoRoot := t.TempDir()
	secretsDir := filepath.Join(repoRoot, "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "common.yaml"), []byte("sops:\n  version: 3.9.0\n"), 0o644); err != nil {
		t.Fatalf("write common secret: %v", err)
	}

	err := assertCommonSecretDecryptable(repoRoot, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "no age key is available") {
		t.Fatalf("expected missing age key error, got %v", err)
	}
}

func TestPersistInstalledAgeKeyWritesTargetKey(t *testing.T) {
	mountPoint := t.TempDir()
	ageKeyFile := filepath.Join(t.TempDir(), "keys.txt")
	expected := "AGE-SECRET-KEY-1TEST\n"
	if err := os.WriteFile(ageKeyFile, []byte(expected), 0o600); err != nil {
		t.Fatalf("write age key: %v", err)
	}

	if err := persistInstalledAgeKey(mountPoint, ageKeyFile); err != nil {
		t.Fatalf("persist installed age key: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(mountPoint, "var/lib/sops-nix/key.txt"))
	if err != nil {
		t.Fatalf("read persisted key: %v", err)
	}
	if string(content) != expected {
		t.Fatalf("unexpected persisted key content: got %q want %q", string(content), expected)
	}
}
