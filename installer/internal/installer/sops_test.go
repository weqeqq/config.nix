package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSopsConfigIncludesOwnerAndHostKeys(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "secrets", "hosts"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	original := loadAllHostMeta
	loadAllHostMeta = func(string) (hostMetaMap, error) {
		return hostMetaMap{
			"desktop": {
				OwnerAgeRecipients: []string{"age1owner", "age1other"},
			},
			"vm-test": {
				OwnerAgeRecipients: []string{"age1owner"},
			},
		}, nil
	}
	defer func() { loadAllHostMeta = original }()

	if err := os.WriteFile(filepath.Join(repoRoot, "secrets", "hosts", "desktop.age.pub"), []byte("age1host\n"), 0o644); err != nil {
		t.Fatalf("write host pub: %v", err)
	}
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
	if !strings.Contains(text, "path_regex: ^secrets/hosts/desktop\\.yaml$") {
		t.Fatalf("missing desktop rule: %s", text)
	}
	if !strings.Contains(text, "age1host") {
		t.Fatalf("missing host recipient: %s", text)
	}
}
