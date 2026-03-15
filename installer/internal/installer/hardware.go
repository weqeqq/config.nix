package installer

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func readTrimmedFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func detectPlatform(root string) PlatformState {
	dmiPaths := []string{
		filepath.Join(root, "sys/class/dmi/id/product_name"),
		filepath.Join(root, "sys/class/dmi/id/sys_vendor"),
		filepath.Join(root, "sys/class/dmi/id/board_vendor"),
	}

	var joined strings.Builder
	for _, path := range dmiPaths {
		if value := readTrimmedFile(path); value != "" {
			if joined.Len() > 0 {
				joined.WriteByte(' ')
			}
			joined.WriteString(strings.ToLower(value))
		}
	}
	fingerprint := joined.String()

	switch {
	case strings.Contains(fingerprint, "qemu"):
		return PlatformState{Kind: "vm", Hypervisor: "qemu"}
	case strings.Contains(fingerprint, "kvm"):
		return PlatformState{Kind: "vm", Hypervisor: "kvm"}
	case strings.Contains(fingerprint, "vmware"),
		strings.Contains(fingerprint, "virtualbox"),
		strings.Contains(fingerprint, "hyper-v"),
		strings.Contains(fingerprint, "virtual machine"):
		return PlatformState{Kind: "vm", Hypervisor: "none"}
	}

	cpuInfo := strings.ToLower(readTrimmedFile(filepath.Join(root, "proc/cpuinfo")))
	if strings.Contains(cpuInfo, "hypervisor") {
		return PlatformState{Kind: "vm", Hypervisor: "none"}
	}

	return PlatformState{Kind: "bare-metal", Hypervisor: "none"}
}

func detectGraphics(root string) (GraphicsState, error) {
	devicesDir := filepath.Join(root, "sys/bus/pci/devices")
	entries, err := os.ReadDir(devicesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return GraphicsState{Vendor: "generic", Enable32Bit: false, PCIIDs: []string{}}, nil
		}
		return GraphicsState{}, err
	}

	nvidiaIDs := []string{}
	amdIDs := []string{}
	for _, entry := range entries {
		devicePath := filepath.Join(devicesDir, entry.Name())
		class := readTrimmedFile(filepath.Join(devicePath, "class"))
		if !strings.HasPrefix(class, "0x03") {
			continue
		}
		vendor := strings.ToLower(readTrimmedFile(filepath.Join(devicePath, "vendor")))
		switch vendor {
		case "0x10de":
			nvidiaIDs = append(nvidiaIDs, entry.Name())
		case "0x1002":
			amdIDs = append(amdIDs, entry.Name())
		}
	}

	sort.Strings(nvidiaIDs)
	sort.Strings(amdIDs)

	switch {
	case len(nvidiaIDs) > 0:
		return GraphicsState{Vendor: "nvidia", Enable32Bit: true, PCIIDs: nvidiaIDs}, nil
	case len(amdIDs) > 0:
		return GraphicsState{Vendor: "amd", Enable32Bit: true, PCIIDs: amdIDs}, nil
	default:
		return GraphicsState{Vendor: "generic", Enable32Bit: false, PCIIDs: []string{}}, nil
	}
}

func detectHardware(root string) (MachineState, error) {
	graphics, err := detectGraphics(root)
	if err != nil {
		return MachineState{}, err
	}
	return MachineState{
		Platform: detectPlatform(root),
		Graphics: graphics,
	}, nil
}

func generateMachineID() (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

func buildMachineState(settings SharedSettings, disk string, detected MachineState, now time.Time, machineID string) MachineState {
	prefix := settings.HostNamePrefix
	if prefix == "" {
		prefix = "nixos"
	}

	shortID := machineID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	return MachineState{
		MachineID:   machineID,
		HostName:    fmt.Sprintf("%s-%s", prefix, shortID),
		InstalledAt: now.UTC().Format(time.RFC3339),
		InstallDisk: disk,
		Platform:    detected.Platform,
		Graphics:    detected.Graphics,
	}
}

func writeMachineStateFile(localDir string, state MachineState) error {
	content := fmt.Sprintf(`{
  machineId = %q;
  hostName = %q;
  installedAt = %q;
  installDisk = %q;
  platform = {
    kind = %q;
    hypervisor = %q;
  };
  graphics = {
    vendor = %q;
    enable32Bit = %s;
    pciIds = [ %s ];
  };
}
`,
		state.MachineID,
		state.HostName,
		state.InstalledAt,
		state.InstallDisk,
		state.Platform.Kind,
		state.Platform.Hypervisor,
		state.Graphics.Vendor,
		boolLiteral(state.Graphics.Enable32Bit),
		nixStringList(state.Graphics.PCIIDs),
	)

	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(localDir, "machine-state.nix"), []byte(content), 0o644)
}

func writeHardwareConfigFile(localDir, hardware string) error {
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(localDir, "hardware-configuration.nix"), []byte(hardware), 0o644)
}

func boolLiteral(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func nixStringList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}
	return strings.Join(quoted, " ")
}
