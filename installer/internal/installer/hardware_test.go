package installer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFixtureFile(t *testing.T, root, path, content string) {
	t.Helper()
	fullPath := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullPath, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
}

func TestDetectPlatformQEMU(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "sys/class/dmi/id/product_name", "QEMU Standard PC\n")

	platform := detectPlatform(root)
	if platform.Kind != "vm" || platform.Hypervisor != "qemu" {
		t.Fatalf("unexpected platform: %+v", platform)
	}
}

func TestDetectGraphicsPrefersNVIDIA(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "sys/bus/pci/devices/0000:01:00.0/class", "0x030000\n")
	writeFixtureFile(t, root, "sys/bus/pci/devices/0000:01:00.0/vendor", "0x10de\n")
	writeFixtureFile(t, root, "sys/bus/pci/devices/0000:02:00.0/class", "0x030000\n")
	writeFixtureFile(t, root, "sys/bus/pci/devices/0000:02:00.0/vendor", "0x1002\n")

	graphics, err := detectGraphics(root)
	if err != nil {
		t.Fatalf("detect graphics: %v", err)
	}
	if graphics.Vendor != "nvidia" {
		t.Fatalf("expected nvidia, got %+v", graphics)
	}
	if len(graphics.PCIIDs) != 1 || graphics.PCIIDs[0] != "0000:01:00.0" {
		t.Fatalf("unexpected pci ids: %+v", graphics.PCIIDs)
	}
}

func TestBuildMachineStateIncludesDerivedHostname(t *testing.T) {
	settings := SharedSettings{
		HostNamePrefix: "config",
	}
	detected := MachineState{
		Platform: PlatformState{Kind: "vm", Hypervisor: "qemu"},
		Graphics: GraphicsState{Vendor: "amd", Enable32Bit: true, PCIIDs: []string{"0000:03:00.0"}},
	}

	state := buildMachineState(settings, "/dev/vda", detected, time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC), "abcdef1234567890")
	if state.HostName != "config-abcdef12" {
		t.Fatalf("unexpected hostname: %s", state.HostName)
	}
	if state.Graphics.Vendor != "amd" || state.Platform.Kind != "vm" {
		t.Fatalf("unexpected state: %+v", state)
	}
}
