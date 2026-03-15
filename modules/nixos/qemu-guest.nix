{ lib, hostVars, pkgs, ... }:
let
  qemuGuestEnabled = (hostVars.virtualization or { }).qemuGuest or false;
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
