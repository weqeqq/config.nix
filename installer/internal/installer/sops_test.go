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
