{ lib, hostVars, pkgs, ... }:
let
  secureBoot = (hostVars.boot or { }).secureBoot or { };
  secureBootEnabled = secureBoot.enable or false;
  pkiBundle = secureBoot.pkiBundle or "/var/lib/sbctl";
in
lib.mkIf secureBootEnabled {
  environment.systemPackages = [
    pkgs.sbctl
  ];

  boot.loader.systemd-boot.enable = lib.mkForce false;
  boot.lanzaboote = {
    enable = true;
    inherit pkiBundle;
  };
}
