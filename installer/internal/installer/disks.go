package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type lsblkPayload struct {
	BlockDevices []lsblkDevice `json:"blockdevices"`
}

type lsblkDevice struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Size        string   `json:"size"`
	Type        string   `json:"type"`
	Model       string   `json:"model"`
	Vendor      string   `json:"vendor"`
	Serial      string   `json:"serial"`
	Tran        string   `json:"tran"`
	Mountpoints []string `json:"mountpoints"`
}

func preferredDiskPath(diskPath string) string {
	resolved, err := filepath.EvalSymlinks(diskPath)
	if err != nil {
		return diskPath
	}

	entries, err := os.ReadDir("/dev/disk/by-id")
	if err != nil {
		return diskPath
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, "-part") {
			continue
		}
		candidate := filepath.Join("/dev/disk/by-id", name)
		target, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		if target == resolved {
			return candidate
		}
	}
	return diskPath
}

func liveMediaDisk() string {
	for _, mountpoint := range []string{"/iso", "/run/rootfsbase"} {
		stdout, _, err := run([]string{"findmnt", "-no", "SOURCE", mountpoint}, nil, "")
		if err != nil {
			continue
		}
		source := strings.TrimSpace(stdout)
		if source == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(source)
		if err != nil {
			continue
		}
		parent, _, err := run([]string{"lsblk", "-ndo", "PKNAME", resolved}, nil, "")
		if err == nil {
			if trimmed := strings.TrimSpace(parent); trimmed != "" {
				return filepath.Join("/dev", trimmed)
			}
		}
		return resolved
	}
	return ""
}

func diskRecordsFromPayload(payload []byte, liveReal string) ([]DiskRecord, error) {
	var raw lsblkPayload
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}

	records := make([]DiskRecord, 0, len(raw.BlockDevices))
	for _, device := range raw.BlockDevices {
		if device.Type != "disk" {
			continue
		}
		if strings.HasPrefix(device.Name, "loop") || strings.HasPrefix(device.Name, "ram") || strings.HasPrefix(device.Name, "zram") {
			continue
		}
		if device.Path == "" {
			continue
		}

		actualReal := ""
		if resolved, err := filepath.EvalSymlinks(device.Path); err == nil {
			actualReal = resolved
		}
		model := strings.TrimSpace(strings.Join([]string{device.Vendor, device.Model}, " "))
		mountpoints := make([]string, 0, len(device.Mountpoints))
		for _, mountpoint := range device.Mountpoints {
			if mountpoint != "" {
				mountpoints = append(mountpoints, mountpoint)
			}
		}
		records = append(records, DiskRecord{
			Path:          device.Path,
			PreferredPath: preferredDiskPath(device.Path),
			Size:          defaultString(device.Size, "?"),
			Model:         defaultString(model, "Unknown device"),
			Transport:     defaultString(device.Tran, "unknown"),
			Serial:        defaultString(device.Serial, "no-serial"),
			Mountpoints:   mountpoints,
			IsLiveMedia:   liveReal != "" && actualReal == liveReal,
		})
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].PreferredPath < records[j].PreferredPath
	})
	return records, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func listDisks() ([]DiskRecord, error) {
	stdout, err := requireOK([]string{"lsblk", "-J", "-o", "NAME,PATH,SIZE,TYPE,MODEL,VENDOR,SERIAL,TRAN,MOUNTPOINTS"}, nil, "")
	if err != nil {
		return nil, err
	}
	liveReal := ""
	if live := liveMediaDisk(); live != "" {
		if resolved, err := filepath.EvalSymlinks(live); err == nil {
			liveReal = resolved
		}
	}
	return diskRecordsFromPayload([]byte(stdout), liveReal)
}

func assertSafeInstallDisk(disk string) (string, error) {
	info, err := os.Lstat(disk)
	if err != nil {
		return "", fmt.Errorf("selected disk is not accessible: %s", disk)
	}
	if info.Mode()&os.ModeSymlink == 0 && info.Mode()&os.ModeDevice == 0 {
		return "", fmt.Errorf("selected disk is not a block device: %s", disk)
	}

	selectedReal, err := filepath.EvalSymlinks(disk)
	if err != nil {
		return "", fmt.Errorf("cannot resolve selected disk: %s", disk)
	}
	liveReal := ""
	if live := liveMediaDisk(); live != "" {
		if resolved, err := filepath.EvalSymlinks(live); err == nil {
			liveReal = resolved
		}
	}
	if liveReal != "" && selectedReal == liveReal {
		return "", fmt.Errorf("selected disk %s appears to be the current boot medium", disk)
	}
	return preferredDiskPath(disk), nil
}
