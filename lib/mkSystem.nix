{ inputs, sharedSettings, installPlan, localState }:
{ system, phase ? "final" }:
let
  lib = inputs.nixpkgs.lib;
  userName = sharedSettings.user.name;
  hardwareModule =
    if localState.hardwareConfigurationPath == null then
      {
        fileSystems."/" = {
          device = "/dev/disk/by-label/config-nix-root";
          fsType = "btrfs";
        };
        fileSystems."/boot" = {
          device = "/dev/disk/by-label/config-nix-boot";
          fsType = "vfat";
        };
      }
    else
      localState.hardwareConfigurationPath;
in
inputs.nixpkgs.lib.nixosSystem {
  inherit system;

  specialArgs = {
    inherit inputs installPlan localState phase sharedSettings userName;
    machineState = localState.machineState;
    runtimeSecrets = localState.runtimeSecrets;
  };

  modules = [
    inputs.disko.nixosModules.disko
    inputs.lanzaboote.nixosModules.lanzaboote
    inputs.sops-nix.nixosModules.sops
    inputs.home-manager.nixosModules.home-manager
    ../modules/nixos/config-nix-state.nix
    ../modules/nixos/base.nix
    ../modules/nixos/boot.nix
    ../modules/nixos/install-finalize.nix
    ../modules/nixos/secure-boot.nix
    ../modules/nixos/users.nix
    ../modules/nixos/graphics.nix
    ../modules/nixos/qemu-guest.nix
    ../modules/nixos/hyprland.nix
    ../modules/nixos/desktop-audio.nix
    ../modules/nixos/sops.nix
    hardwareModule
    ({ machineState, runtimeSecrets, ... }: {
      home-manager = {
        useGlobalPkgs = true;
        useUserPackages = true;
        extraSpecialArgs = {
          inherit inputs machineState runtimeSecrets sharedSettings;
          hmIntegrated = true;
        };
        users.${userName} = {
          imports = [
            ../modules/home/base.nix
            ../modules/home/desktop.nix
            ../modules/home/packages.nix
            ../home/default.nix
            ../home/packages.nix
          ];
        };
      };
    })
  ];
}
