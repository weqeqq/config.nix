{ lib, hostVars, pkgs, ... }:
let
  qemuGuestEnabled = (hostVars.virtualization or { }).qemuGuest or false;
in
lib.mkIf qemuGuestEnabled {
  services.qemuGuest.enable = true;
  services.spice-vdagentd.enable = true;

  environment.systemPackages = [
    pkgs.spice-vdagent
  ];
}
