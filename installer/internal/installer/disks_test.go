package installer

import "testing"

func TestDiskRecordsFromPayloadFiltersPseudoDevices(t *testing.T) {
	payload := []byte(`{
	  "blockdevices": [
	    {"name":"loop0","path":"/dev/loop0","size":"1G","type":"disk","model":"","vendor":"","serial":"","tran":"","mountpoints":[]},
	    {"name":"zram0","path":"/dev/zram0","size":"2G","type":"disk","model":"","vendor":"","serial":"","tran":"","mountpoints":["[SWAP]"]},
	    {"name":"sda","path":"/dev/sda","size":"10G","type":"disk","model":"Disk A","vendor":"ATA","serial":"AAA","tran":"sata","mountpoints":[]}
	  ]
	}`)

	disks, err := diskRecordsFromPayload(payload, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(disks) != 1 {
		t.Fatalf("expected 1 disk, got %d", len(disks))
	}
	if disks[0].Path != "/dev/sda" {
		t.Fatalf("expected /dev/sda, got %s", disks[0].Path)
	}
}
