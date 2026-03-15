{ inputs, hostMeta }:
{ hostName, system }:
let
  hostVars = hostMeta.${hostName};
  userName = hostVars.user.name;
  homeModule = ../homes + "/${userName}";
in
inputs.home-manager.lib.homeManagerConfiguration {
  pkgs = inputs.nixpkgs.legacyPackages.${system};

  extraSpecialArgs = {
    inherit inputs hostName hostVars userName;
    hmIntegrated = false;
  };

  modules = [
    ../modules/home/base.nix
    ../modules/home/desktop.nix
    ../modules/home/packages.nix
    homeModule
  ];
}
