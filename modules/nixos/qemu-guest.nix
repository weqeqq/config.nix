{ lib, machineState, pkgs, ... }:
let
  qemuGuestEnabled = ((machineState.platform or { }).kind or "bare-metal") == "vm";
in
lib.mkIf qemuGuestEnabled {
  boot.initrd.availableKernelModules = [
    "ata_piix"
    "sd_mod"
    "sr_mod"
    "virtio_blk"
    "virtio_mmio"
    "virtio_net"
    "virtio_pci"
    "virtio_scsi"
  ];

  services.qemuGuest.enable = true;
  services.spice-vdagentd.enable = true;

  environment.systemPackages = [
    pkgs.spice-vdagent
  ];
}
