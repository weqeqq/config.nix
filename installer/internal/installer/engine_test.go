package installer

import (
	"strings"
	"testing"
)

func TestDiskoCommandUsesNonInteractiveWipeFlag(t *testing.T) {
	args := diskoCommand("/tmp/disko-config.nix")

	expected := []string{
		"disko",
		"--mode", "destroy,format,mount",
		"--yes-wipe-all-disks",
		"/tmp/disko-config.nix",
	}

	if len(args) != len(expected) {
		t.Fatalf("unexpected arg count: got %d want %d", len(args), len(expected))
	}
	for idx := range expected {
		if args[idx] != expected[idx] {
			t.Fatalf("unexpected arg %d: got %q want %q", idx, args[idx], expected[idx])
		}
	}
}

func TestNixosInstallCommandDisablesInteractiveRootPassword(t *testing.T) {
	args := nixosInstallCommand("/mnt", "/mnt/etc/nixos", "vm-test-install")

	expected := []string{
		"nixos-install",
		"--root", "/mnt",
		"--flake", "path:/mnt/etc/nixos#vm-test-install",
		"--no-root-passwd",
	}

	if len(args) != len(expected) {
		t.Fatalf("unexpected arg count: got %d want %d", len(args), len(expected))
	}
	for idx := range expected {
		if args[idx] != expected[idx] {
			t.Fatalf("unexpected arg %d: got %q want %q", idx, args[idx], expected[idx])
		}
	}
}

func TestRenderDiskoConfigIncludesLuksPasswordFile(t *testing.T) {
	config := renderDiskoConfig("/repo/hosts/vm-test/disko.nix", "/dev/vda", "/tmp/luks-pass")

	for _, snippet := range []string{
		`diskDevice = "/dev/vda";`,
		`luksPasswordFile = "/tmp/luks-pass";`,
	} {
		if !strings.Contains(config, snippet) {
			t.Fatalf("rendered config missing %q in %q", snippet, config)
		}
	}
}
