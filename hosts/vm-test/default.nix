{ lib, hostVars, ... }:
let
  hardwareConfig = ./hardware-configuration.nix;
in
{
  imports =
    [
      (import ./disko.nix { })
      ./packages.nix
    ]
    ++ lib.optional (builtins.pathExists hardwareConfig) hardwareConfig;

  networking.hostName = hostVars.hostName;
}
