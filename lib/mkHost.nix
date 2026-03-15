{ inputs, hostMeta }:
{ hostName, system }:
let
  hostVars = hostMeta.${hostName};
  userName = hostVars.user.name;
  hostModule = ../hosts + "/${hostName}";
  homeModule = ../homes + "/${userName}";
in
inputs.nixpkgs.lib.nixosSystem {
  inherit system;

  specialArgs = {
    inherit inputs hostName hostVars userName;
  };

  modules = [
    inputs.disko.nixosModules.disko
    inputs.sops-nix.nixosModules.sops
    inputs.home-manager.nixosModules.home-manager
    ../modules/nixos/base.nix
    ../modules/nixos/boot.nix
    ../modules/nixos/users.nix
    ../modules/nixos/nvidia.nix
    ../modules/nixos/qemu-guest.nix
    ../modules/nixos/hyprland.nix
    ../modules/nixos/desktop-audio.nix
    ../modules/nixos/sops.nix
    hostModule
    ({ ... }: {
      home-manager = {
        useGlobalPkgs = true;
        useUserPackages = true;
        extraSpecialArgs = {
          inherit inputs hostName hostVars userName;
        };
        users.${userName} = import homeModule;
      };
    })
  ];
}
