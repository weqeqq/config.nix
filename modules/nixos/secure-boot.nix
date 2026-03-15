{ lib, hostVars, phase, ... }:
let
  secureBoot = (hostVars.boot or { }).secureBoot or { };
  secureBootEnabled = secureBoot.enable or false;
  pkiBundle = secureBoot.pkiBundle or "/var/lib/sbctl";
in
lib.mkIf (secureBootEnabled && phase == "final") {
  boot.loader.systemd-boot.enable = lib.mkForce false;
  boot.lanzaboote = {
    configurationLimit = 10;
    enable = true;
    enrollKeys = false;
    inherit pkiBundle;
  };
}
